package extract

import (
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/tsox/graph"
)

func TestPrototypeObjectLiteralDiagnosesInheritedOrdinaryRead(t *testing.T) {
	// Node prints 7. Treating __proto__ as an own struct field instead made
	// holder.child absent and printed undefined in both emission modes.
	source := `interface Child { n: number; }
interface Proto { child?: Child; }
interface Holder { __proto__: Proto; child?: Child; }
const proto: Proto = { child: {n: 7} };
const holder: Holder = {
  __proto__: proto
};
console.log(holder.child?.n);`
	assertPrototypeObjectLiteralFence(t, Extract("prototype.ts", source), "prototype.ts", graph.Position{Line: 6, Column: 3})
}

func TestPrototypeObjectLiteralRecognizesSetterSyntax(t *testing.T) {
	for _, name := range []string{`__proto__`, `"__proto__"`, `'__proto__'`, `__pr\u006fto__`, `"__pr\u006fto__"`} {
		t.Run(name, func(t *testing.T) {
			source := "interface Proto { n: number; } interface Holder { __proto__: Proto; }\nconst holder: Holder = {\n  " + name + ": {n: 7}\n};"
			assertPrototypeObjectLiteralFence(t, Extract("prototype.ts", source), "prototype.ts", graph.Position{Line: 3, Column: 3})
		})
	}
}

func TestPrototypeObjectLiteralDiagnosesPrimitiveAndNullInitializers(t *testing.T) {
	for _, initializer := range []string{"1", `"value"`, "true", "undefined", "null", "[]", "{}", "(): number => 1"} {
		t.Run(initializer, func(t *testing.T) {
			// The syntax itself is unsupported even without a contextual shape.
			// A primitive setter RHS creates no own property; null changes the
			// prototype. Neither is an ordinary field initializer.
			source := "console.log({\n  __proto__: " + initializer + "\n});"
			assertPrototypeObjectLiteralFence(t, Extract("prototype.ts", source), "prototype.ts", graph.Position{Line: 2, Column: 3})
		})
	}
	for _, test := range []struct{ typ, initializer string }{{"number", "1"}, {"string", `"value"`}, {"boolean", "true"}, {"number | undefined", "undefined"}} {
		t.Run("named "+test.typ, func(t *testing.T) {
			source := "interface Holder { __proto__: " + test.typ + "; }\nconst holder: Holder = {\n  __proto__: " + test.initializer + "\n};\nconsole.log(holder.__proto__);"
			assertPrototypeObjectLiteralFence(t, Extract("prototype.ts", source), "prototype.ts", graph.Position{Line: 3, Column: 3})
		})
	}
}

func TestPrototypeObjectLiteralDiagnosesEveryExtractedContext(t *testing.T) {
	prelude := "interface Proto { n: number; } interface Holder { __proto__?: Proto; n?: number; }\n"
	cases := []struct{ name, before, after string }{
		{"initializer", "const value: Holder = {", "};"},
		{"return", "function make(): Holder { return {", "}; }"},
		{"arrow return", "const make = (): Holder => { return {", "}; };"},
		{"call argument", "function read(value: Holder): void {} read({", "});"},
		{"array element", "const values: Holder[] = [{", "}];"},
		{"nested field", "interface Outer { value: Holder; } const outer: Outer = {value: {", "}};"},
		{"assignment", "let value: Holder = {}; value = {", "};"},
		{"parameter default", "function read(value: Holder = {", "}): void {}"},
		{"property receiver", "console.log(({", "}).__proto__.n);"},
		{"optional field slot", "interface Outer { value?: Holder; } const outer: Outer = {value: {", "}};"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			source := prelude + test.before + "\n  __proto__: {n: 7}\n" + test.after
			assertPrototypeObjectLiteralFence(t, Extract("prototype.ts", source), "prototype.ts", graph.Position{Line: 3, Column: 3})
		})
	}
}

func TestPrototypeObjectLiteralKeepsDependencyPosition(t *testing.T) {
	result := ExtractFiles("app/main.ts", map[string]string{
		"app/main.ts": `import { value } from "../value.ts"; console.log(value.n);`,
		"value.ts":    "interface Proto { n: number; } interface Holder { __proto__: Proto; n?: number; }\nexport const value: Holder = {\n  __proto__: {n: 7}\n};",
	})
	assertPrototypeObjectLiteralFence(t, result, "value.ts", graph.Position{SourcePath: "value.ts", Line: 3, Column: 3})
}

func TestPrototypeObjectLiteralPreservesOrdinaryOwnPropertyRules(t *testing.T) {
	for _, source := range []string{
		`interface Holder { n: number; prototype: number; __proto: number; __proto___: number; } const holder: Holder = {n: 1, prototype: 2, __proto: 3, __proto___: 4}; console.log(holder.n);`,
		`const __proto__ = 7; interface Holder { n: number; } const holder: Holder = {n: __proto__}; console.log(holder.n);`,
		`interface Holder { __proto__?: number; n: number; } const holder: Holder = {n: 7}; console.log(holder.n);`,
	} {
		if result := Extract("prototype.ts", source); result.Program == nil || len(result.Diagnostics) != 0 {
			t.Fatalf("ordinary own-field program rejected: %+v", result)
		}
	}
	// These own-property forms are not yet supported. They must retain their
	// actual syntax fences, never be mistaken for prototype setters.
	for _, test := range []struct{ name, source, construct string }{
		{"shorthand", `const __proto__ = 7; interface Holder { __proto__: number; } const holder: Holder = {__proto__};`, "ShorthandPropertyAssignment"},
		{"computed literal", `interface Holder { __proto__: number; } const holder: Holder = {["__proto__"]: 7};`, "PropertyAssignment"},
		{"computed binding", `const key = "__proto__"; interface Holder { __proto__: number; } const holder: Holder = {[key]: 7};`, "PropertyAssignment"},
		{"method", `interface Holder { __proto__: () => number; } const holder: Holder = {__proto__(): number { return 7; }};`, "MethodDeclaration"},
		{"ordinary quoted field", `interface Holder { n: number; } const holder: Holder = {"n": 7};`, "PropertyAssignment"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := Extract("prototype.ts", test.source)
			if result.Program != nil || len(result.Diagnostics) != 1 || result.Diagnostics[0].Construct != test.construct {
				t.Fatalf("own-property syntax acquired prototype semantics: %+v", result)
			}
		})
	}
}

func assertPrototypeObjectLiteralFence(t *testing.T, result graph.Result, sourcePath string, position graph.Position) {
	t.Helper()
	if result.Program != nil || len(result.Diagnostics) != 1 {
		t.Fatalf("want one prototype setter diagnostic, got %+v", result)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.Construct != "PrototypeObjectLiteral" || diagnostic.SourcePath != sourcePath || diagnostic.Position != position || !strings.Contains(diagnostic.Message, "__proto__ prototype setter") || !strings.Contains(diagnostic.Message, "no own property") {
		t.Fatalf("unexpected prototype setter diagnostic: %+v", diagnostic)
	}
}
