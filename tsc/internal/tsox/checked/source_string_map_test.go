package checked

import (
	"reflect"
	"testing"
)

func TestSourceStringMapFreshValidation(t *testing.T) {
	cases := []struct {
		name    string
		initial map[string]string
		edit    func(*map[string]string)
		valid   bool
	}{
		{"unchanged", map[string]string{"a": "one"}, func(*map[string]string) {}, true},
		{"value mutation", map[string]string{"a": "one"}, func(m *map[string]string) { (*m)["a"] = "two" }, false},
		{"insert", map[string]string{"a": "one"}, func(m *map[string]string) { (*m)["b"] = "two" }, false},
		{"delete", map[string]string{"a": "one"}, func(m *map[string]string) { delete(*m, "a") }, false},
		{"same size different key", map[string]string{"a": ""}, func(m *map[string]string) { delete(*m, "a"); (*m)["b"] = "" }, false},
		{"equal replacement", map[string]string{"a": "one"}, func(m *map[string]string) { *m = map[string]string{"a": "one"} }, true},
		{"changed replacement", map[string]string{"a": "one"}, func(m *map[string]string) { *m = map[string]string{"a": "two"} }, false},
		{"nil to empty", nil, func(m *map[string]string) { *m = map[string]string{} }, false},
		{"empty to nil", map[string]string{}, func(m *map[string]string) { *m = nil }, false},
		{"nil unchanged", nil, func(*map[string]string) {}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			value := c.initial
			integrity := &sourceSyntaxIntegrity{}
			if err := integrity.capture(reflect.ValueOf(&value).Elem()); err != nil {
				t.Fatal(err)
			}
			if len(integrity.stringMaps) != 1 {
				t.Fatal("string map path not exercised")
			}
			if err := integrity.validate(); err != nil {
				t.Fatal(err)
			}
			c.edit(&value)
			if err := integrity.validate(); (err == nil) != c.valid {
				t.Fatalf("valid=%v, error=%v", c.valid, err)
			}
		})
	}
}
