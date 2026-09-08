package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestMiddlewareSourceHelperBodies(t *testing.T) {
	root := sourcefixture.Get(t, "optional-flow")
	portfolio := sourcefixture.Get(t, "portfolio")
	if root == "" || portfolio == "" {
		t.Fatal("explicit input/output required")
	}
	project, ds := checked.ReadProjectWithOptions(filepath.Join(portfolio, "tsconfig.json"), "web-api/route.ts", checked.ProjectOptions{DependencyTypes: checked.DependencyTypesInferJS})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	p, ds := project.Check()
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	out := sourceHelperBodies(p)
	data, e := json.MarshalIndent(out, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "helpers.json"), data, 0600); e != nil {
		t.Fatal(e)
	}
	if len(out.Diagnostics) > 0 {
		t.Fatal(out.Diagnostics)
	}
	names := map[string]bool{}
	for _, fn := range out.Program.Statements {
		names[fn.Name] = true
	}
	for _, name := range []string{"authenticate", "createTask", "listTasks", "validateTask"} {
		if !names[name] {
			t.Fatal("actual helper omitted", name)
		}
	}
	if len(out.LayoutTransfers) != 1 || out.LayoutTransfers[0].Destination.Shape == out.LayoutTransfers[0].Value.Type.Shape || out.LayoutTransfers[0].Obligation == "" {
		t.Fatal("structural alias transfer was hidden", out.LayoutTransfers)
	}
	callbacks, aliases, omittedDetail, presentOptional, transfers := 0, 0, 0, 0, 0
	walkGraphExpressions(out.Program.Statements, func(x *graph.Expression) {
		source, ok := out.Expressions[x]
		if (x.Kind == graph.ExpressionArrow || x.Kind == graph.ExpressionObject) && !ok {
			t.Fatal("actual constructor/callback source lost", x.Kind)
		}
		if x.Kind == graph.ExpressionArrow {
			callbacks++
			if source.Node.Kind != ast.KindArrowFunction || len(x.Body) != 1 || x.Body[0].Kind != graph.StatementReturn {
				t.Fatal("callback source/return lost")
			}
		}
		if x.Kind == graph.ExpressionAssignment && x.Right == out.LayoutTransfers[0].Value {
			transfers++
		}
		if x.Kind == graph.ExpressionProperty && x.Name == "tags" {
			aliases++
		}
		if x.Kind == graph.ExpressionObject {
			for _, property := range x.Properties {
				if property.Name == "detail" && property.Omitted {
					omittedDetail++
				}
				if (property.Name == "owner" || property.Name == "note") && property.Value.OptionalChain && !property.Omitted {
					presentOptional++
				}
			}
		}
	})
	if callbacks != 3 || aliases < 2 || omittedDetail < 1 || presentOptional != 2 || transfers != 1 {
		t.Fatal("actual callback/alias/presence edges lost", callbacks, aliases, omittedDetail, presentOptional, transfers)
	}
	inputs, e := checked.CallableBodyInputs(p, "handle")
	if e != nil {
		t.Fatal(e)
	}
	linked := map[string]bool{}
	for _, input := range inputs {
		body := ExtractCallableBody(input)
		if len(body.Diagnostics) > 0 {
			t.Fatal(body.Diagnostics)
		}
		for _, sources := range body.BindingSources {
			for _, source := range sources {
				for _, helper := range out.Functions {
					if source.Node == helper.Node {
						if source.ID != helper.ID {
							t.Fatal("actual source identity changed")
						}
						linked[helper.ID] = true
					}
				}
			}
		}
	}
	if len(linked) != 4 {
		t.Fatal("helper/async call source linkage incomplete", len(linked))
	}
	if len(out.DeferredCallables) != 3 || len(out.ModuleOperations) == 0 || len(out.Obligations) == 0 {
		t.Fatal("source initialization/callable obligations lost")
	}
}

func TestMiddlewareHelperSourceControls(t *testing.T) {
	for _, test := range []struct{ name, source string }{
		{"concise", `export function run(items:number[]){return items.map(x=>({value:x,nested:{text:"x"}}));}`},
		{"locals", `export function run(note:string|undefined){const nested={note};const alias=nested;return {nested,alias};}`},
		{"capture", `export function run(items:number[]){let count=0;const values=items.map(x=>({value:(count+=x),note:x>1?"x":undefined}));return {values,count};}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := sourcefixture.Get(t, "optional-flow")
			if root == "" {
				t.Fatal("explicit fixture output required")
			}
			dir := filepath.Join(root, "controls", test.name)
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "control.ts"), []byte(test.source), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"type":"module"}`), 0600); err != nil {
				t.Fatal(err)
			}
			p, ds := checked.New("control.ts", map[string]string{"control.ts": test.source})
			if len(ds) > 0 {
				t.Fatal(ds)
			}
			out := sourceHelperBodies(p)
			if len(out.Diagnostics) > 0 {
				t.Fatal(out.Diagnostics)
			}
			if len(out.Program.Statements) != 1 {
				t.Fatal("actual function lost")
			}
			if len(out.Expressions) == 0 || len(out.Obligations) == 0 {
				t.Fatal("source identity or domain obligation lost")
			}
		})
	}
}

func TestMiddlewareHelperSourceBoundaries(t *testing.T) {
	for name, source := range map[string]string{
		"undefined-only":  `export function run(){return {note:undefined};}`,
		"async-callback":  `export function run(items:number[]){return items.map(async x=>({value:x}));}`,
		"accessor":        `export function run(){const x={get value(){return 1}};return x;}`,
		"computed":        `export function run(){const key="value";const x={[key]:1};return x;}`,
		"prototype":       `export function run(){const x={__proto__:{value:1}};return x;}`,
		"assertion":       `export function run(raw:unknown){return raw as {value:string};}`,
		"different-order": `interface A{a:number;b:string}interface B{b:string;a:number}export function run(a:A,b:B){a=b;return a;}`,
		"extra-fields":    `interface A{a:number}interface B{a:number;b:string}export function run(a:A,b:B){a=b;return a;}`,
	} {
		t.Run(name, func(t *testing.T) {
			p, ds := checked.New("boundary.ts", map[string]string{"boundary.ts": source})
			if len(ds) > 0 {
				t.Fatal(ds)
			}
			out := sourceHelperBodies(p)
			if len(out.Diagnostics) == 0 {
				t.Fatal("unresolved source semantics erased")
			}
		})
	}
	// The new hooks are private and do not change ordinary public admission.
	if got := Extract("public.ts", `function run(xs:number[]){return xs.map(x=>({value:x}));}`); got.Program != nil {
		t.Fatal("public gate opened")
	}
}
