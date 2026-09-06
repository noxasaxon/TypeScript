package extract

import (
	"reflect"
	"testing"

	"github.com/microsoft/typescript-go/tsox/graph"
)

func TestReexportsPreserveOriginalBindingsShapesAndInitialization(t *testing.T) {
	result := ExtractFiles("main.ts", map[string]string{
		"main.ts":   `console.log("entry"); import { live, object, read, bump } from "./barrel.ts"; import type { Box as View } from "./barrel.ts"; const alias: View = object; console.log(live); console.log(object.value); console.log(read()); bump(); export { live as count, object, read as handle }; export const direct = 9; export function own(): number { return direct; } export type { Box as handleType } from "./barrel.ts";`,
		"barrel.ts": `console.log("barrel"); export { local as live, box as object, get as read, bump } from "./local.ts"; export type { Box } from "./local.ts"; export { type Inline } from "./inline.ts"; export { type Mixed, value as mixed } from "./mixed.ts"; export {} from "./empty.ts";`,
		"local.ts":  `console.log("local"); import { count as local, box, read as get, bump } from "./state.ts"; export { local, box, get, bump }; export type { Box } from "./state.ts"; export type { Erased } from "./erased.ts";`,
		"state.ts":  `console.log("state"); export interface Box { value: number; } export let count = 1; export let box: Box = { value: 2 }; export function read(): number { return count; } export function bump(): void { count = count + 1; box.value = box.value + 1; }`,
		"erased.ts": `console.log("erased"); export interface Erased { other: number; }`,
		"inline.ts": `console.log("inline"); export interface Inline { other: number; }`,
		"mixed.ts":  `console.log("mixed"); export interface Mixed { other: number; } export const value = 3;`,
		"empty.ts":  `console.log("empty");`,
	})
	if result.Program == nil {
		t.Fatal(result.Diagnostics)
	}
	declarations := map[string]*graph.Statement{}
	var prints []string
	var variableCount int
	for _, statement := range result.Program.Statements {
		if statement.Kind == graph.StatementVariable || statement.Kind == graph.StatementFunction {
			declarations[statement.Name] = statement
		}
		if statement.Kind == graph.StatementVariable {
			variableCount++
		}
		if statement.Kind == graph.StatementPrint && statement.Arguments[0].Kind == graph.ExpressionString {
			prints = append(prints, statement.Arguments[0].String)
		}
	}
	if want := []string{"state", "local", "inline", "mixed", "empty", "barrel", "entry"}; !reflect.DeepEqual(prints, want) {
		t.Fatalf("initialization %v, want %v", prints, want)
	}
	if variableCount != 5 {
		t.Fatalf("re-exports introduced alias variables: %d", variableCount)
	}
	box, count, read := declarations["box"], declarations["count"], declarations["read"]
	if declarations["alias"].Value.Binding != box.Binding || declarations["alias"].Type.Shape != box.Type.Shape {
		t.Fatal("local imported alias lost binding or ShapeID")
	}
	if read.Body[0].Value.Binding != count.Binding {
		t.Fatal("live scalar binding changed")
	}
	exports := map[string]graph.BindingID{}
	for _, item := range result.Program.EntryExports {
		exports[item.Name] = item.Binding
		if item.Position.SourcePath != "" || item.Position.Line != 1 || item.Position.Column < 1 {
			t.Fatalf("entry export provenance: %+v", item)
		}
	}
	want := map[string]graph.BindingID{"count": count.Binding, "object": box.Binding, "handle": read.Binding, "direct": declarations["direct"].Binding, "own": declarations["own"].Binding}
	if !reflect.DeepEqual(exports, want) {
		t.Fatalf("entry exports %+v, want %+v", exports, want)
	}
}

func TestReexportEntryMetadataIncludesDirectDestructuredBindings(t *testing.T) {
	result := ExtractFiles("main.ts", map[string]string{"main.ts": `interface Box { value: number; } const box: Box = { value: 1 }; export const { value: renamed } = box; export { renamed as alias }; export {};`})
	if result.Program == nil {
		t.Fatal(result.Diagnostics)
	}
	exports := result.Program.EntryExports
	if len(exports) != 2 || exports[0].Name != "renamed" || exports[1].Name != "alias" || exports[0].Binding == 0 || exports[0].Binding != exports[1].Binding {
		t.Fatalf("destructured exports: %+v", exports)
	}
}

func TestReexportCheckerErrorsAndCyclesKeepOriginalSource(t *testing.T) {
	cases := []struct{ name, barrel, dependency, construct string }{
		{"missing export", `export { missing } from "./dep.ts";`, `export const value = 1;`, "TypeScriptDiagnostic"},
		{"type dependency checked", `export type { Shape } from "./dep.ts";`, `export interface Shape { value: number; } const bad: number = "bad";`, "TypeScriptDiagnostic"},
		{"runtime cycle", `export {} from "./dep.ts";`, `export {} from "./barrel.ts";`, "ModuleCycle"},
		{"type cycle", `export type { Shape } from "./dep.ts";`, `export type { Shape } from "./barrel.ts";`, "ModuleCycle"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := ExtractFiles("main.ts", map[string]string{"main.ts": `export {} from "./barrel.ts";`, "barrel.ts": "\n" + test.barrel, "dep.ts": "\n" + test.dependency})
			path := "dep.ts"
			if test.name == "missing export" {
				path = "barrel.ts"
			}
			if result.Program != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Construct != test.construct || result.Diagnostics[0].SourcePath != path || result.Diagnostics[0].Position.Line != 2 {
				t.Fatalf("diagnostic: %+v", result)
			}
		})
	}
}

func TestReexportListsRemainFencedInLegacySingleSourceExtraction(t *testing.T) {
	result := Extract("main.ts", `const value = 1; export { value };`)
	if result.Program != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Construct != "ExportDeclaration" {
		t.Fatalf("legacy export boundary changed: %+v", result)
	}
}
