package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/typescript-go/tsox/graph"
)

// Keep the private checked-emitter graph attributable to actual current source
// extraction. Explicit regeneration writes beside the source in this fork;
// normal CI compares and never silently refreshes stale graph evidence.
func TestNumericJSONFixturesMatchCurrentExtraction(t *testing.T) {
	for _, name := range []string{"predicates", "strings", "properties", "unchecked"} {
		directory := filepath.Join("testdata", "numeric-json")
		source, err := os.ReadFile(filepath.Join(directory, name+".ts"))
		if err != nil {
			t.Fatal(err)
		}
		result := extractSource(name+".ts", string(source), true)
		if result.Program == nil {
			t.Fatalf("%s: %+v", name, result.Diagnostics)
		}
		encoded, err := json.MarshalIndent(result.Program, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		encoded = append(encoded, '\n')
		path := filepath.Join(directory, name+".json")
		if os.Getenv("TSOX_UPDATE_NUMERIC_FIXTURES") == "1" {
			if err = os.WriteFile(path, encoded, 0644); err != nil {
				t.Fatal(err)
			}
		}
		actual, err := os.ReadFile(path)
		if err != nil || string(actual) != string(encoded) {
			t.Fatalf("%s private graph differs from current extraction; explicitly regenerate numeric fixtures: %v", name, err)
		}
	}
}

func TestNumericBuiltinsPreserveUnknownAndRejectObjectCoercion(t *testing.T) {
	program := jsonValueGraph(t, `function finite(value: unknown): boolean { return Number.isFinite(value); } function integer(value: unknown): boolean { return Number.isInteger(value); }`)
	for index, kind := range []graph.ExpressionKind{graph.ExpressionNumberIsFinite, graph.ExpressionNumberIsInteger} {
		value := program.Statements[index].Body[0].Value
		if value.Kind != kind || len(value.Arguments) != 1 || value.Arguments[0].Type.Kind != graph.TypeUnknown || value.Arguments[0].Kind != graph.ExpressionIdentifier {
			t.Fatalf("predicate coerced unknown argument: %+v", value)
		}
	}
	for _, source := range []string{
		`const number = new Number("1");`,
		`interface Value { text: string; } const value: Value = {text: "1"}; console.log(Number(value));`,
		`function value(input: unknown): number { return Number(input); }`,
	} {
		result := extractSource("numeric.ts", source, true)
		if result.Program != nil || len(result.Diagnostics) == 0 || result.Diagnostics[0].Construct != "NumberBuiltin" || result.Diagnostics[0].Position.Line == 0 {
			t.Fatalf("missing positioned numeric fence for %s: %+v", source, result)
		}
	}
}

func TestNumericBuiltinMembersDoNotCaptureShadowedObjects(t *testing.T) {
	for _, member := range []string{"isFinite", "isInteger"} {
		source := "interface Local { " + member + ": (value: number) => boolean; } function local(Number: Local): boolean { return Number." + member + "(1); }"
		result := extractSource("numeric-shadow.ts", source, true)
		if result.Program != nil {
			walkGraphExpressions(result.Program.Statements, func(value *graph.Expression) {
				if value.Kind == graph.ExpressionNumberIsFinite || value.Kind == graph.ExpressionNumberIsInteger {
					t.Fatalf("shadow acquired builtin semantics: %+v", value)
				}
			})
		}
		if len(result.Diagnostics) != 1 || result.Diagnostics[0].Construct == "NumberBuiltin" {
			t.Fatalf("ordinary shadow call changed admission: %+v", result)
		}
	}
}
