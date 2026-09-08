package extract

import (
	"github.com/microsoft/typescript-go/tsox/graph"
	"testing"
)

const sourceErrorTypes = `interface Input{key:string;}interface Output{body:string;}declare function hostRead(key:string):Promise<string>;`

func TestSourceErrorIntrinsicAndOriginalCatchStorage(t *testing.T) {
	for _, name := range []string{"Error", "TypeError", "RangeError", "ReferenceError", "SyntaxError", "EvalError", "URIError"} {
		t.Run(name, func(t *testing.T) {
			source := sourceErrorTypes + `export async function handler(input:Input):Promise<Output>{try{const value=await hostRead(input.key);throw new ` + name + `(value);}catch(error){if(error instanceof ` + name + `)return {body:error.message};throw error;}}`
			result := ExtractAsyncFiles("entry.ts", map[string]string{"entry.ts": source}, "handler", "hostRead")
			if result.Program == nil {
				t.Fatal(result.Diagnostics)
			}
			a := result.Program
			if a.CatchBinding == nil || a.CatchBinding.Type.Kind != graph.TypeThrown {
				t.Fatal("catch storage was narrowed from checked use")
			}
			thrown := a.After[0].Value
			if thrown.Kind != graph.ExpressionErrorConstruct || thrown.Name != name || len(thrown.Arguments) != 1 {
				t.Fatalf("constructor identity lost: %+v", thrown)
			}
			condition := a.Catch[0].Condition
			field := a.Catch[0].Then[0].Value.Properties[0].Value
			if condition.Kind != graph.ExpressionErrorInstanceOf || condition.Name != name || condition.Receiver.Type.Kind != graph.TypeThrown || field.Kind != graph.ExpressionErrorField || field.Receiver.Type.Kind != graph.TypeThrown {
				t.Fatal("guard or property replaced original caught storage")
			}
		})
	}
}
func TestSourceErrorShadowAndArgumentOrder(t *testing.T) {
	source := sourceErrorTypes + `function Error(value:string):string{return "shadow:"+value;}export async function handler(input:Input):Promise<Output>{const value=await hostRead(input.key);throw Error(value);}`
	result := ExtractAsyncFiles("entry.ts", map[string]string{"entry.ts": source}, "handler", "hostRead")
	if result.Program == nil {
		t.Fatal(result.Diagnostics)
	}
	if value := result.Program.After[0].Value; value.Kind != graph.ExpressionCall || value.Type.Kind != graph.TypeString {
		t.Fatalf("shadow acquired intrinsic semantics: %+v", value)
	}
	source = sourceErrorTypes + `function step(value:string):string{console.log(value);return value;}export async function handler(input:Input):Promise<Output>{const value=await hostRead(input.key);
// @ts-ignore: extra primitive arguments still evaluate at runtime
throw Error(step("first"),step("second"),step("third"));}`
	result = ExtractAsyncFiles("entry.ts", map[string]string{"entry.ts": source}, "handler", "hostRead")
	if result.Program == nil {
		t.Fatal(result.Diagnostics)
	}
	value := result.Program.After[0].Value
	if len(value.Arguments) != 3 {
		t.Fatal("ignored arguments erased")
	}
	for i, want := range []string{"first", "second", "third"} {
		if got := value.Arguments[i].Arguments[0].String; got != want {
			t.Fatalf("argument %d: %s", i, got)
		}
	}
}
