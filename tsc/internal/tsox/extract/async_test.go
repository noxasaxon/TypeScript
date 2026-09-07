package extract

import (
	"github.com/microsoft/typescript-go/tsox/graph"
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
	if a.Await.Host == 0 || a.Await.Binding == 0 || a.Await.Position.Line != 5 || a.Await.Argument.Kind != graph.ExpressionProperty || len(a.After) != 1 {
		t.Fatalf("incomplete await graph: %+v", a.Await)
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
	if helper == nil || helper.Position.SourcePath != "helpers.ts" || r.Program.Await.Argument.Callee.Binding != helper.Binding {
		t.Fatal("imported helper binding/position lost")
	}
	if r.Program.After[0].Value.Kind != graph.ExpressionCall {
		t.Fatal("response factory call lost")
	}
}
