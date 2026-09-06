package extract

import (
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/tsox/graph"
)

func TestObjectDestructuringKeepsOrderedBindingsAndSourceIdentity(t *testing.T) {
	result := Extract("bindings.ts", `interface Pair { first: number; second: string; }
const source: Pair = {first: 1, second: "two"};
const before = 0, {second: renamed, first} = source, after = first;
let {first: mutable} = source;
mutable = 3;
console.log(renamed);`)
	if result.Program == nil {
		t.Fatal(result.Diagnostics)
	}
	statements := result.Program.Statements
	for index, name := range []string{"source", "before", "renamed", "first", "after", "mutable"} {
		if statements[index].Kind != graph.StatementVariable || statements[index].Name != name || statements[index].Mutable != (name == "mutable") {
			t.Fatalf("declaration %d: %+v", index, statements[index])
		}
	}
	for index, property := range map[int]string{2: "second", 3: "first", 5: "first"} {
		value := statements[index].Value
		if value.Kind != graph.ExpressionProperty || value.Name != property || value.Receiver.Kind != graph.ExpressionIdentifier || value.Receiver.Binding != statements[0].Binding || !sameType(value.Receiver.Type, statements[0].Type) {
			t.Fatalf("field read %d: %+v", index, value)
		}
	}
	if statements[2].Type.Kind != graph.TypeString || statements[3].Type.Kind != graph.TypeNumber || statements[4].Value.Binding != statements[3].Binding || statements[6].Value.Left.Binding != statements[5].Binding || statements[7].Arguments[0].Binding != statements[2].Binding {
		t.Fatal("destructured symbols or checked types were lost")
	}
}

func TestObjectDestructuringEvaluatesNontrivialAndEmptySourcesOnce(t *testing.T) {
	result := Extract("bindings.ts", `interface Pair { first: number; second: number; }
function make(): Pair { return {first: 1, second: 2}; }
const before = 0, {second, first} = make(), after = first;
const {} = make();`)
	if result.Program == nil {
		t.Fatal(result.Diagnostics)
	}
	statements := result.Program.Statements
	if len(statements) != 7 || statements[2].Kind != graph.StatementVariable || statements[2].Value.Kind != graph.ExpressionCall || statements[2].Mutable || statements[3].Name != "second" || statements[4].Name != "first" || statements[5].Name != "after" || statements[6].Kind != graph.StatementExpression || statements[6].Value.Kind != graph.ExpressionCall {
		t.Fatalf("unexpected source/declaration schedule: %+v", statements)
	}
	for _, index := range []int{3, 4} {
		if statements[index].Value.Receiver.Binding != statements[2].Binding {
			t.Fatal("field did not read the single saved source")
		}
	}
	calls := 0
	walkGraphExpressions(statements, func(value *graph.Expression) {
		if value.Kind == graph.ExpressionCall {
			calls++
		}
	})
	if calls != 2 {
		t.Fatalf("got %d RHS calls, want one per pattern", calls)
	}
}

func TestObjectDestructuringAnnotationDoesNotReplaceActualShape(t *testing.T) {
	result := Extract("bindings.ts", `interface Full { value: number; other: number; }
interface View { value: number; }
const source: Full = {value: 1, other: 2};
const {value}: View = source;
const {value: fresh}: View = {value: 3};`)
	if result.Program == nil {
		t.Fatal(result.Diagnostics)
	}
	statements := result.Program.Statements
	if statements[1].Value.Receiver.Type.Shape != statements[0].Type.Shape || statements[2].Value.Kind != graph.ExpressionObject || statements[2].Type.Shape == statements[0].Type.Shape || statements[3].Value.Receiver.Binding != statements[2].Binding {
		t.Fatal("annotation substituted an existing identity or failed to contextually type a fresh literal")
	}
}

func TestObjectDestructuringNamedLimits(t *testing.T) {
	const prelude = `interface Child { n: number; } interface Item { n: number; child: Child; } const source: Item = {n: 1, child: {n: 2}}; `
	cases := []struct{ name, source, construct, message string }{
		{"default", prelude + `const {n = 0} = source;`, "ObjectDestructuring", "default"},
		{"rest", prelude + `const {n, ...rest} = source;`, "ObjectDestructuring", "rest"},
		{"nested", prelude + `const {child: {n}} = source;`, "ObjectDestructuring", "nested"},
		{"computed", prelude + `const {["n"]: n} = source;`, "ObjectDestructuring", "computed"},
		{"literal", prelude + `const {"n": n} = source;`, "ObjectDestructuring", "literal"},
		{"array", `const [n] = [1];`, "VariableDeclaration", "unsupported"},
		{"assignment", prelude + `let n = 0; ({n} = source);`, "ObjectLiteralExpression", "unsupported"},
		{"parameter", `interface Item { n: number; } function read({n}: Item): number { return n; }`, "Parameter", "unsupported"},
		{"for of", prelude + `const items: Item[] = [source]; for (const {n} of items) { console.log(n); }`, "VariableDeclaration", "unsupported"},
		{"nonrecord", `const {length} = "text";`, "ObjectDestructuring", "named plain record"},
		{"anonymous", `const {n} = {n: 1};`, "ObjectLiteralExpression", "anonymous shape"},
		{"composite annotation", `interface First { n: number; } interface Second { n: number; } interface Source { child: First; } interface View { child: Second; } const source: Source = {child: {n: 1}}; const {child}: View = source;`, "BindingElement", "distinct named shapes"},
		{"prototype field", `interface Item { __proto__: number; } const source: Item = {__proto__: 1}; const {__proto__: value} = source;`, "PrototypeObjectLiteral", "prototype setter"},
		{"inherited ordinary field", `interface Proto { n: number; } interface Item { __proto__: Proto; n?: number; } const source: Item = {__proto__: {n: 1}}; const {n} = source;`, "PrototypeObjectLiteral", "prototype setter"},
		{"empty prototype shape", `interface Item { __proto__?: number; } const source: Item = {}; const {} = source;`, "ObjectDestructuring", "prototype lookup"},
		{"inherited source receiver", `interface Item { n: number; } interface Proto { item: Item; } interface Holder { __proto__: Proto; item?: Item; } const holder: Holder = {__proto__: {item: {n: 1}}}; const {n} = holder.item!;`, "PrototypeObjectLiteral", "prototype setter"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := Extract("bindings.ts", test.source)
			if result.Program != nil || len(result.Diagnostics) != 1 {
				t.Fatalf("expected one positioned fence: %+v", result)
			}
			diagnostic := result.Diagnostics[0]
			if diagnostic.Construct != test.construct || !strings.Contains(diagnostic.Message, test.message) || diagnostic.SourcePath != "bindings.ts" || diagnostic.Position.Line < 1 || diagnostic.Position.Column < 1 {
				t.Fatalf("unexpected diagnostic: %+v", diagnostic)
			}
		})
	}
}
