package extract

import "testing"

// These are the former temporary fences. Receiver scheduling is now shared
// with ordinary accesses rather than restricting JSON or destructuring.
func TestEffectfulIndexedSourcesAccepted(t *testing.T) {
	cases := []struct{ name, source string }{
		{"JSON argument", `interface Item { n: number; } let values: Item[] = [{n: 1}]; function index(): number { values = [{n: 2}]; return 0; } console.log(JSON.stringify(values[index()]));`},
		{"destructuring", `interface Item { n: number; } let values: Item[] = [{n: 1}]; function index(): number { values = [{n: 2}]; return 0; } const {n} = values[index()];`},
		{"nested destructuring", `interface Item { n: number; } interface Holder { item: Item; } let values: Holder[] = [{item: {n: 1}}]; function index(): number { values = [{item: {n: 2}}]; return 0; } const {n} = values[index()].item;`},
		{"empty destructuring", `interface Item { n: number; } let values: Item[] = [{n: 1}]; function index(): number { values = [{n: 2}]; return 0; } const {} = values[index()];`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if result := Extract("indexed.ts", test.source); result.Program == nil {
				t.Fatal(result.Diagnostics)
			}
		})
	}
}
