package tsox_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/tsox"
	"github.com/microsoft/typescript-go/tsox/mutation"
)

func TestMutationSitesReturnsExactCheckedTextCoordinates(t *testing.T) {
	t.Parallel()

	source := `interface Box { value: number; }
function add(left: Box, right: Box): number {
  return left.value + right.value;
}
const first: Box = { value: 1 };
const second: Box = { value: 2 };
const result = add(first, second);
console.log(result);
`
	result := tsox.MutationSites("fixture.ts", source)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("MutationSites diagnostics: %v", result.Diagnostics)
	}
	if len(result.Calls) != 2 {
		t.Fatalf("MutationSites calls: got %d, want 2", len(result.Calls))
	}

	call := result.Calls[0]
	assertText(t, source, call.Span, "add(first, second)")
	assertText(t, source, call.Statement, "const result = add(first, second);")
	if got, want := texts(source, call.Arguments), []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("argument spans: got %q, want %q", got, want)
	}
	if got, want := call.ParameterTypes, []mutation.TypeIdentity{{Kind: "object", Named: "Box"}, {Kind: "object", Named: "Box"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parameter identities: got %#v, want %#v", got, want)
	}
	if call.Arguments[0].Binding == 0 || call.Arguments[1].Binding == 0 || call.Arguments[0].Binding == call.Arguments[1].Binding {
		t.Fatalf("direct argument binding identities are not distinct: %#v", call.Arguments)
	}

	var first mutation.Binding
	for _, binding := range result.Bindings {
		if binding.Name == "first" {
			first = binding
			break
		}
	}
	if first.ID == 0 {
		t.Fatal("first binding not returned")
	}
	assertText(t, source, first.Declaration, "const first: Box = { value: 1 };")
	assertText(t, source, first.Initializer, "{ value: 1 }")
	if got, want := spanTexts(source, first.Uses), []string{"first"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first binding uses: got %q, want %q", got, want)
	}
}

func TestMutationSitesReturnsCheckerDiagnosticsWithoutPartialSites(t *testing.T) {
	t.Parallel()

	result := tsox.MutationSites("bad.ts", `const value: number = "no"; console.log(value);`)
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Construct != "TypeScriptDiagnostic" {
		t.Fatalf("diagnostics: got %#v, want one TypeScript diagnostic", result.Diagnostics)
	}
	if len(result.Calls) != 0 || len(result.Bindings) != 0 {
		t.Fatalf("diagnostic result leaked partial sites: calls=%d bindings=%d", len(result.Calls), len(result.Bindings))
	}
}

func TestMutationSitesKeepsFunctionArrayAndNamedObjectIdentitiesDistinct(t *testing.T) {
	t.Parallel()

	source := `interface Box { value: number; }
function combine(callback: () => number, values: number[], box: Box): number {
  return callback() + values[0] + box.value;
}
const callback = (): number => { return 1; };
const values: number[] = [2];
const box: Box = { value: 3 };
console.log(combine(callback, values, box));`
	result := tsox.MutationSites("types.ts", source)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("MutationSites diagnostics: %v", result.Diagnostics)
	}
	var call mutation.CallSite
	for _, candidate := range result.Calls {
		if len(candidate.Arguments) == 3 {
			call = candidate
			break
		}
	}
	want := []mutation.TypeIdentity{{Kind: "function", Named: "() => number"}, {Kind: "array", Named: "number[]"}, {Kind: "object", Named: "Box"}}
	if !reflect.DeepEqual(call.ParameterTypes, want) {
		t.Fatalf("parameter identities: got %#v, want %#v", call.ParameterTypes, want)
	}
	for _, binding := range result.Bindings {
		if binding.Name == "Box" || binding.Name == "value" || binding.Name == "log" {
			t.Fatalf("non-runtime symbol leaked into bindings: %#v", binding)
		}
	}
}

func TestMutationSitesExcludesWriteTargetsFromBindingUses(t *testing.T) {
	t.Parallel()

	result := tsox.MutationSites("writes.ts", `function noop(): void { return; }
let empty: void = noop();
empty = noop();`)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("MutationSites diagnostics: %v", result.Diagnostics)
	}
	for _, binding := range result.Bindings {
		if binding.Name == "empty" && len(binding.Uses) != 0 {
			t.Fatalf("assignment target leaked into binding uses: %#v", binding.Uses)
		}
	}
}

func TestMutationSitesKeepsDistinctAnonymousObjectIdentitiesConservative(t *testing.T) {
	t.Parallel()

	result := tsox.MutationSites("anonymous.ts", `const left = { value: 1 };
const right = { value: 2 };
console.log(left.value + right.value);`)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("MutationSites diagnostics: %v", result.Diagnostics)
	}
	identities := make(map[string]mutation.TypeIdentity)
	for _, binding := range result.Bindings {
		if binding.Name == "left" || binding.Name == "right" {
			identities[binding.Name] = binding.Type
		}
	}
	if identities["left"].Kind != "object" || identities["right"].Kind != "object" || identities["left"].Named == "" || identities["left"] == identities["right"] {
		t.Fatalf("anonymous identities are not conservative: %#v", identities)
	}
}

func TestMutationSitesReturnsLiteralSlotsWithContextualTypes(t *testing.T) {
	t.Parallel()

	source := `interface Handle { value: number; }
interface Holder { item: Handle; }
function wrap(parameter: Handle): Holder {
  const local: Handle = parameter;
  const holder: Holder = { item: { value: 1 } };
  const populated: Handle[] = [{ value: 2 }];
  const empty: Handle[] = [];
  return holder;
}`
	result := tsox.MutationSites("literals.ts", source)
	if len(result.Diagnostics) != 0 {
		t.Fatalf("MutationSites diagnostics: %v", result.Diagnostics)
	}
	if got, want := len(result.Literals), 5; got != want {
		t.Fatalf("literal sites: got %d, want %d: %#v", got, want, result.Literals)
	}

	holder := result.Literals[0]
	assertText(t, source, holder.Span, "{ item: { value: 1 } }")
	assertText(t, source, holder.Statement, "const holder: Holder = { item: { value: 1 } };")
	if holder.Kind != "object" || len(holder.Slots) != 1 || holder.Slots[0].Field != "item" {
		t.Fatalf("holder literal: got %#v", holder)
	}
	assertText(t, source, holder.Slots[0].Span, "{ value: 1 }")
	if got, want := holder.Slots[0].Type, (mutation.TypeIdentity{Kind: "object", Named: "Handle"}); got != want {
		t.Fatalf("holder item contextual identity: got %#v, want %#v", got, want)
	}

	populated := result.Literals[2]
	if populated.Kind != "array" || len(populated.Slots) != 1 {
		t.Fatalf("populated array literal: got %#v", populated)
	}
	assertText(t, source, populated.Slots[0].Span, "{ value: 2 }")
	if got, want := populated.ElementType, (mutation.TypeIdentity{Kind: "object", Named: "Handle"}); got != want || populated.Slots[0].Type != want {
		t.Fatalf("populated element contextual identity: got element=%#v slot=%#v, want %#v", got, populated.Slots[0].Type, want)
	}
	if got, want := source[populated.Insertion:populated.Insertion], ""; got != want || populated.Insertion != populated.Slots[0].Span.End {
		t.Fatalf("populated insertion: got %d, want slot end %d", populated.Insertion, populated.Slots[0].Span.End)
	}

	empty := result.Literals[4]
	if empty.Kind != "array" || len(empty.Slots) != 0 || empty.Insertion <= empty.Span.Start || empty.Insertion >= empty.Span.End {
		t.Fatalf("empty array literal: got %#v", empty)
	}
	if source[empty.Insertion-1:empty.Insertion+1] != "[]" {
		t.Fatalf("empty insertion is not between brackets: %d", empty.Insertion)
	}

	declarations := make(map[string]string)
	for _, binding := range result.Bindings {
		if binding.Name == "parameter" || binding.Name == "wrap" {
			declarations[binding.Name] = source[binding.Declaration.Start:binding.Declaration.End]
		}
	}
	if declarations["parameter"] != "parameter: Handle" {
		t.Fatalf("parameter declaration span: got %q", declarations["parameter"])
	}
	if !strings.HasPrefix(declarations["wrap"], "function wrap") || !strings.HasSuffix(declarations["wrap"], "}") {
		t.Fatalf("function declaration span: got %q", declarations["wrap"])
	}
}

func assertText(t *testing.T, source string, span mutation.Span, want string) {
	t.Helper()
	if got := source[span.Start:span.End]; got != want {
		t.Fatalf("span [%d:%d] text: got %q, want %q", span.Start, span.End, got, want)
	}
}

func texts(source string, arguments []mutation.Argument) []string {
	result := make([]string, len(arguments))
	for index, argument := range arguments {
		result[index] = source[argument.Span.Start:argument.Span.End]
	}
	return result
}

func spanTexts(source string, spans []mutation.Span) []string {
	result := make([]string, len(spans))
	for index, span := range spans {
		result[index] = source[span.Start:span.End]
	}
	return result
}
