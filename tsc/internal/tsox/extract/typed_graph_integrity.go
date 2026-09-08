package extract

import (
	"fmt"
	"github.com/microsoft/typescript-go/tsox/graph"
	"math"
	"reflect"
)

// Each edge is captured, including metadata edges omitted from JSON. Sharing is
// retained by pointer identity. Validation checks that edge before traversing
// its original descendants, so a newly introduced cycle returns a bounded error.
type typedGraphIntegrity struct{ root *typedGraphValue }
type typedGraphValue struct {
	typ      reflect.Type
	pointer  uintptr
	bits     uint64
	text     string
	children []*typedGraphValue
	keys     []reflect.Value
}
type typedGraphAddress struct {
	typ     reflect.Type
	pointer uintptr
}

func captureTypedGraphIntegrity(body *graph.TypedSourceBody) (*typedGraphIntegrity, error) {
	seen := map[typedGraphAddress]*typedGraphValue{}
	var capture func(reflect.Value, int) (*typedGraphValue, error)
	capture = func(v reflect.Value, depth int) (*typedGraphValue, error) {
		if depth > 512 {
			return nil, fmt.Errorf("registered graph exceeds bounded source domain")
		}
		out := &typedGraphValue{typ: v.Type()}
		child := func(v reflect.Value) error {
			x, e := capture(v, depth+1)
			if e == nil {
				out.children = append(out.children, x)
			}
			return e
		}
		switch v.Kind() {
		case reflect.Pointer:
			out.pointer = v.Pointer()
			if v.IsNil() {
				break
			}
			key := typedGraphAddress{v.Type(), v.Pointer()}
			if old := seen[key]; old != nil {
				return old, nil
			}
			seen[key] = out
			if e := child(v.Elem()); e != nil {
				return nil, e
			}
		case reflect.Interface:
			if !v.IsNil() {
				if e := child(v.Elem()); e != nil {
					return nil, e
				}
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if e := child(v.Field(i)); e != nil {
					return nil, e
				}
			}
		case reflect.Slice:
			// Graph-node sequences retain their actual membership storage. Scalar
			// metadata lists (for example lexical IDs) are value contracts:
			// restoring equal ordered contents need not preserve backing memory.
			if v.Type().Elem() == reflect.TypeFor[*graph.Expression]() || v.Type().Elem() == reflect.TypeFor[*graph.Statement]() {
				out.pointer = v.Pointer()
			}
			fallthrough
		case reflect.Array:
			for i := 0; i < v.Len(); i++ {
				if e := child(v.Index(i)); e != nil {
					return nil, e
				}
			}
		case reflect.Map:
			if v.IsNil() {
				out.bits = 1
			}
			out.keys = v.MapKeys()
			for _, k := range out.keys {
				if e := child(v.MapIndex(k)); e != nil {
					return nil, e
				}
			}
		case reflect.Bool:
			if v.Bool() {
				out.bits = 1
			}
		case reflect.String:
			out.text = v.String()
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			out.bits = uint64(v.Int())
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			out.bits = v.Uint()
		case reflect.Float32, reflect.Float64:
			out.bits = math.Float64bits(v.Float())
		default:
			return nil, fmt.Errorf("unclassified registered graph field %s", v.Type())
		}
		return out, nil
	}
	root, e := capture(reflect.ValueOf(body), 0)
	if e != nil {
		return nil, e
	}
	return &typedGraphIntegrity{root}, nil
}
func (i *typedGraphIntegrity) validate(body *graph.TypedSourceBody) error {
	if i == nil || i.root == nil {
		return fmt.Errorf("original graph integrity required")
	}
	seen := map[*typedGraphValue]bool{}
	var validate func(*typedGraphValue, reflect.Value) bool
	validate = func(s *typedGraphValue, v reflect.Value) bool {
		if !v.IsValid() || v.Type() != s.typ {
			return false
		}
		switch v.Kind() {
		case reflect.Pointer:
			if v.Pointer() != s.pointer {
				return false
			}
			if v.IsNil() || seen[s] {
				return true
			}
			seen[s] = true
			return validate(s.children[0], v.Elem())
		case reflect.Interface:
			if v.IsNil() {
				return len(s.children) == 0
			}
			return len(s.children) == 1 && validate(s.children[0], v.Elem())
		case reflect.Struct:
			if v.NumField() != len(s.children) {
				return false
			}
			for j, c := range s.children {
				if !validate(c, v.Field(j)) {
					return false
				}
			}
		case reflect.Slice:
			if s.pointer != 0 && v.Pointer() != s.pointer {
				return false
			}
			fallthrough
		case reflect.Array:
			if v.Len() != len(s.children) {
				return false
			}
			for j, c := range s.children {
				if !validate(c, v.Index(j)) {
					return false
				}
			}
		case reflect.Map:
			if v.IsNil() != (s.bits == 1) || v.Len() != len(s.keys) {
				return false
			}
			for j, k := range s.keys {
				if !validate(s.children[j], v.MapIndex(k)) {
					return false
				}
			}
		case reflect.Bool:
			return v.Bool() == (s.bits != 0)
		case reflect.String:
			return v.String() == s.text
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return uint64(v.Int()) == s.bits
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			return v.Uint() == s.bits
		case reflect.Float32, reflect.Float64:
			return math.Float64bits(v.Float()) == s.bits
		default:
			return false
		}
		return true
	}
	if !validate(i.root, reflect.ValueOf(body)) {
		return fmt.Errorf("original complete body graph edge/content changed")
	}
	return nil
}
