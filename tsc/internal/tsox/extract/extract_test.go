package extract

import (
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/tsox/graph"
)

const extractSourcePath = "/src/extract.ts"

func TestExtractAcceptsCorpus01To04Subset(t *testing.T) {
	t.Parallel()

	source := `let counter = 3;
const greeting = "count";
counter = counter + 1 - 2 * 3 / 4 % 2;
counter += 2;
counter++;
const less = counter < 10;
const atMost = counter <= 10;
const greater = counter > 0;
const equal = counter === counter;
const enabled = true;
const both = less && greater;
const either = less || greater;
const negated = !equal;
const negative = -counter;
const concatenated = greeting + counter;
const templated = ` + "`" + `${greeting}:${counter}` + "`" + `;
if (less) {
  counter += 1;
} else {
  counter = counter - 1;
}
while (counter > 0) {
  counter = counter - 1;
}
for (let index = 0; index < 2; index++) {
  counter += index;
}
function increment(value: number): number {
  return value + 1;
}
function factorial(value: number): number {
  if (value === 0) {
    return 1;
  }
  return value * factorial(value - 1);
}
let result = increment(counter);
let recursive = factorial(3);
let offset = 1;
const bump = (value: number) => {
  return value + offset;
};
let closedValue = bump(counter);
console.log(templated);`
	result := Extract(extractSourcePath, source)

	if result.Program == nil {
		t.Fatalf("Extract returned no graph: diagnostics=%v", result.Diagnostics)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("Extract returned unexpected diagnostics: %v", result.Diagnostics)
	}
	program := result.Program
	if program.SourcePath != extractSourcePath {
		t.Fatalf("graph source path: got %q, want %q", program.SourcePath, extractSourcePath)
	}

	statements := graphStatements(program)
	wantStatements := []graph.StatementKind{
		graph.StatementVariable,
		graph.StatementExpression,
		graph.StatementPrint,
		graph.StatementIf,
		graph.StatementWhile,
		graph.StatementFor,
		graph.StatementFunction,
		graph.StatementReturn,
	}
	for _, want := range wantStatements {
		if !statements[want] {
			t.Errorf("graph is missing statement kind %q", want)
		}
	}

	expressions, operators := graphExpressions(program)
	wantExpressions := []graph.ExpressionKind{
		graph.ExpressionNumber,
		graph.ExpressionString,
		graph.ExpressionBoolean,
		graph.ExpressionIdentifier,
		graph.ExpressionBinary,
		graph.ExpressionUnary,
		graph.ExpressionAssignment,
		graph.ExpressionUpdate,
		graph.ExpressionCall,
		graph.ExpressionTemplate,
		graph.ExpressionArrow,
	}
	for _, want := range wantExpressions {
		if !expressions[want] {
			t.Errorf("graph is missing expression kind %q", want)
		}
	}
	for _, want := range []string{"+", "-", "*", "/", "%", "<", "<=", ">", "===", "=", "+=", "++", "!", "&&", "||"} {
		if !operators[want] {
			t.Errorf("graph is missing operator %q", want)
		}
	}

	counter := variableStatement(t, program, "counter")
	greeting := variableStatement(t, program, "greeting")
	less := variableStatement(t, program, "less")
	equal := variableStatement(t, program, "equal")
	negative := variableStatement(t, program, "negative")
	concatenated := variableStatement(t, program, "concatenated")
	templated := variableStatement(t, program, "templated")
	offset := variableStatement(t, program, "offset")
	bump := variableStatement(t, program, "bump")
	resultValue := variableStatement(t, program, "result")

	if !counter.Mutable {
		t.Error("counter should be mutable")
	}
	if greeting.Mutable {
		t.Error("greeting should be immutable")
	}
	if counter.Binding == 0 || greeting.Binding == 0 || offset.Binding == 0 {
		t.Fatal("declarations must have non-zero binding identities")
	}
	if counter.Type.Kind != graph.TypeNumber {
		t.Errorf("counter type: got %q, want number", counter.Type.Kind)
	}
	if greeting.Type.Kind != graph.TypeString {
		t.Errorf("greeting type: got %q, want string", greeting.Type.Kind)
	}
	if less.Type.Kind != graph.TypeBoolean || equal.Type.Kind != graph.TypeBoolean {
		t.Errorf("comparison types: less=%q equal=%q, want boolean", less.Type.Kind, equal.Type.Kind)
	}
	if negative.Type.Kind != graph.TypeNumber {
		t.Errorf("negative type: got %q, want number", negative.Type.Kind)
	}
	if concatenated.Type.Kind != graph.TypeString {
		t.Errorf("string concatenation type: got %q, want string", concatenated.Type.Kind)
	}
	if templated.Type.Kind != graph.TypeString {
		t.Errorf("template type: got %q, want string", templated.Type.Kind)
	}

	if counterReference := firstIdentifier(t, program, "counter"); counterReference.Binding != counter.Binding {
		t.Errorf("counter reference binding: got %d, want declaration binding %d", counterReference.Binding, counter.Binding)
	}

	increment := functionStatement(t, program, "increment")
	if increment.Binding == 0 {
		t.Fatal("increment declaration must have a non-zero binding identity")
	}
	if increment.ReturnType.Kind != graph.TypeNumber {
		t.Errorf("increment return type: got %q, want number", increment.ReturnType.Kind)
	}
	if len(increment.Parameters) != 1 {
		t.Fatalf("increment parameters: got %d, want 1", len(increment.Parameters))
	}
	if increment.Parameters[0].Type.Kind != graph.TypeNumber || increment.Parameters[0].Binding == 0 {
		t.Errorf("increment parameter: type=%q binding=%d, want number and non-zero binding", increment.Parameters[0].Type.Kind, increment.Parameters[0].Binding)
	}
	valueReference := firstIdentifierInStatements(t, increment.Body, "value")
	if valueReference.Binding != increment.Parameters[0].Binding {
		t.Errorf("increment parameter reference binding: got %d, want %d", valueReference.Binding, increment.Parameters[0].Binding)
	}

	factorial := functionStatement(t, program, "factorial")
	if factorial.ReturnType.Kind != graph.TypeNumber {
		t.Errorf("factorial return type: got %q, want number", factorial.ReturnType.Kind)
	}
	recursiveCall := firstCallTo(t, factorial.Body, "factorial")
	if recursiveCall.Callee == nil || recursiveCall.Callee.Binding != factorial.Binding {
		t.Fatalf("recursive call binding: got %v, want function binding %d", bindingOf(recursiveCall.Callee), factorial.Binding)
	}

	if resultValue.Value == nil || resultValue.Value.Kind != graph.ExpressionCall || resultValue.Value.Type.Kind != graph.TypeNumber {
		t.Fatalf("result initializer: got %#v, want number-valued call", resultValue.Value)
	}
	if resultValue.Value.Callee == nil || resultValue.Value.Callee.Binding != increment.Binding {
		t.Errorf("increment call binding: got %v, want function binding %d", bindingOf(resultValue.Value.Callee), increment.Binding)
	}

	if bump.Value == nil || bump.Value.Kind != graph.ExpressionArrow {
		t.Fatalf("bump initializer: got %#v, want block arrow", bump.Value)
	}
	if bump.Type.Kind != graph.TypeFunction || bump.Value.Type.Kind != graph.TypeFunction {
		t.Errorf("bump function type: statement=%q expression=%q, want function", bump.Type.Kind, bump.Value.Type.Kind)
	}
	if len(bump.Value.Parameters) != 1 || bump.Value.Parameters[0].Type.Kind != graph.TypeNumber {
		t.Errorf("bump parameters: got %#v, want one number parameter", bump.Value.Parameters)
	}
	if len(bump.Value.Body) == 0 {
		t.Fatal("bump arrow body is empty")
	}
	offsetReference := firstIdentifierInStatements(t, bump.Value.Body, "offset")
	if offsetReference.Binding != offset.Binding {
		t.Errorf("closure capture binding: got %d, want offset binding %d", offsetReference.Binding, offset.Binding)
	}

	printStatement := statementOfKind(t, program, graph.StatementPrint)
	if len(printStatement.Arguments) != 1 || printStatement.Arguments[0].Type.Kind != graph.TypeString {
		t.Errorf("console.log argument: got %#v, want one string argument", printStatement.Arguments)
	}

	for _, expression := range allGraphExpressions(program) {
		if expression.Kind == graph.ExpressionUpdate && expression.Operator == "++" && expression.Prefix {
			t.Error("counter/index ++ should be represented as postfix update")
		}
	}
}

func TestExtractAcceptsEscapingMutableClosure(t *testing.T) {
	t.Parallel()

	result := Extract(extractSourcePath, `function makeCounter(start: number): () => number {
  let value: number = start;
  return (): number => {
    value += 1;
    return value;
  };
}
const next = makeCounter(10);
console.log(next());`)
	if result.Program == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("closure extraction failed: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}

	makeCounter := functionStatement(t, result.Program, "makeCounter")
	if makeCounter.ReturnType.Kind != graph.TypeFunction || makeCounter.ReturnType.Result == nil || makeCounter.ReturnType.Result.Kind != graph.TypeNumber {
		t.Fatalf("makeCounter return type: got %#v, want () => number", makeCounter.ReturnType)
	}
	next := variableStatement(t, result.Program, "next")
	if next.Type.Kind != graph.TypeFunction || next.Type.Result == nil || next.Type.Result.Kind != graph.TypeNumber {
		t.Fatalf("next type: got %#v, want () => number", next.Type)
	}

	value := variableStatement(t, result.Program, "value")
	var closure *graph.Expression
	for _, expression := range allGraphExpressions(result.Program) {
		if expression.Kind == graph.ExpressionArrow {
			closure = expression
			break
		}
	}
	if closure == nil {
		t.Fatal("returned closure expression not found")
	}
	valueReference := firstIdentifierInStatements(t, closure.Body, "value")
	if valueReference.Binding != value.Binding {
		t.Fatalf("closure capture binding: got %d, want %d", valueReference.Binding, value.Binding)
	}
}

func TestExtractFencesFunctionAliasAtTheStorageConstruct(t *testing.T) {
	t.Parallel()

	result := Extract(extractSourcePath, `function value(): number { return 1; }
const alias = value;
console.log(alias());`)
	if result.Program != nil || len(result.Diagnostics) != 1 {
		t.Fatalf("function alias result: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Construct != "FunctionAlias" || diagnostic.Position != (graph.Position{Line: 2, Column: 15}) || !strings.Contains(diagnostic.Message, "function-valued storage alias") {
		t.Fatalf("function alias diagnostic: got %#v", diagnostic)
	}
}

func TestExtractFencesReturnedFunctionCallAtTheOuterCall(t *testing.T) {
	t.Parallel()

	result := Extract(extractSourcePath, `function make(): () => number { return (): number => { return 1; }; }
console.log(make()());`)
	if result.Program != nil || len(result.Diagnostics) != 1 {
		t.Fatalf("returned function call result: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Construct != "CallExpression" || diagnostic.Position != (graph.Position{Line: 2, Column: 13}) || !strings.Contains(diagnostic.Message, "non-identifier call target") {
		t.Fatalf("returned function call diagnostic: got %#v", diagnostic)
	}
}

func TestExtractFencesFunctionTypedBindingUsedAsValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		source   string
		position graph.Position
	}{
		{name: "return", source: "function pass(next: () => number): () => number {\n  return next;\n}", position: graph.Position{Line: 2, Column: 10}},
		{name: "parenthesized return", source: "function pass(next: () => number): () => number {\n  return (next);\n}", position: graph.Position{Line: 2, Column: 11}},
		{name: "object member", source: "interface Holder { next: () => number; }\nconst next = (): number => { return 1; };\nconst holder: Holder = { next: (next) };", position: graph.Position{Line: 3, Column: 33}},
		{name: "array element", source: "const next = (): number => { return 1; };\nconst values: (() => number)[] = [(next)];", position: graph.Position{Line: 2, Column: 36}},
		{name: "array method argument", source: "const callbacks: (() => number)[] = [];\nconst next = (): number => { return 1; };\ncallbacks.push((next));", position: graph.Position{Line: 3, Column: 17}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Extract(extractSourcePath, test.source)
			if result.Program != nil || len(result.Diagnostics) != 1 {
				t.Fatalf("function value result: program=%v diagnostics=%v", result.Program, result.Diagnostics)
			}
			diagnostic := result.Diagnostics[0]
			if diagnostic.Construct != "FunctionValue" || diagnostic.Position != test.position || !strings.Contains(diagnostic.Message, "function-typed binding used as a value") {
				t.Fatalf("function value diagnostic: got %#v, want position %#v", diagnostic, test.position)
			}
		})
	}
}

func TestExtractAcceptsNamedObjectsDenseArraysMutationAndIdentity(t *testing.T) {
	t.Parallel()

	result := Extract(extractSourcePath, `interface Point { x: number; y: number; }
type Box = { point: Point; rows: number[][] };
function mutate(point: Point, values: number[]): void {
  point.x = values[0];
  values[1] = point.y;
}
const point: Point = { x: 1, y: 2 };
const alias: Point = point;
const values: number[] = [3, 4];
const box: Box = { point: point, rows: [[5, 6]] };
mutate(alias, values);
console.log(point.x);
console.log(values[1]);
console.log(box.point.y);
console.log(box.rows[0][1]);
console.log(values.length);
console.log(point === alias);
for (const value of values) { console.log(value); }`)
	if result.Program == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("object/array extraction failed: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}
	if len(result.Program.Shapes) != 2 {
		t.Fatalf("shapes: got %d, want 2 (%#v)", len(result.Program.Shapes), result.Program.Shapes)
	}
	shapeByName := make(map[string]graph.Shape)
	for _, shape := range result.Program.Shapes {
		shapeByName[shape.Name] = shape
	}
	pointShape, ok := shapeByName["Point"]
	if !ok || len(pointShape.Fields) != 2 || pointShape.Fields[0].Name != "x" || pointShape.Fields[1].Name != "y" {
		t.Fatalf("Point shape does not preserve declaration fields: %#v", pointShape)
	}
	boxShape, ok := shapeByName["Box"]
	if !ok || len(boxShape.Fields) != 2 || boxShape.Fields[0].Type.Shape != pointShape.ID {
		t.Fatalf("Box shape does not reference Point identity: %#v", boxShape)
	}
	if boxShape.Fields[1].Type.Kind != graph.TypeArray || boxShape.Fields[1].Type.Element == nil || boxShape.Fields[1].Type.Element.Kind != graph.TypeArray {
		t.Fatalf("Box.rows is not a nested array type: %#v", boxShape.Fields[1].Type)
	}

	statements := graphStatements(result.Program)
	if !statements[graph.StatementForOf] {
		t.Fatal("graph is missing for-of statement")
	}
	expressions, operators := graphExpressions(result.Program)
	for _, kind := range []graph.ExpressionKind{
		graph.ExpressionObject,
		graph.ExpressionProperty,
		graph.ExpressionArray,
		graph.ExpressionIndex,
		graph.ExpressionArrayLength,
		graph.ExpressionAssignment,
	} {
		if !expressions[kind] {
			t.Errorf("graph is missing expression kind %q", kind)
		}
	}
	if !operators["==="] {
		t.Error("graph is missing object identity operator")
	}
}

func TestExtractRejectsObjectAndArrayFences(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		construct string
	}{
		{name: "anonymous shape", source: `const value = { x: 1 }; console.log(value.x);`, construct: "anonymous shape"},
		{
			name: "cross declaration flow",
			source: `interface Wide { x: number; y: number; }
interface Narrow { x: number; }
const wide: Wide = { x: 1, y: 2 };
const narrow: Narrow = wide;`,
			construct: "distinct named shapes",
		},
		{name: "recursive named shape", source: `interface Link { value: number; next: Link; }`, construct: "recursive named shape"},
		{name: "array method", source: `const values: number[] = [1]; values.reverse();`, construct: "reverse"},
		{name: "compound composite assignment", source: `interface Point { x: number; } const point: Point = { x: 1 }; point.x += 1;`, construct: "compound composite assignment"},
		{name: "object print", source: `interface Point { x: number; } const point: Point = { x: 1 }; console.log(point);`, construct: "console.log argument"},
		{name: "array print", source: `const values: number[] = [1]; console.log(values);`, construct: "console.log argument"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := Extract(extractSourcePath, tt.source)
			if result.Program != nil {
				t.Fatalf("unsupported %s produced a graph: %#v", tt.construct, result.Program)
			}
			if len(result.Diagnostics) != 1 {
				t.Fatalf("diagnostics for %s: got %d, want 1 (%v)", tt.construct, len(result.Diagnostics), result.Diagnostics)
			}
			diagnostic := result.Diagnostics[0]
			if diagnostic.Position.Line < 1 || diagnostic.Position.Column < 1 {
				t.Errorf("diagnostic has no source position: %#v", diagnostic)
			}
			if !diagnosticNamesConstruct(diagnostic, tt.construct) {
				t.Errorf("diagnostic does not name %q: %#v", tt.construct, diagnostic)
			}
		})
	}
}

func TestExtractAcceptsArrayMethodCalls(t *testing.T) {
	t.Parallel()
	result := Extract(extractSourcePath, `const values: number[] = [1, 2, 3];
const pushed: number = values.push(4);
const unshifted: number = values.unshift(0);
const found: number = values.indexOf(2, -2);
const present: boolean = values.includes(3);
const copy: number[] = values.slice(-3.5, 4);`)
	if result.Program == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("array method extraction failed: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}
	wants := map[string]graph.TypeKind{"pushed": graph.TypeNumber, "unshifted": graph.TypeNumber, "found": graph.TypeNumber, "present": graph.TypeBoolean, "copy": graph.TypeArray}
	for name, wantType := range wants {
		value := variableStatement(t, result.Program, name)
		if value.Value == nil || value.Value.Kind != graph.ExpressionMethodCall || value.Value.Type.Kind != wantType {
			t.Errorf("%s initializer: got %#v, want method call returning %s", name, value.Value, wantType)
		}
		if value.Value.Receiver == nil || value.Value.Name == "" {
			t.Errorf("%s method call lacks receiver or method name: %#v", name, value.Value)
		}
	}
}

func TestExtractAcceptsOptionalScalarBindingAndCheckerNarrowing(t *testing.T) {
	t.Parallel()
	result := Extract(extractSourcePath, `let value: number | undefined = undefined;
value = 2;
const missing: boolean = value === undefined;
if (value !== undefined) {
  console.log(value + 1);
}`)
	if result.Program == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("optional scalar extraction failed: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}
	value := variableStatement(t, result.Program, "value")
	if value.Type.Kind != graph.TypeNumber || !value.Type.Optional {
		t.Fatalf("value type: got %#v, want optional number", value.Type)
	}
	if value.Value == nil || value.Value.Kind != graph.ExpressionUndefined {
		t.Fatalf("value initializer: got %#v, want undefined", value.Value)
	}
	var narrowed *graph.Expression
	for _, expression := range allGraphExpressions(result.Program) {
		if expression.Kind == graph.ExpressionIdentifier && expression.Name == "value" && expression.UnwrapOptional {
			narrowed = expression
			break
		}
	}
	if narrowed == nil || narrowed.Type.Kind != graph.TypeNumber || narrowed.Type.Optional {
		t.Fatalf("narrowed use: got %#v, want unwrapped number use", narrowed)
	}
}

func TestExtractAcceptsOptionalFieldsAndOmittedLiteralProperties(t *testing.T) {
	t.Parallel()
	result := Extract(extractSourcePath, `interface Point { x: number; }
interface Holder { point?: Point; label: string | undefined; }
const empty: Holder = { label: undefined };
const point: Point = { x: 1 };
const full: Holder = { point: point, label: "full" };
if (full.point !== undefined) { full.point.x = 2; }`)
	if result.Program == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("optional field extraction failed: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}
	if len(result.Program.Shapes) != 2 || !result.Program.Shapes[1].Fields[0].Type.Optional || !result.Program.Shapes[1].Fields[1].Type.Optional {
		t.Fatalf("optional field types were not preserved: %#v", result.Program.Shapes)
	}
	empty := variableStatement(t, result.Program, "empty")
	if empty.Value == nil || len(empty.Value.Properties) != 2 || empty.Value.Properties[0].Value.Kind != graph.ExpressionUndefined {
		t.Fatalf("omitted optional property was not materialized as undefined: %#v", empty.Value)
	}
}

func TestExtractAcceptsOptionalParametersAndOmittedArguments(t *testing.T) {
	t.Parallel()
	result := Extract(extractSourcePath, `interface Point { x: number; }
function scalar(value?: number): number { if (value === undefined) { return 0; } return value; }
function composite(value: Point | undefined): number { if (value === undefined) { return 0; } return value.x; }
const point: Point = { x: 2 };
console.log(scalar() + scalar(1) + composite(point));`)
	if result.Program == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("optional parameter extraction failed: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}
	scalar := functionStatement(t, result.Program, "scalar")
	if len(scalar.Parameters) != 1 || !scalar.Parameters[0].Type.Optional {
		t.Fatalf("optional parameter was not preserved: %#v", scalar.Parameters)
	}
	call := firstCallTo(t, result.Program.Statements, "scalar")
	if len(call.Arguments) != 1 || call.Arguments[0].Kind != graph.ExpressionUndefined {
		t.Fatalf("omitted argument was not materialized as undefined: %#v", call.Arguments)
	}
}

func TestExtractAcceptsOptionalReturningArrayMethods(t *testing.T) {
	t.Parallel()
	result := Extract(extractSourcePath, `interface Point { x: number; }
const values: number[] = [1, 2];
const popped: number | undefined = values.pop();
const shifted: number | undefined = values.shift();
const points: Point[] = [{ x: 1 }];
const found: Point | undefined = points.find((point: Point): boolean => { return point.x === 1; });`)
	if result.Program == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("optional array method extraction failed: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}
	for _, name := range []string{"popped", "shifted", "found"} {
		value := variableStatement(t, result.Program, name)
		if value.Value == nil || value.Value.Kind != graph.ExpressionMethodCall || !value.Value.Type.Optional {
			t.Errorf("%s initializer: got %#v, want optional method call", name, value.Value)
		}
	}
}

func TestExtractFencesOptionalBoundariesWithNamedPositions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		source    string
		construct string
		position  graph.Position
	}{
		{name: "general union", source: `let value: number | string = 1;`, construct: "UnsupportedUnion", position: graph.Position{Line: 1, Column: 12}},
		{name: "boolean literal union", source: `let value: true | false = true;`, construct: "UnsupportedUnion", position: graph.Position{Line: 1, Column: 12}},
		{name: "optional void", source: `let value: void | undefined = undefined;`, construct: "UnsupportedUnion", position: graph.Position{Line: 1, Column: 12}},
		{name: "null", source: `let value: number | null = null;`, construct: "UnsupportedUnion", position: graph.Position{Line: 1, Column: 12}},
		{name: "optional element", source: `const values: (number | undefined)[] = [undefined];`, construct: "OptionalElement", position: graph.Position{Line: 1, Column: 15}},
		{name: "typeof check", source: `const value: number | undefined = undefined; const missing = typeof value === "undefined";`, construct: "TypeofCheck", position: graph.Position{Line: 1, Column: 62}},
		{name: "optional truthiness", source: `let value: number | undefined = undefined; if (value) { value = 1; }`, construct: "NonBooleanCondition", position: graph.Position{Line: 1, Column: 48}},
		{name: "optional logical operand", source: `let value: number | undefined = undefined; const result = value || 1;`, construct: "NonBooleanCondition", position: graph.Position{Line: 1, Column: 59}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Extract(extractSourcePath, test.source)
			if result.Program != nil || len(result.Diagnostics) != 1 {
				t.Fatalf("result: program=%v diagnostics=%v", result.Program, result.Diagnostics)
			}
			diagnostic := result.Diagnostics[0]
			if diagnostic.Construct != test.construct || diagnostic.Position != test.position {
				t.Fatalf("diagnostic: got %#v, want positioned %s", diagnostic, test.construct)
			}
		})
	}
}

func TestExtractAcceptsPlan0016OptionalSyntax(t *testing.T) {
	t.Parallel()
	result := Extract(extractSourcePath, `interface Point { x: number; }
interface Holder { point?: Point; }
function choose(value: number = 2): number { return value; }
let point: Point | undefined = undefined;
let holder: Holder = {};
point = { x: 1 };
holder.point = { x: 3 };
const chained: number | undefined = point?.x;
const coalesced: number = chained ?? choose();
const asserted: number = chained!;
const alreadyPresent = asserted;
const redundant: number = alreadyPresent!;
const equal: boolean = chained === asserted;`)
	if result.Program == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("plan 0016 extraction failed: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}
	choose := functionStatement(t, result.Program, "choose")
	if len(choose.Parameters) != 1 || choose.Parameters[0].Default == nil || !choose.Parameters[0].BoundaryOptional || choose.Parameters[0].Type.Optional {
		t.Fatalf("default parameter metadata: %#v", choose.Parameters)
	}
	chained := variableStatement(t, result.Program, "chained").Value
	if chained == nil || !chained.OptionalChain || chained.Kind != graph.ExpressionProperty {
		t.Fatalf("optional chain metadata: %#v", chained)
	}
	if got := variableStatement(t, result.Program, "coalesced").Value; got == nil || got.Kind != graph.ExpressionNullish {
		t.Fatalf("nullish expression: %#v", got)
	}
	if got := variableStatement(t, result.Program, "asserted").Value; got == nil || !got.UnwrapOptional {
		t.Fatalf("non-null assertion: %#v", got)
	}
	if got := variableStatement(t, result.Program, "redundant").Value; got == nil || got.UnwrapOptional || !got.NonNullAssertion {
		t.Fatalf("redundant non-null assertion: %#v", got)
	}
}

func TestExtractFencesNeverTypedUseAsUnreachable(t *testing.T) {
	t.Parallel()
	result := Extract(extractSourcePath, `let value: number | undefined = 1;
value = undefined;
if (value !== undefined) { console.log(value * 2); }`)
	if result.Program != nil || len(result.Diagnostics) != 1 {
		t.Fatalf("result: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Construct != "UnreachableUse" || diagnostic.Position != (graph.Position{Line: 3, Column: 40}) {
		t.Fatalf("diagnostic: got %#v, want positioned UnreachableUse", diagnostic)
	}
}

func TestExtractAcceptsCallbackArrayMethodsAndFunctionArguments(t *testing.T) {
	t.Parallel()
	result := Extract(extractSourcePath, `function apply(callback: (value: number) => number, value: number): number {
  return callback(value);
}
function positive(value: number): boolean { return value > 0; }
const values: number[] = [1, 2, 3];
const visit = (value: number): void => { console.log(value); };
values.forEach(visit);
values.forEach((value: number, index: number): void => { console.log(value + index); });
const mapped: number[] = values.map((value: number): number => { return value + 1; });
const filtered: number[] = values.filter(positive);
values.forEach(positive);
const any: boolean = values.some(positive);
const all: boolean = values.every(positive);
const index: number = values.findIndex(positive);
const total: number = values.reduce((sum: number, value: number): number => { return sum + value; }, 0);
const applied: number = apply((value: number): number => { return value + 1; }, 1);`)
	if result.Program == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("callback extraction failed: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}
}

func TestExtractFencesCallbackMethodBoundaries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		source    string
		construct string
		position  graph.Position
	}{
		{name: "third parameter", source: `const values: number[] = [1]; values.forEach((value: number, index: number, array: number[]): void => { console.log(value); });`, construct: "CallbackArrayParameter", position: graph.Position{Line: 1, Column: 77}},
		{name: "reduce no initial", source: `const values: number[] = [1]; const total = values.reduce((sum: number, value: number): number => { return sum + value; });`, construct: "ReduceWithoutInitial", position: graph.Position{Line: 1, Column: 52}},
		{name: "reduce composite", source: `interface Box { total: number; } const values: number[] = [1]; const box: Box = values.reduce((acc: Box, value: number): Box => { acc.total = acc.total + value; return acc; }, { total: 0 });`, construct: "ReduceCompositeAccumulator", position: graph.Position{Line: 1, Column: 88}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := Extract(extractSourcePath, test.source)
			if result.Program != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Construct != test.construct || result.Diagnostics[0].Position != test.position {
				t.Fatalf("result: program=%v diagnostics=%v, want %s", result.Program, result.Diagnostics, test.construct)
			}
		})
	}
}

func TestExtractFencesForEachUsedAsAValue(t *testing.T) {
	t.Parallel()
	result := Extract(extractSourcePath, `const values: number[] = [1];
const result: void = values.forEach((value: number): void => { console.log(value); });`)
	if result.Program != nil || len(result.Diagnostics) != 1 {
		t.Fatalf("forEach value result: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Construct != "ForEachValue" || diagnostic.Position != (graph.Position{Line: 2, Column: 22}) {
		t.Fatalf("diagnostic: got %#v, want ForEachValue at 2:22", diagnostic)
	}
}

func TestExtractFencesUnsupportedCallbackValueForms(t *testing.T) {
	t.Parallel()
	tests := []string{
		`function make(): (value: number) => number { return (value: number): number => { return value; }; }
function apply(callback: (value: number) => number): number { return callback(1); }
const result: number = apply(make());`,
		`const values: number[] = [1];
const callbacks: (() => number)[] = values.map((value: number): () => number => { return (): number => { return value; }; });`,
	}
	for _, source := range tests {
		result := Extract(extractSourcePath, source)
		if result.Program != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Construct != "FunctionValue" || result.Diagnostics[0].Position.Line < 1 || result.Diagnostics[0].Position.Column < 1 {
			t.Fatalf("unsupported callback value result: program=%v diagnostics=%v", result.Program, result.Diagnostics)
		}
	}
}

func TestExtractFencesUnsupportedArrayMethodsAtTheMethodName(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"reverse"} {
		method := method
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			result := Extract(extractSourcePath, "const values: number[] = [1];\nvalues."+method+"();")
			if result.Program != nil || len(result.Diagnostics) != 1 {
				t.Fatalf("unsupported method result: program=%v diagnostics=%v", result.Program, result.Diagnostics)
			}
			diagnostic := result.Diagnostics[0]
			if diagnostic.Construct != "ArrayMethodCall" || diagnostic.Position != (graph.Position{Line: 2, Column: 8}) || !diagnosticNamesConstruct(diagnostic, method) {
				t.Fatalf("diagnostic: got %#v, want %s at 2:8", diagnostic, method)
			}
		})
	}
}

func TestExtractUsesModuleScopeAlongsideBundledDOMDeclarations(t *testing.T) {
	t.Parallel()

	result := Extract(extractSourcePath, `const name: string = "TSOX";
console.log(name);`)
	if result.Program == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("module-scoped extraction failed: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}
	name := variableStatement(t, result.Program, "name")
	if name.Type.Kind != graph.TypeString {
		t.Fatalf("name type: got %q, want string", name.Type.Kind)
	}
}

func TestExtractAcceptsSourceCallableTargets(t *testing.T) {
	t.Parallel()

	result := Extract(extractSourcePath, `function increment(value: number): number {
  return value + 1;
}
const fromDeclaration = increment(1);
const add = (value: number): number => {
  return value + 1;
};
const fromVariable = add(1);
function makeCounter(start: number): () => number {
  return (): number => {
    return start;
  };
}
const next = makeCounter(10);
const fromClosure = next();
console.log(fromDeclaration);`)
	if result.Program == nil || len(result.Diagnostics) != 0 {
		t.Fatalf("source callable extraction failed: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}

	for _, name := range []string{"fromDeclaration", "fromVariable", "next", "fromClosure"} {
		value := variableStatement(t, result.Program, name)
		if value.Value == nil || value.Value.Kind != graph.ExpressionCall {
			t.Errorf("%s initializer: got %#v, want call", name, value.Value)
		}
	}
	printStatement := statementOfKind(t, result.Program, graph.StatementPrint)
	if len(printStatement.Arguments) != 1 || printStatement.Arguments[0].Kind != graph.ExpressionIdentifier {
		t.Fatalf("console.log statement: got %#v, want identifier argument", printStatement.Arguments)
	}
}

func TestExtractFencesSelfReferentialArrowAtTheReference(t *testing.T) {
	t.Parallel()

	result := Extract(extractSourcePath, `const factorial = (value: number): number => {
  if (value === 0) { return 1; }
  return value * factorial(value - 1);
};`)
	if result.Program != nil || len(result.Diagnostics) != 1 {
		t.Fatalf("self-referential arrow result: program=%v diagnostics=%v", result.Program, result.Diagnostics)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Construct != "SelfReferentialArrow" || diagnostic.Position != (graph.Position{Line: 3, Column: 18}) {
		t.Fatalf("diagnostic: got %#v, want SelfReferentialArrow at 3:18", diagnostic)
	}
	parenthesized := Extract(extractSourcePath, `const recurse = ((value: number): number => { return recurse(value - 1); });`)
	if parenthesized.Program != nil || len(parenthesized.Diagnostics) != 1 || parenthesized.Diagnostics[0].Construct != "SelfReferentialArrow" {
		t.Fatalf("parenthesized self-referential arrow result: program=%v diagnostics=%v", parenthesized.Program, parenthesized.Diagnostics)
	}
}

func TestExtractRejectsUnsupportedBundledFunctionCalls(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		source     string
		wantInText string
	}{
		{
			name:       "direct global",
			source:     `const parsed = parseFloat("1");`,
			wantInText: "parseFloat",
		},
		{
			name: "source alias",
			source: `const parser = parseFloat;
const parsed = parser("1");`,
			wantInText: "parser",
		},
		{
			name:       "console argument",
			source:     `console.log(parseFloat("1"));`,
			wantInText: "parseFloat",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := Extract(extractSourcePath, tt.source)
			if result.Program != nil {
				t.Fatalf("unsupported bundled function call produced a graph: %#v", result.Program)
			}
			if len(result.Diagnostics) != 1 {
				t.Fatalf("bundled function diagnostics: got %d, want 1 (%v)", len(result.Diagnostics), result.Diagnostics)
			}
			text := result.Diagnostics[0].Construct + " " + result.Diagnostics[0].Message
			if !strings.Contains(text, tt.wantInText) {
				t.Errorf("diagnostic does not name %s: construct=%q message=%q", tt.wantInText, result.Diagnostics[0].Construct, result.Diagnostics[0].Message)
			}
		})
	}
}

func TestExtractRejectsUnsupportedConstructAtFirstFence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		construct string
		position  graph.Position
	}{
		{
			name:      "anonymous object literal",
			source:    "const before = 1;\nlet x = {};",
			construct: "anonymous shape",
			position:  graph.Position{Line: 2, Column: 9},
		},
		{
			name:      "array literal",
			source:    "const before = 1;\nlet x = [];",
			construct: "array",
			position:  graph.Position{Line: 2, Column: 9},
		},
		{
			name:      "for of",
			source:    "const before = 1;\nfor (const value of \"ok\") {}",
			construct: "for-of",
			position:  graph.Position{Line: 2, Column: 1},
		},
		{
			name:      "property access",
			source:    "const before = 1;\nlet x = before.toString();",
			construct: "property access",
			position:  graph.Position{Line: 2, Column: 9},
		},
		{
			name:      "async function",
			source:    "const before = 1;\nasync function run() { return before; }",
			construct: "async",
			position:  graph.Position{Line: 2, Column: 1},
		},
		{
			name:      "any type",
			source:    "const value: any = 1;",
			construct: "any",
			position:  graph.Position{Line: 1, Column: 14},
		},
		{
			name:      "union type",
			source:    "const value: number | string = 1;",
			construct: "union",
			position:  graph.Position{Line: 1, Column: 14},
		},
		{
			name:      "nested function",
			source:    "function outer(): number {\n  function inner(): number { return 1; }\n  return inner();\n}",
			construct: "nested function",
			position:  graph.Position{Line: 2, Column: 3},
		},
		{
			name:      "function print",
			source:    "function value(): number { return 1; }\nconsole.log(value);",
			construct: "console.log argument",
			position:  graph.Position{Line: 2, Column: 13},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := Extract(extractSourcePath, tt.source)
			if result.Program != nil {
				t.Fatalf("unsupported %s produced a graph: %#v", tt.construct, result.Program)
			}
			if len(result.Diagnostics) != 1 {
				t.Fatalf("diagnostics for unsupported %s: got %d, want 1 (%v)", tt.construct, len(result.Diagnostics), result.Diagnostics)
			}
			diagnostic := result.Diagnostics[0]
			if diagnostic.SourcePath != extractSourcePath {
				t.Errorf("diagnostic source path: got %q, want %q", diagnostic.SourcePath, extractSourcePath)
			}
			if diagnostic.Position != tt.position {
				t.Errorf("diagnostic position: got %+v, want %+v", diagnostic.Position, tt.position)
			}
			if !diagnosticNamesConstruct(diagnostic, tt.construct) {
				t.Errorf("diagnostic does not name %q: construct=%q message=%q", tt.construct, diagnostic.Construct, diagnostic.Message)
			}
		})
	}
}

func TestExtractReturnsSemanticDiagnosticWithoutGraph(t *testing.T) {
	t.Parallel()

	result := Extract(extractSourcePath, `const value: number = "not a number";`)
	if result.Program != nil {
		t.Fatalf("semantic error produced a graph: %#v", result.Program)
	}
	if len(result.Diagnostics) != 1 {
		t.Fatalf("semantic diagnostics: got %d, want 1 (%v)", len(result.Diagnostics), result.Diagnostics)
	}
	if result.Diagnostics[0].Message == "" {
		t.Fatal("semantic diagnostic has an empty message")
	}
}

func graphStatements(program *graph.Program) map[graph.StatementKind]bool {
	seen := make(map[graph.StatementKind]bool)
	var visit func([]*graph.Statement)
	visit = func(statements []*graph.Statement) {
		for _, statement := range statements {
			if statement == nil {
				continue
			}
			seen[statement.Kind] = true
			visit(statement.Init)
			visit(statement.Then)
			visit(statement.Else)
			visit(statement.Body)
		}
	}
	visit(program.Statements)
	return seen
}

func graphExpressions(program *graph.Program) (map[graph.ExpressionKind]bool, map[string]bool) {
	kinds := make(map[graph.ExpressionKind]bool)
	operators := make(map[string]bool)
	for _, expression := range allGraphExpressions(program) {
		kinds[expression.Kind] = true
		if expression.Operator != "" {
			operators[expression.Operator] = true
		}
	}
	return kinds, operators
}

func allGraphExpressions(program *graph.Program) []*graph.Expression {
	var expressions []*graph.Expression
	var visitExpression func(*graph.Expression)
	var visitStatements func([]*graph.Statement)
	visitExpression = func(expression *graph.Expression) {
		if expression == nil {
			return
		}
		expressions = append(expressions, expression)
		visitExpression(expression.Left)
		visitExpression(expression.Right)
		visitExpression(expression.Operand)
		visitExpression(expression.Callee)
		visitExpression(expression.Receiver)
		visitExpression(expression.Index)
		for _, argument := range expression.Arguments {
			visitExpression(argument)
		}
		for _, expressionPart := range expression.Expressions {
			visitExpression(expressionPart)
		}
		for _, property := range expression.Properties {
			visitExpression(property.Value)
		}
		visitStatements(expression.Body)
	}
	visitStatements = func(statements []*graph.Statement) {
		for _, statement := range statements {
			if statement == nil {
				continue
			}
			visitExpression(statement.Value)
			for _, argument := range statement.Arguments {
				visitExpression(argument)
			}
			visitExpression(statement.Condition)
			visitStatements(statement.Init)
			visitExpression(statement.Increment)
			visitStatements(statement.Then)
			visitStatements(statement.Else)
			visitStatements(statement.Body)
		}
	}
	visitStatements(program.Statements)
	return expressions
}

func variableStatement(t *testing.T, program *graph.Program, name string) *graph.Statement {
	t.Helper()
	for _, statement := range allStatements(program) {
		if statement.Kind == graph.StatementVariable && statement.Name == name {
			return statement
		}
	}
	t.Fatalf("variable declaration %q not found", name)
	return nil
}

func functionStatement(t *testing.T, program *graph.Program, name string) *graph.Statement {
	t.Helper()
	for _, statement := range allStatements(program) {
		if statement.Kind == graph.StatementFunction && statement.Name == name {
			return statement
		}
	}
	t.Fatalf("function declaration %q not found", name)
	return nil
}

func statementOfKind(t *testing.T, program *graph.Program, kind graph.StatementKind) *graph.Statement {
	t.Helper()
	for _, statement := range allStatements(program) {
		if statement.Kind == kind {
			return statement
		}
	}
	t.Fatalf("statement kind %q not found", kind)
	return nil
}

func allStatements(program *graph.Program) []*graph.Statement {
	var result []*graph.Statement
	var visit func([]*graph.Statement)
	visit = func(statements []*graph.Statement) {
		for _, statement := range statements {
			if statement == nil {
				continue
			}
			result = append(result, statement)
			visit(statement.Init)
			visit(statement.Then)
			visit(statement.Else)
			visit(statement.Body)
		}
	}
	visit(program.Statements)
	return result
}

func firstIdentifier(t *testing.T, program *graph.Program, name string) *graph.Expression {
	t.Helper()
	return firstIdentifierInExpressions(t, allGraphExpressions(program), name)
}

func firstIdentifierInStatements(t *testing.T, statements []*graph.Statement, name string) *graph.Expression {
	t.Helper()
	var expressions []*graph.Expression
	visitProgram := &graph.Program{Statements: statements}
	expressions = allGraphExpressions(visitProgram)
	return firstIdentifierInExpressions(t, expressions, name)
}

func firstIdentifierInExpressions(t *testing.T, expressions []*graph.Expression, name string) *graph.Expression {
	t.Helper()
	for _, expression := range expressions {
		if expression.Kind == graph.ExpressionIdentifier && expression.Name == name {
			return expression
		}
	}
	t.Fatalf("identifier %q not found", name)
	return nil
}

func firstCallTo(t *testing.T, statements []*graph.Statement, name string) *graph.Expression {
	t.Helper()
	for _, expression := range allGraphExpressions(&graph.Program{Statements: statements}) {
		if expression.Kind == graph.ExpressionCall && expression.Callee != nil && expression.Callee.Name == name {
			return expression
		}
	}
	t.Fatalf("call to %q not found", name)
	return nil
}

func bindingOf(expression *graph.Expression) graph.BindingID {
	if expression == nil {
		return 0
	}
	return expression.Binding
}

func diagnosticNamesConstruct(diagnostic graph.Diagnostic, construct string) bool {
	text := normalizeDiagnosticText(diagnostic.Construct + " " + diagnostic.Message)
	want := normalizeDiagnosticText(construct)
	return strings.Contains(text, want)
}

func normalizeDiagnosticText(value string) string {
	value = strings.ToLower(value)
	var normalized strings.Builder
	for _, r := range value {
		if r >= 'a' && r <= 'z' {
			normalized.WriteRune(r)
		}
	}
	return normalized.String()
}
