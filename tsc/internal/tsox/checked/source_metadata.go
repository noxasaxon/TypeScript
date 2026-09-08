package checked

import (
	"crypto/sha256"
	"fmt"
	"hash"
	"reflect"
	"sort"
)

// The source AST is sealed separately. Here actual AST/checker owner pointers
// remain opaque identities; walking their cyclic/lazy caches would certify no
// additional source semantics. All source-plan/startup metadata is observed.
func sourceSnapshotDigest(s *CJSBodySourceSnapshot) ([32]byte, error) {
	return sourceMetadataDigest(reflect.ValueOf(struct {
		Owner           *CJSBodySourceSnapshot
		Program         *Program
		States          map[string]*CJSExportState
		Plans           *RuntimeSourcePlans
		ConfiguredRoots int
		ConfiguredFiles []string
		Fingerprint     string
		Runtime         []CJSBodyRuntimeModule
	}{s, s.Program, s.States, s.Plans, s.ConfiguredRoots, s.ConfiguredFiles, s.CapturedFingerprint, s.RuntimeModules}))
}
func sourceMetadataDigest(v reflect.Value) ([32]byte, error) {
	h := sha256.New()
	seen := map[struct {
		typ reflect.Type
		ptr uintptr
	}]bool{}
	var walk func(reflect.Value, int) error
	walk = func(v reflect.Value, depth int) error {
		if depth > 1024 {
			return fmt.Errorf("source metadata nesting exceeds captured domain")
		}
		if !v.IsValid() {
			fmt.Fprint(h, "invalid;")
			return nil
		}
		fmt.Fprintf(h, "%s:", v.Type())
		switch v.Kind() {
		case reflect.Interface:
			if v.IsNil() {
				fmt.Fprint(h, "nil;")
				return nil
			}
			return walk(v.Elem(), depth+1)
		case reflect.Pointer:
			fmt.Fprintf(h, "%x;", v.Pointer())
			if v.IsNil() {
				return nil
			}
			t := v.Type().Elem()
			if t.PkgPath() == "github.com/microsoft/typescript-go/internal/ast" || t.PkgPath() == "github.com/microsoft/typescript-go/internal/compiler" || t == reflect.TypeFor[CJSBodySourceSnapshot]() {
				return nil
			}
			k := struct {
				typ reflect.Type
				ptr uintptr
			}{v.Type(), v.Pointer()}
			if seen[k] {
				return nil
			}
			seen[k] = true
			return walk(v.Elem(), depth+1)
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				fmt.Fprint(h, v.Type().Field(i).Name, ";")
				if err := walk(v.Field(i), depth+1); err != nil {
					return err
				}
			}
		case reflect.Slice:
			fmt.Fprintf(h, "%x/%d;", v.Pointer(), v.Len())
			for i := 0; i < v.Len(); i++ {
				if err := walk(v.Index(i), depth+1); err != nil {
					return err
				}
			}
		case reflect.Array:
			for i := 0; i < v.Len(); i++ {
				if err := walk(v.Index(i), depth+1); err != nil {
					return err
				}
			}
		case reflect.Map:
			if v.IsNil() {
				fmt.Fprint(h, "nil;")
			}
			keys := v.MapKeys()
			keyText := func(k reflect.Value) string {
				switch k.Kind() {
				case reflect.String:
					return "s" + k.String()
				case reflect.Pointer:
					return fmt.Sprintf("p%x", k.Pointer())
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
					return fmt.Sprintf("i%d", k.Int())
				case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
					return fmt.Sprintf("u%d", k.Uint())
				}
				return ""
			}
			sort.Slice(keys, func(i, j int) bool { return keyText(keys[i]) < keyText(keys[j]) })
			fmt.Fprintf(h, "%d;", len(keys))
			for _, k := range keys {
				if keyText(k) == "" {
					return fmt.Errorf("unclassified source map key %s", k.Type())
				}
				if err := walk(k, depth+1); err != nil {
					return err
				}
				if err := walk(v.MapIndex(k), depth+1); err != nil {
					return err
				}
			}
		default:
			if err := sourceMetadataScalar(h, v); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(v, 0); err != nil {
		return [32]byte{}, err
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}
func sourceMetadataScalar(h hash.Hash, v reflect.Value) error {
	switch v.Kind() {
	case reflect.Bool:
		fmt.Fprintf(h, "%t;", v.Bool())
	case reflect.String:
		fmt.Fprintf(h, "%q;", v.String())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fmt.Fprintf(h, "%d;", v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		fmt.Fprintf(h, "%d;", v.Uint())
	case reflect.Float32, reflect.Float64:
		fmt.Fprintf(h, "%x;", v.Float())
	default:
		return fmt.Errorf("unclassified source metadata %s", v.Type())
	}
	return nil
}
