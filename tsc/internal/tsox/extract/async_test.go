package extract

import (
	"github.com/microsoft/typescript-go/tsox/graph"
	"reflect"
	"strings"
	"testing"
)

const asyncSource = `interface Input { key: string; }
interface Output { text: string; }
declare function hostRead(key: string): Promise<string>;
export async function handler(input: Input): Promise<Output> {
 const text=await hostRead(input.key);
 return {text:text};
}`

func TestAsyncGraphBoundary(t *testing.T) {
	r := ExtractAsyncFiles("entry.ts", map[string]string{"entry.ts": asyncSource}, "handler", "hostRead")
	if r.Program == nil {
		t.Fatal(r.Diagnostics)
	}
	a := r.Program
	if a.Stages[0].Await.Host == 0 || a.Stages[0].Await.Binding == 0 || a.Stages[0].Await.Position.Line != 5 || a.Stages[0].Await.Argument.Kind != graph.ExpressionProperty || len(a.After) != 1 {
		t.Fatalf("incomplete await graph: %+v", a.Stages[0].Await)
	}
	// The regular entrypoint must keep rejecting async source.
	ordinary := ExtractFiles("entry.ts", map[string]string{"entry.ts": asyncSource})
	if ordinary.Program != nil || len(ordinary.Diagnostics) != 1 {
		t.Fatalf("ordinary extraction admitted host source: %+v", ordinary)
	}
}
func TestAsyncHostSymbolIdentity(t *testing.T) {
	// Renaming the selected ambient symbol is fine; a local same-spelling function
	// is not that host operation, even when its return type is compatible.
	renamed := strings.ReplaceAll(asyncSource, "hostRead", "lookup")
	if r := ExtractAsyncFiles("entry.ts", map[string]string{"entry.ts": renamed}, "handler", "lookup"); r.Program == nil {
		t.Fatal(r.Diagnostics)
	}
	shadowed := strings.Replace(asyncSource, " const text=", " const hostRead = (key: string): string => { return key; }; const text=", 1)
	r := ExtractAsyncFiles("entry.ts", map[string]string{"entry.ts": shadowed}, "handler", "hostRead")
	if r.Program != nil || len(r.Diagnostics) != 1 || r.Diagnostics[0].Construct != "AsyncHost" {
		t.Fatalf("shadowed host admitted: %+v", r)
	}
}

func TestAsyncImportedSynchronousHelpers(t *testing.T) {
	sources := map[string]string{
		"entry.ts": `import type {Input,Output} from "./helpers.ts"; import {key,respond} from "./helpers.ts";
declare function hostRead(key: string): Promise<string>;
export async function handler(input: Input): Promise<Output> {const text=await hostRead(key(input));return respond(text);}`,
		"helpers.ts": `export interface Input { key: string; } export interface Output { text: string; }
export function key(input: Input): string { if(input.key==="bad"){throw "key";} return input.key; }
export function respond(text: string): Output { return {text:text}; }`,
	}
	r := ExtractAsyncFiles("entry.ts", sources, "handler", "hostRead")
	if r.Program == nil {
		t.Fatal(r.Diagnostics)
	}
	var helper *graph.Statement
	for _, statement := range r.Program.Module.Statements {
		if statement.Name == "key" {
			helper = statement
		}
	}
	if helper == nil || helper.Position.SourcePath != "helpers.ts" || r.Program.Stages[0].Await.Argument.Callee.Binding != helper.Binding {
		t.Fatal("imported helper binding/position lost")
	}
	if r.Program.After[0].Value.Kind != graph.ExpressionCall {
		t.Fatal("response factory call lost")
	}
}

func TestAsyncSequentialStageGraph(t *testing.T) {
	source := strings.Replace(asyncSource, "return {text:text};", `const next=text+".data"; const second=await hostRead(next); const third=await hostRead(second); return {text:third};`, 1)
	r := ExtractAsyncFiles("entry.ts", map[string]string{"entry.ts": source}, "handler", "hostRead")
	if r.Program == nil {
		t.Fatal(r.Diagnostics)
	}
	stages := r.Program.Stages
	if len(stages) != 3 || len(stages[1].Before) != 1 || len(stages[2].Before) != 0 {
		t.Fatalf("lost sequential stages: %+v", stages)
	}
	if stages[1].Await.Argument.Binding != stages[1].Before[0].Binding || stages[2].Await.Argument.Binding != stages[1].Await.Binding {
		t.Fatal("dependent await bindings lost")
	}
	for _, stage := range stages {
		if stage.Await.Host != stages[0].Await.Host || stage.Await.Position.Line < 1 {
			t.Fatal("host identity/position lost")
		}
	}
}

func TestAsyncTypedFulfillmentUsesImportedHelperResult(t *testing.T) {
	sources := map[string]string{
		"host.ts":   `export declare function hostRead(key:string):Promise<string>;`,
		"helper.ts": `import{hostRead}from"./host.ts";export interface Data{key:string;}export async function load(key:string):Promise<Data>{const text=await hostRead(key);return{key:text};}`,
		"entry.ts":  `import{load}from"./helper.ts";interface Input{key:string;}interface Output{text:string;}export async function handler(input:Input):Promise<Output>{const data=await load(input.key);return{text:data.key};}`,
	}
	result := ExtractAsyncFiles("entry.ts", sources, "handler", "hostRead")
	if result.Program == nil {
		t.Fatal(result.Diagnostics)
	}
	a := result.Program
	if len(a.Helpers) != 1 || !reflect.DeepEqual(a.Stages[0].Await.Type, a.Helpers[0].Program.Result) {
		t.Fatalf("await lost actual imported fulfillment type: %+v", a.Stages)
	}
	op := a.Stages[0].Await
	if op.Type.Kind != graph.TypeObject || op.Type.Shape == 0 || !reflect.DeepEqual(op.Argument.Type, op.Type) || op.Position.Line < 1 || op.Position.Column < 1 {
		t.Fatalf("incomplete typed await: %+v", op)
	}
	if a.Helpers[0].Program.Stages[0].Await.Type.Kind != graph.TypeString {
		t.Fatal("host fulfillment must remain string")
	}
}
