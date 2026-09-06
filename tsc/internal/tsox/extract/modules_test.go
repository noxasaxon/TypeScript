package extract

import (
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/tsox/graph"
)

func TestModulesPreserveBindingShapeIdentityAndInitializationOrder(t *testing.T) {
	result := ExtractFiles("app/main.ts", map[string]string{
		"app/main.ts": `console.log("entry"); import type { OnlyType } from "../types.ts"; import { read as left } from "../left.ts"; import { read as right } from "../right.ts"; import { count as live, box as object } from "../state.ts"; console.log(live); console.log(object.value); console.log(left() + right());`,
		"types.ts":    `console.log("type-only"); export interface OnlyType { unused: number; }`,
		"state.ts":    `console.log("state"); export let count = 1; export interface Box { value: number; } export let box: Box = { value: 2 };`,
		"left.ts":     `import { count as n } from "./state.ts"; console.log("left"); export function read(): number { return n; }`,
		"right.ts":    `import { count as n } from "./state.ts"; console.log("right"); export function read(): number { return n; }`,
	})
	if result.Program == nil {
		t.Fatalf("extract: %v", result.Diagnostics)
	}
	var prints []string
	var count, box graph.BindingID
	var live, object *graph.Expression
	for _, statement := range result.Program.Statements {
		if statement.Kind == graph.StatementVariable {
			if statement.Name == "count" {
				count = statement.Binding
			}
			if statement.Name == "box" {
				box = statement.Binding
			}
		}
		if statement.Kind == graph.StatementPrint {
			expression := statement.Arguments[0]
			if expression.Kind == graph.ExpressionString {
				prints = append(prints, expression.String)
			}
			if expression.Kind == graph.ExpressionIdentifier {
				live = expression
			}
			if expression.Kind == graph.ExpressionProperty {
				object = expression.Receiver
			}
		}
	}
	if strings.Join(prints, ",") != "state,left,right,entry" {
		t.Fatalf("runtime order: %v", prints)
	}
	if live == nil || live.Binding != count || object == nil || object.Binding != box || count == box {
		t.Fatalf("lost alias identity: live=%+v count=%d object=%+v box=%d", live, count, object, box)
	}
	if len(result.Program.Shapes) != 1 || object.Type.Shape != result.Program.Shapes[0].ID {
		t.Fatalf("lost shared shape identity: %+v", result.Program.Shapes)
	}
	if result.Program.Shapes[0].Position.SourcePath != "state.ts" {
		t.Fatalf("lost declaration provenance: %+v", result.Program.Shapes[0])
	}
}

func TestModuleFencesNameUnsupportedFormsAndOriginalSource(t *testing.T) {
	cases := []struct{ name, source, construct string }{
		{"default import", `import item from "./dep.ts";`, "ModuleImport"},
		{"namespace import", `import * as item from "./dep.ts";`, "ModuleImport"},
		{"inline type", `import { type Item } from "./dep.ts";`, "ModuleImport"},
		{"package", `import { value } from "a-package";`, "ModuleSpecifier"},
		{"builtin", `import { readFile } from "node:fs";`, "ModuleSpecifier"},
		{"extensionless", `import { value } from "./dep";`, "ModuleSpecifier"},
		{"URL encoding", `import { value } from "./%64ep.ts";`, "ModuleSpecifier"},
		{"declaration source", `import { value } from "./dep.d.ts";`, "ModuleSpecifier"},
		{"attributes", `import { value } from "./dep.ts" with { type: "json" };`, "ModuleImport"},
		{"re-export", `export { value } from "./dep.ts";`, "ModuleExport"},
		{"star export", `export * from "./dep.ts";`, "ModuleExport"},
		{"local export list", `const value = 1; export { value };`, "ModuleExport"},
		{"default export", `export default 1;`, "ModuleExport"},
		{"import equals", `import item = require("./dep.ts");`, "ModuleImport"},
		{"dynamic import", `const pending = import("./dep.ts");`, "DynamicImport"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := ExtractFiles("main.ts", map[string]string{"main.ts": `import "./bad.ts";`, "bad.ts": "\n" + test.source, "dep.ts": `export const value = 1; export interface Item { value: number; }`})
			if result.Program != nil || len(result.Diagnostics) != 1 {
				t.Fatalf("expected fence: %+v", result)
			}
			diagnostic := result.Diagnostics[0]
			if diagnostic.Construct != test.construct || diagnostic.SourcePath != "bad.ts" || diagnostic.Position.Line != 2 {
				t.Fatalf("fence lost form or provenance: %+v", diagnostic)
			}
		})
	}
}

func TestModuleCyclesMissingFilesAndCheckerErrors(t *testing.T) {
	cases := []struct {
		name            string
		files           map[string]string
		construct, file string
		line            int
	}{
		{"runtime cycle", map[string]string{"main.ts": `import "./other.ts";`, "other.ts": "\nimport \"./main.ts\";"}, "ModuleCycle", "other.ts", 2},
		{"type cycle", map[string]string{"main.ts": `import type { B } from "./other.ts"; export interface A { value: number; }`, "other.ts": "\nimport type { A } from \"./main.ts\"; export interface B { value: number; }"}, "ModuleCycle", "other.ts", 2},
		{"missing", map[string]string{"main.ts": "\nimport \"./missing.ts\";"}, "ModuleSource", "main.ts", 2},
		{"checker", map[string]string{"main.ts": `import { value } from "./other.ts";`, "other.ts": "\nexport const value: number = \"bad\";"}, "TypeScriptDiagnostic", "other.ts", 2},
		{"unsupported body", map[string]string{"main.ts": `import "./other.ts";`, "other.ts": "\nconst value = new URL(\"https://example.com\");"}, "NewExpression", "other.ts", 2},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := ExtractFiles("main.ts", test.files)
			if result.Program != nil || len(result.Diagnostics) != 1 {
				t.Fatalf("expected fence: %+v", result)
			}
			d := result.Diagnostics[0]
			if d.Construct != test.construct || d.SourcePath != test.file || d.Position.Line != test.line {
				t.Fatalf("diagnostic: %+v", d)
			}
		})
	}
}
