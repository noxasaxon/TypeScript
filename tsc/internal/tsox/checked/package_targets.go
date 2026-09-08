package checked

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// JSON object source order is observable in Node's condition selection. A
// duplicate key replaces its value while retaining its original key position.
type packageJSONValue struct {
	kind   byte
	text   string
	object []packageJSONMember
	array  []*packageJSONValue
}
type packageJSONMember struct {
	name  string
	value *packageJSONValue
}

func (v *packageJSONValue) get(name string) *packageJSONValue {
	if v == nil {
		return nil
	}
	for _, m := range v.object {
		if m.name == name {
			return m.value
		}
	}
	return nil
}
func (v *packageJSONValue) stringValue() string {
	if v != nil && v.kind == 's' {
		return v.text
	}
	return ""
}
func parsePackageJSON(text string) (*packageJSONValue, error) {
	d := json.NewDecoder(strings.NewReader(text))
	d.UseNumber()
	var parse func() (*packageJSONValue, error)
	parse = func() (*packageJSONValue, error) {
		token, err := d.Token()
		if err != nil {
			return nil, err
		}
		switch token := token.(type) {
		case nil:
			return &packageJSONValue{kind: 'n'}, nil
		case string:
			return &packageJSONValue{kind: 's', text: token}, nil
		case bool:
			return &packageJSONValue{kind: 'b'}, nil
		case json.Number:
			return &packageJSONValue{kind: 'd', text: string(token)}, nil
		case json.Delim:
			value := &packageJSONValue{kind: byte(token)}
			switch token {
			case '{':
				for d.More() {
					key, err := d.Token()
					if err != nil {
						return nil, err
					}
					name, ok := key.(string)
					if !ok {
						return nil, fmt.Errorf("object key")
					}
					child, err := parse()
					if err != nil {
						return nil, err
					}
					replaced := false
					for i := range value.object {
						if value.object[i].name == name {
							value.object[i].value = child
							replaced = true
							break
						}
					}
					if !replaced {
						value.object = append(value.object, packageJSONMember{name, child})
					}
				}
			case '[':
				for d.More() {
					child, err := parse()
					if err != nil {
						return nil, err
					}
					value.array = append(value.array, child)
				}
			default:
				return nil, fmt.Errorf("unexpected delimiter")
			}
			if _, err = d.Token(); err != nil {
				return nil, err
			}
			return value, nil
		}
		return nil, fmt.Errorf("unexpected JSON token")
	}
	value, err := parse()
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing package JSON")
	}
	if value.kind != '{' {
		return nil, fmt.Errorf("package metadata must be an object")
	}
	return value, nil
}

var invalidPackageTarget = errors.New("invalid package target")

type packageTarget struct {
	path, external string
	blocked        bool
}

func (t packageTarget) selected() bool { return t.path != "" || t.external != "" || t.blocked }
func nodePackageConditions(mode RuntimeMode) map[string]bool {
	return map[string]bool{"node": true, "node-addons": true, "module-sync": true, string(mode): true}
}
func packageNumericCondition(s string) bool {
	v, err := strconv.ParseUint(s, 10, 32)
	return err == nil && v < 4294967295 && strconv.FormatUint(v, 10) == s
}
func selectPackageTarget(value *packageJSONValue, directory string, mode RuntimeMode, internal bool) (packageTarget, error) {
	if value == nil {
		return packageTarget{}, nil
	}
	switch value.kind {
	case 'n':
		return packageTarget{blocked: true}, nil
	case 's':
		target := value.text
		if !strings.HasPrefix(target, "./") {
			if internal && target != "" && !strings.HasPrefix(target, "../") && !strings.HasPrefix(target, "/") && !strings.ContainsAny(target, ":\\#") {
				return packageTarget{external: target}, nil
			}
			return packageTarget{}, fmt.Errorf("%w: %q", invalidPackageTarget, target)
		}
		if strings.ContainsAny(target, "%?#\\*") {
			return packageTarget{}, fmt.Errorf("encoded/URL/pattern package target is not implemented")
		}
		for _, part := range strings.Split(target[2:], "/") {
			if part == ".." || part == "." || strings.EqualFold(part, "node_modules") {
				return packageTarget{}, fmt.Errorf("%w: forbidden path segment", invalidPackageTarget)
			}
			if part == "" {
				return packageTarget{}, fmt.Errorf("deprecated empty target segment is not implemented")
			}
		}
		return packageTarget{path: filepath.Join(directory, target)}, nil
	case '[':
		if len(value.array) == 0 {
			return packageTarget{blocked: true}, nil
		}
		var lastErr error
		last := packageTarget{}
		for _, item := range value.array {
			target, err := selectPackageTarget(item, directory, mode, internal)
			if err != nil {
				if !errors.Is(err, invalidPackageTarget) {
					return packageTarget{}, err
				}
				lastErr = err
				continue
			}
			if !target.selected() {
				continue
			}
			if target.blocked {
				lastErr = nil
				last = target
				continue
			}
			return target, nil
		}
		if lastErr != nil {
			return packageTarget{}, lastErr
		}
		return last, nil
	case '{':
		for _, m := range value.object {
			if packageNumericCondition(m.name) {
				return packageTarget{}, fmt.Errorf("numeric condition key is invalid")
			}
		}
		conditions := nodePackageConditions(mode)
		for _, m := range value.object {
			if m.name == "default" || conditions[m.name] {
				target, err := selectPackageTarget(m.value, directory, mode, internal)
				if err != nil {
					return packageTarget{}, err
				}
				if target.selected() {
					return target, nil
				}
			}
		}
		return packageTarget{}, nil
	default:
		return packageTarget{}, invalidPackageTarget
	}
}
func packageMapTarget(value *packageJSONValue, key, directory string, mode RuntimeMode, internal bool) (packageTarget, error) {
	if value == nil {
		return packageTarget{}, nil
	}
	if internal {
		if value.kind != '{' {
			return packageTarget{}, fmt.Errorf("imports mapping must be an object")
		}
	} else {
		sugar := value.kind == 's' || value.kind == '['
		if value.kind == '{' {
			style := -1
			for _, m := range value.object {
				current := 0
				if !strings.HasPrefix(m.name, ".") {
					current = 1
				}
				if style != -1 && current != style {
					return packageTarget{}, fmt.Errorf("mixed exports condition/subpath keys")
				}
				style = current
			}
			sugar = style == 1
		}
		if sugar {
			if key != "." {
				return packageTarget{}, nil
			}
			return selectPackageTarget(value, directory, mode, false)
		}
	}
	if strings.HasSuffix(key, "/") || strings.Contains(key, "*") {
		return packageTarget{}, fmt.Errorf("pattern/directory request is not implemented")
	}
	if exact := value.get(key); exact != nil {
		return selectPackageTarget(exact, directory, mode, internal)
	}
	for _, m := range value.object {
		if strings.Contains(m.name, "*") {
			return packageTarget{}, fmt.Errorf("package subpath patterns are not implemented")
		}
	}
	return packageTarget{}, nil
}
