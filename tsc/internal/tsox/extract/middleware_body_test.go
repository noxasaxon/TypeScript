package extract

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestMiddlewareActualBodyGraph(t *testing.T) {
	root := sourcefixture.Get(t, "optional-flow")
	portfolio := sourcefixture.Get(t, "portfolio")
	if root == "" || portfolio == "" {
		t.Fatal("explicit frozen input/output required")
	}
	p, ds := checked.ReadProjectWithOptions(filepath.Join(portfolio, "tsconfig.json"), "web-api/route.ts", checked.ProjectOptions{DependencyTypes: checked.DependencyTypesInferJS})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	program, ds := p.Check()
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	inputs, err := checked.CallableBodyInputs(program, "handle")
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 3 {
		t.Fatal(len(inputs))
	}
	for _, input := range inputs {
		out := ExtractCallableBody(input)
		data, marshalErr := json.MarshalIndent(out, "", "  ")
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%d-%s.json", input.Instance, filepath.Base(ast.GetSourceFileOfNode(input.Node).FileName()))), data, 0600); err != nil {
			t.Fatal(err)
		}
		if len(out.Diagnostics) > 0 {
			t.Error(out.Diagnostics)
		}
		if out.Flow == nil || len(out.Obligations) == 0 {
			t.Fatal("missing graph or unresolved admission obligations")
		}
		if filepath.Base(ast.GetSourceFileOfNode(input.Node).FileName()) == "auth.ts" {
			if len(out.Stages) != 0 || len(out.Calls) != 1 || len(out.Captures) != 1 || len(out.SourceObligations) != 0 {
				t.Fatal("auth arrow lost actual instance call", out)
			}
			call := out.Calls[0].Expression
			if call.Kind != graph.ExpressionCallAsync || call.Callee.Kind != graph.ExpressionAsyncCallable || call.Callee.Binding != out.Captures[0].Binding || len(call.Arguments) != 2 {
				t.Fatal("callee/arguments lost", call)
			}
		} else {
			if len(out.Stages) != 1 || out.Stages[0].Await.Producer == nil || out.Stages[0].Await.Producer.Kind != graph.ProducerBodyJSON {
				t.Fatal("actual BodyJSON suspension missing")
			}
			if len(out.SourceObligations) != 0 {
				t.Fatal("aggregate source obligation remains", out.SourceObligations)
			}
			aggregate := false
			for _, shape := range out.Shapes {
				if len(shape.Fields) != 3 || shape.Fields[0].Name != "total" || shape.Fields[1].Name != "tasks" || shape.Fields[2].Name != "completed" {
					continue
				}
				aggregate = true
				tasks := shape.Fields[1].Type
				if tasks.Kind != graph.TypeArray || tasks.Element == nil {
					t.Fatal("listTasks array layout lost")
				}
				for _, nested := range out.Shapes {
					if nested.ID == tasks.Element.Shape {
						fields := map[string]graph.Field{}
						for _, field := range nested.Fields {
							fields[field.Name] = field
						}
						for _, name := range []string{"owner", "note"} {
							field := fields[name]
							if field.Type.Kind != graph.TypeString || !field.Type.Optional {
								t.Fatal("inferred optional field lost", name, field)
							}
						}
						if fields["tags"].Type.Kind != graph.TypeArray {
							t.Fatal("tags array layout lost")
						}
					}
				}
			}
			if !aggregate {
				t.Fatal("actual inferred listTasks return layout absent")
			}
			found := false
			for _, s := range out.Body {
				if s.Protected != nil {
					ss := s.Protected.Try
					if len(ss) < 4 || ss[0].Kind != graph.StatementVariable || ss[0].Value.Type.Kind != graph.TypeFunction || ss[1].Kind != graph.StatementAsyncAwait || ss[2].Kind != graph.StatementVariable || ss[3].Value.Kind != graph.ExpressionCall || ss[3].Value.Callee.Binding != ss[0].Binding {
						t.Fatal("callee/await/argument/call order changed")
					}
					found = true
				}
			}
			if !found || len(out.Flow.Regions) != 2 {
				t.Fatal("protected route lost")
			}
		}
		t.Log(input.Template, len(out.Body), len(out.Stages))
	}
}

func TestMiddlewarePromiseReturnVersusAwait(t *testing.T) {
	for _, awaited := range []bool{false, true} {
		keyword := ""
		if awaited {
			keyword = "await "
		}
		source := `async function leaf(request:Request):Promise<Response>{return new Response("ok");}function wrap(next:(request:Request)=>Promise<Response>){return async(request:Request)=>{try{return ` + keyword + `next(request);}finally{console.log("cleanup");}};}export const handle=wrap(leaf);`
		p, ds := checked.New("entry.ts", map[string]string{"entry.ts": source})
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		inputs, err := checked.CallableBodyInputs(p, "handle")
		if err != nil {
			t.Fatal(err)
		}
		out := ExtractCallableBody(inputs[0])
		root := sourcefixture.Get(t, "optional-flow")
		if root == "" {
			t.Fatal("explicit output required")
		}
		dir := filepath.Join(root, fmt.Sprintf("promise-await-%t", awaited))
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		write := func(name, text string) {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
		}
		write("handler.ts", source)
		write("package.json", `{"type":"module"}`)
		write("oracle.mjs", `import{handle}from './handler.ts';const pending=handle(new Request('http://local.test/'));console.log('caller');console.log(await(await pending).text());`)
		trace, runErr := exec.Command("node", filepath.Join(dir, "oracle.mjs")).CombinedOutput()
		write("node.stdout", string(trace))
		want := "cleanup\ncaller\nok\n"
		if awaited {
			want = "caller\ncleanup\nok\n"
		}
		if runErr != nil || string(trace) != want {
			t.Fatal(runErr, string(trace))
		}
		serialized, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		write("graph.json", string(serialized))

		if len(out.Diagnostics) > 0 || len(out.SourceObligations) > 0 {
			t.Fatal(out.Diagnostics, out.SourceObligations)
		}
		if len(out.Calls) != 1 || len(out.Captures) != 1 || out.Captures[0].Cell == 0 {
			t.Fatal("source instance/capture lost", out)
		}
		if awaited {
			if len(out.Stages) != 1 || out.Stages[0].Await.Promise == nil || out.Stages[0].Await.Producer != nil {
				t.Fatal("await lost promise identity")
			}
		} else {
			if len(out.Stages) != 0 {
				t.Fatal("return promise became await")
			}
		}
		if len(out.Flow.Regions) != 2 || out.Flow.Regions[1].FinallyEntry < 0 {
			t.Fatal("cleanup region lost")
		}
		found := false
		for _, block := range out.Flow.Blocks {
			for _, s := range block.Before {
				if s.Kind == graph.StatementReturnPromise {
					if awaited {
						t.Fatal("await became adoption-only return")
					}
					found = true
					route := out.Flow.RouteCompletion(block.Region, block.Phase, graph.AsyncReturn, -1)
					if route.Target != out.Flow.Regions[1].FinallyEntry {
						t.Fatal("returned promise bypassed cleanup", route)
					}
				}
			}
		}
		if !awaited && !found {
			t.Fatal("ReturnPromise missing")
		}
	}
}

func TestMiddlewareBodySourceIdentity(t *testing.T) {
	source := `export async function handle(request:Request):Promise<Response>{return new Response("ok");}`
	p, ds := checked.New("entry.ts", map[string]string{"entry.ts": source})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	inputs, err := checked.CallableBodyInputs(p, "handle")
	if err != nil {
		t.Fatal(err)
	}
	other, ds := checked.New("entry.ts", map[string]string{"entry.ts": source})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	input := inputs[0]
	input.Program = other
	if out := ExtractCallableBody(input); out.Flow != nil || len(out.Obligations) < 4 {
		t.Fatal("independent Program AST accepted")
	}
}
