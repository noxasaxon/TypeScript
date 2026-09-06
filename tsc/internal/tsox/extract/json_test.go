package extract

import (
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/tsox/graph"
)

func TestJSONStringifyFencesNameTheLimitation(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"replacer", `console.log(JSON.stringify(1, undefined));`, "replacer"},
		{"space", `console.log(JSON.stringify(1, undefined, 2));`, "space"},
		{"dynamic", `function encode(value: any): string { return JSON.stringify(value); }`, "value domain"},
		{"null", `console.log(JSON.stringify(null));`, "null"},
		{"function", `console.log(JSON.stringify((): number => 1));`, "function"},
		{"cycle domain", `interface Link { next?: Link; } const value: Link = {}; value.next = value; console.log(JSON.stringify(value));`, "recursive"},
		{"toJSON", `interface RecordValue { toJSON: () => string; } const value: RecordValue = {toJSON: (): string => "custom"}; console.log(JSON.stringify(value));`, "toJSON"},
		{"method toJSON", `interface RecordValue { toJSON(): string; } function encode(value: RecordValue): string { return JSON.stringify(value); }`, "toJSON"},
		{"effectful argument index", `interface Item { n: number; } let values: Item[] = [{n: 1}]; function index(): number { values = [{n: 2}]; return 0; } console.log(JSON.stringify(values[index()]));`, "receiver identity"},
		{"prototype", `interface RecordValue { __proto__: number; } const value: RecordValue = {__proto__: 1}; console.log(JSON.stringify(value));`, "__proto__"},
		{"inherited intermediate property", `interface Child { n: number; } interface Proto { child?: Child; } interface Holder { __proto__: Proto; child?: Child; } const proto: Proto = {child: {n: 7}}; const holder: Holder = {__proto__: proto}; console.log(JSON.stringify(holder.child));`, "prototype lookup"},
		{"undefined array literal", `console.log(JSON.stringify([undefined]));`, "undefined"},
		{"optional array", `const values: (number | undefined)[] = [1, undefined]; console.log(JSON.stringify(values));`, "optional array element"},
		{"absent write", `interface RecordValue { a?: number; b: number; } const value: RecordValue = {b: 1}; value.a = 2; console.log(JSON.stringify(value));`, "insertion order"},
		{"unrelated same shape write", `interface RecordValue { a?: number; b: number; } const value: RecordValue = {b: 1}; const other: RecordValue = {a: 0,b: 2}; other.a = 3; console.log(JSON.stringify(value));`, "shape/field"},
		{"nested absent write", `interface Child { a?: number; b: number; } interface Parent { child: Child; } const child: Child = {b: 1}; const parent: Parent = {child: child}; function fill(item: Child): void { item.a = 2; } fill(child); console.log(JSON.stringify(parent));`, "insertion order"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := Extract("json.ts", test.source)
			if result.Program != nil || len(result.Diagnostics) != 1 {
				t.Fatalf("expected one JSON fence: %+v", result)
			}
			diagnostic := result.Diagnostics[0]
			if diagnostic.Construct != "JSONStringify" || !strings.Contains(diagnostic.Message, test.want) || diagnostic.SourcePath != "json.ts" || diagnostic.Position.Line < 1 || diagnostic.Position.Column < 1 {
				t.Fatalf("unexpected diagnostic: %+v", diagnostic)
			}
		})
	}
}

func TestJSONStringifyKeepsAbsentAndPresentUndefinedDistinct(t *testing.T) {
	source := `interface RecordValue { a?: number; b: number; } const absent: RecordValue = {b: 1}; const present: RecordValue = {a: undefined,b: 2}; console.log(JSON.stringify(absent)); console.log(JSON.stringify(present));`
	result := Extract("json.ts", source)
	if result.Program == nil {
		t.Fatal(result.Diagnostics)
	}
	var values []*graph.Expression
	walkGraphExpressions(result.Program.Statements, func(value *graph.Expression) {
		if value.Kind == graph.ExpressionObject {
			values = append(values, value)
		}
	})
	if len(values) != 2 || !values[0].Properties[0].Omitted || values[1].Properties[0].Omitted {
		t.Fatalf("absent provenance lost: %+v", values)
	}
	for _, program := range []string{
		`interface RecordValue { a?: number; b: number; } const value: RecordValue = {a: undefined,b: 1}; value.a = 2; console.log(JSON.stringify(value));`,
		`interface RecordValue { a?: number; b: number; } const value: RecordValue = {b: 1}; value.a = 2; console.log(value.a);`,
	} {
		if result := Extract("json.ts", program); result.Program == nil {
			t.Fatal(result.Diagnostics)
		}
	}
}

func TestJSONStringifyResolvesBuiltinRatherThanSpelling(t *testing.T) {
	source := `const JSON = (value: number): number => { return value + 1; }; console.log(JSON(2));`
	result := Extract("json.ts", source)
	if result.Program == nil {
		t.Fatal(result.Diagnostics)
	}
	walkGraphExpressions(result.Program.Statements, func(value *graph.Expression) {
		if value.Kind == graph.ExpressionJSONStringify {
			t.Fatal("shadow acquired JSON semantics")
		}
	})
	shadow := `interface LocalJSON { stringify: (value: number) => number; } function local(JSON: LocalJSON): number { return JSON.stringify(1); }`
	result = Extract("json.ts", shadow)
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Construct == "JSONStringify" {
		t.Fatalf("shadowed property call acquired JSON semantics: %+v", result)
	}
}

func TestJSONStringifyDiagnosesLoneSurrogateLiterals(t *testing.T) {
	for _, literal := range []string{`"\ud800"`, `"\udfff"`, "`\\ud800`", "`head${1}\\udfff`"} {
		result := Extract("json.ts", `console.log(JSON.stringify(`+literal+`));`)
		if result.Program != nil || len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "surrogate") || !strings.Contains(result.Diagnostics[0].Message, "JSON.stringify") {
			t.Fatalf("lone surrogate %s: %+v", literal, result)
		}
	}
}

func TestJSONStringifyDependencyDiagnosticKeepsOriginalSource(t *testing.T) {
	result := ExtractFiles("app/main.ts", map[string]string{
		"app/main.ts": `import { encode } from "../encode.ts"; console.log(encode());`,
		"encode.ts":   `export function encode(): string { return JSON.stringify(1, undefined, 2); }`,
	})
	if result.Program != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].SourcePath != "encode.ts" || result.Diagnostics[0].Position.SourcePath != "encode.ts" || result.Diagnostics[0].Construct != "JSONStringify" {
		t.Fatalf("dependency JSON fence: %+v", result)
	}
}

func TestJSONOrderProofRecognizesCompoundAndUpdateWrites(t *testing.T) {
	// Those source write forms are currently fenced by the frontend. The graph
	// proof still must reject them if a later slice admits them.
	for _, kind := range []graph.ExpressionKind{graph.ExpressionAssignment, graph.ExpressionUpdate} {
		shape := graph.Shape{ID: 1, Name: "Item", Fields: []graph.Field{{Name: "a", Type: graph.Type{Kind: graph.TypeNumber, Optional: true}}}}
		record := graph.Type{Kind: graph.TypeObject, Shape: 1}
		target := &graph.Expression{Kind: graph.ExpressionProperty, Name: "a", Receiver: &graph.Expression{Kind: graph.ExpressionIdentifier, Type: record}}
		write := &graph.Expression{Kind: kind, Operator: "+=", Left: target, Operand: target}
		program := &graph.Program{SourcePath: "json.ts", Shapes: []graph.Shape{shape}, Statements: []*graph.Statement{
			{Value: &graph.Expression{Kind: graph.ExpressionObject, Type: record, Properties: []graph.PropertyValue{{Name: "a", Omitted: true}}}},
			{Value: write},
			{Value: &graph.Expression{Kind: graph.ExpressionJSONStringify, Position: graph.Position{Line: 3, Column: 1}, Operand: &graph.Expression{Type: record}}},
		}}
		builder := &builder{shapes: program.Shapes}
		result := builder.finishJSON(program)
		if result.Program != nil || len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Message, "insertion order") {
			t.Fatalf("write %s lost: %+v", kind, result)
		}
	}
}

func TestGlobalJSONNumberConstantsRejectWrites(t *testing.T) {
	for _, source := range []string{`NaN = 1;`, `Infinity += 1;`, `NaN++;`, `--Infinity;`} {
		result := Extract("json.ts", source)
		if result.Program != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Construct != "ReadonlyBuiltin" {
			t.Fatalf("global write %s: %+v", source, result)
		}
	}
}
