package extract

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/microsoft/typescript-go/tsox/graph"
)

func jsonValueGraph(t *testing.T, source string) *graph.Program {
	t.Helper()
	result := extractSource("json-values.ts", source, true)
	if result.Program == nil {
		t.Fatalf("internal graph extraction: %+v", result.Diagnostics)
	}
	return result.Program
}

func TestJSONValueGraphRetainsUnknownAliasStorageAndProjectionObligations(t *testing.T) {
	program := jsonValueGraph(t, `function name(text: string): string {
  const raw: unknown = JSON.parse(text);
  if (typeof raw !== "object" || raw === null || !("name" in raw) || typeof raw.name !== "string") return "invalid";
  const alias = raw;
  if (typeof alias.name !== "string") return "invalid";
  const leaf = alias.name;
  return leaf;
}`)
	function := program.Statements[0]
	for _, index := range []int{0, 2, 4} {
		if function.Body[index].Type.Kind != graph.TypeUnknown {
			t.Fatalf("checker narrowing changed actual storage: %+v", function.Body[index])
		}
	}
	parse := function.Body[0].Value
	if parse.Kind != graph.ExpressionJSONParse || parse.Type.Kind != graph.TypeUnknown || parse.Operand.Kind != graph.ExpressionIdentifier {
		t.Fatalf("parse lost runtime producer: %+v", parse)
	}
	projection := function.Body[5].Value
	if projection.Kind != graph.ExpressionUnknownProjection || projection.Type.Kind != graph.TypeString || projection.Operand.Kind != graph.ExpressionIdentifier || projection.Operand.Binding != function.Body[4].Binding || projection.Operand.Type.Kind != graph.TypeUnknown {
		t.Fatalf("projection lost unknown operand: %+v", projection)
	}
	var reads, checks int
	walkGraphExpressions(program.Statements, func(value *graph.Expression) {
		if value.UnwrapOptional || value.NonNullAssertion {
			t.Fatalf("unknown validation acquired aborting checker unwrap: %+v", value)
		}
		if value.Kind == graph.ExpressionUnknownProperty {
			reads++
			if value.Type.Kind != graph.TypeUnknown || value.Receiver.Type.Kind != graph.TypeUnknown {
				t.Fatalf("unknown property acquired checker type: %+v", value)
			}
		}
		if value.Kind == graph.ExpressionHasProperty {
			checks++
		}
	})
	if reads != 3 || checks != 1 {
		t.Fatalf("lost original checks/reads: %d/%d", reads, checks)
	}
}

func TestJSONValueGraphKeepsShortCircuitAndNullDistinct(t *testing.T) {
	program := jsonValueGraph(t, `function valid(value: unknown): boolean {
  return typeof value !== "object" || value === null || !("title" in value) || typeof value.title !== "string";
}`)
	value := program.Statements[0].Body[0].Value
	if value.Kind != graph.ExpressionBinary || value.Operator != "||" || value.Right.Kind != graph.ExpressionBinary || value.Right.Left.Kind != graph.ExpressionTypeOf || value.Right.Left.Operand.Kind != graph.ExpressionUnknownProperty {
		t.Fatalf("read lost its short-circuit branch: %+v", value)
	}
	var nulls int
	walkGraphExpressions(program.Statements, func(value *graph.Expression) {
		if value.Kind == graph.ExpressionNull {
			nulls++
			if value.Type.Kind != graph.TypeNull || value.Type.Optional {
				t.Fatalf("null became undefined: %+v", value)
			}
		}
	})
	if nulls != 1 {
		t.Fatal("missing null comparison")
	}
}

func TestJSONValueGraphArrayChecksDoNotTypeElements(t *testing.T) {
	program := jsonValueGraph(t, `function first(value: unknown): string {
  if (!Array.isArray(value)) return "invalid";
  for (const item of value) {
    if (typeof item !== "string" || item.length > 32) return "invalid";
    return item.trim();
  }
  return "empty";
}`)
	function := program.Statements[0]
	loop := function.Body[1]
	if loop.Kind != graph.StatementForOf || loop.Type.Kind != graph.TypeUnknown || loop.Value.Kind != graph.ExpressionUnknownProjection || loop.Value.Type.Kind != graph.TypeArray || loop.Value.Type.Element.Kind != graph.TypeUnknown {
		t.Fatalf("array check falsely typed elements: %+v", loop)
	}
	var kinds = map[graph.ExpressionKind]int{}
	walkGraphExpressions(program.Statements, func(value *graph.Expression) {
		kinds[value.Kind]++
		if value.Kind == graph.ExpressionIdentifier && value.Binding == loop.Binding && value.Type.Kind != graph.TypeUnknown {
			t.Fatalf("iteration binding storage lost unknown: %+v", value)
		}
	})
	if kinds[graph.ExpressionIsArray] != 1 || kinds[graph.ExpressionStringLength] != 1 || kinds[graph.ExpressionStringTrim] != 1 || kinds[graph.ExpressionUnknownProjection] != 3 {
		t.Fatalf("array/string operations lost: %+v", kinds)
	}
}

func TestJSONValueGraphAnnotationNeverConstructsRecord(t *testing.T) {
	for _, source := range []string{
		`interface Task { title: string; } const task: Task = JSON.parse("{}");`,
		`interface Task { title: string; } const task = JSON.parse("{}") as Task;`,
	} {
		result := extractSource("json-values.ts", source, true)
		if result.Program != nil || len(result.Diagnostics) != 1 {
			t.Fatalf("annotation/cast constructed a record: %+v", result)
		}
	}
	// Even the scalar annotation is only an obligation. No guard has been
	// invented; a consumer must fence this source unless it proves the kind.
	program := jsonValueGraph(t, `const number: number = JSON.parse("null");`)
	value := program.Statements[0].Value
	if value.Kind != graph.ExpressionUnknownProjection || value.Operand.Kind != graph.ExpressionJSONParse || value.UnwrapOptional {
		t.Fatalf("annotation became unchecked coercion: %+v", value)
	}
}

func TestJSONValueGraphKeepsPrototypeSensitiveNames(t *testing.T) {
	program := jsonValueGraph(t, `function has(value: unknown): boolean {
  if (typeof value !== "object" || value === null) return false;
  return "toString" in value;
}`)
	value := program.Statements[0].Body[1].Value
	if value.Kind != graph.ExpressionHasProperty || value.Index.String != "toString" || value.Receiver.Type.Kind != graph.TypeUnknown {
		t.Fatalf("prototype-sensitive operation disappeared: %+v", value)
	}
	// The graph operation is full `in`, not a pre-approved own lookup. The
	// later consumer must prove a suitable origin/prototype domain or fence.
}

func TestJSONValueGraphBuiltinIdentityAndBoundaryCallFences(t *testing.T) {
	for _, source := range []string{
		`interface Local { parse: (text: string) => number; } function local(JSON: Local): number { return JSON.parse("1"); }`,
		`interface Local { isArray: (value: number) => boolean; } function local(Array: Local): boolean { return Array.isArray(1); }`,
	} {
		result := extractSource("json-values.ts", source, true)
		if result.Program != nil {
			walkGraphExpressions(result.Program.Statements, func(value *graph.Expression) {
				if value.Kind == graph.ExpressionJSONParse || value.Kind == graph.ExpressionIsArray {
					t.Fatalf("shadow acquired builtin semantics: %+v", value)
				}
			})
		}
		if len(result.Diagnostics) != 1 || result.Diagnostics[0].Construct == "JSONBoundaryCall" {
			t.Fatalf("ordinary shadow call changed admission: %+v", result)
		}
	}
	for _, source := range []string{
		`const value: unknown = JSON.parse("{}", (key: string, value: unknown): unknown => value);`,
		`const value: unknown = JSON.parse?.("{}");`,
		`const value: unknown = JSON.parse(1);`,
	} {
		result := extractSource("json-values.ts", source, true)
		if result.Program != nil || len(result.Diagnostics) != 1 {
			t.Fatalf("unsupported boundary acquired semantics: %+v", result)
		}
	}
}

func TestJSONValueGraphLiteralCodeUnitsSurviveGraphEncoding(t *testing.T) {
	for _, test := range []struct {
		literal string
		units   []uint16
	}{
		{`"\ud800"`, []uint16{0xd800}},
		{`"\udfff"`, []uint16{0xdfff}},
		{`"a\ud800😀\udfff"`, []uint16{0x61, 0xd800, 0xd83d, 0xde00, 0xdfff}},
	} {
		program := jsonValueGraph(t, "const text = "+test.literal+";")
		value := program.Statements[0].Value
		if value.String != "" || !reflect.DeepEqual(value.StringUnits, test.units) {
			t.Fatalf("literal units changed: %+v", value)
		}
		encoded, err := json.Marshal(program)
		if err != nil {
			t.Fatal(err)
		}
		var decoded graph.Program
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(decoded.Statements[0].Value.StringUnits, test.units) {
			t.Fatalf("graph transport replaced code units: %s", encoded)
		}
	}
	plain := jsonValueGraph(t, `const text = "😀";`).Statements[0].Value
	if plain.String != "😀" || plain.StringUnits != nil {
		t.Fatalf("scalar-valid path changed: %+v", plain)
	}
}

func TestJSONValueGraphRemainsPrivateUntilConsumersLand(t *testing.T) {
	for _, source := range []string{
		`function valid(value: unknown): boolean { return typeof value === "string"; }`,
		`const value: unknown = JSON.parse("{}");`,
		`const text = "\ud800";`,
		`const size = "😀".length;`,
	} {
		for _, result := range []graph.Result{Extract("json-values.ts", source), ExtractFiles("json-values.ts", map[string]string{"json-values.ts": source})} {
			if result.Program != nil || len(result.Diagnostics) != 1 {
				t.Fatalf("public extraction exposed unfinished backend operation: %+v", result)
			}
		}
	}
}
