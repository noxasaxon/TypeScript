package tsox_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/tsox"
)

func TestMutationSitesModulesKeepsSourceCoordinatesAndBindingDomainsSeparate(t *testing.T) {
	sources := map[string]string{
		"app/main.ts": `import { run } from "../shared.ts"; console.log(run());`,
		"shared.ts": "// café 😀 before every source span\n" + `import type { Box } from "./types.ts";
function add(left: Box, right: Box): number { return left.value + right.value; }
export function run(): number { const first: Box = { value: 1 }; const second: Box = { value: 2 }; return add(first, second); }`,
		"types.ts":  `export interface Box { value: number; }`,
		"unused.ts": `this source is neither imported nor checked`,
	}
	result := tsox.MutationSitesModules("app/main.ts", sources)
	if len(result.Diagnostics) != 0 || len(result.Files) != 3 {
		t.Fatalf("module sites: %+v", result)
	}
	for index, name := range []string{"app/main.ts", "shared.ts", "types.ts"} {
		file := result.Files[index]
		if file.SourcePath != name {
			t.Fatalf("file order = %s, want %s", file.SourcePath, name)
		}
		if name == "app/main.ts" && !reflect.DeepEqual(file.Sites, tsox.MutationSitesFiles(name, sources)) {
			t.Fatal("legacy entry sites changed")
		}
		for _, call := range file.Sites.Calls {
			text := sources[name][call.Span.Start:call.Span.End]
			if !strings.Contains(text, "(") || !strings.HasSuffix(text, ")") {
				t.Fatalf("call spans do not refer to %s: %q", name, text)
			}
		}
		if name == "shared.ts" {
			if len(file.Sites.Calls) != 1 {
				t.Fatalf("dependency calls = %+v", file.Sites.Calls)
			}
			call := file.Sites.Calls[0]
			assertText(t, sources[name], call.Span, "add(first, second)")
			assertText(t, sources[name], call.Statement, "return add(first, second);")
			for argumentIndex, bindingName := range []string{"first", "second"} {
				argument := call.Arguments[argumentIndex]
				if argument.Binding == 0 || argument.Type.Named != "Box" {
					t.Fatalf("dependency argument lost checked binding/type: %+v", argument)
				}
				found := false
				for _, binding := range file.Sites.Bindings {
					if binding.ID == argument.Binding && binding.Name == bindingName {
						found = true
						assertText(t, sources[name], binding.Initializer, map[string]string{"first": "{ value: 1 }", "second": "{ value: 2 }"}[bindingName])
					}
				}
				if !found {
					t.Fatalf("binding %s is not in its own source domain", bindingName)
				}
			}
		}
	}
	if repeated := tsox.MutationSitesModules("app/main.ts", sources); !reflect.DeepEqual(result, repeated) {
		t.Fatal("repeated all-module extraction changed")
	}
}

func TestMutationSitesModulesReportsDependencyDiagnosticWithoutPartialSites(t *testing.T) {
	result := tsox.MutationSitesModules("main.ts", map[string]string{
		"main.ts":       `import { value } from "./dependency.ts"; console.log(value);`,
		"dependency.ts": "// café 😀\nexport const value: number = \"bad\";",
	})
	if len(result.Files) != 0 || len(result.Diagnostics) != 1 {
		t.Fatalf("expected diagnostic without partial sites: %+v", result)
	}
	diagnostic := result.Diagnostics[0]
	if diagnostic.SourcePath != "dependency.ts" || diagnostic.Position.Line != 2 || diagnostic.Position.Column != 14 {
		t.Fatalf("wrong dependency diagnostic coordinates: %+v", diagnostic)
	}
}

func TestMutationSitesModulesIncludesReexportOnlyAndErasedSources(t *testing.T) {
	sources := map[string]string{
		"app/main.ts": `export { run as entry } from "../barrel.ts";`,
		"barrel.ts":   `export { run } from "./shared.ts"; export type { Box } from "./types.ts";`,
		"shared.ts":   "// café 😀\n" + `import type { Box } from "./types.ts"; function add(left: Box, right: Box): number { return left.value + right.value; } export function run(): number { const first: Box = { value: 1 }; const second: Box = { value: 2 }; return add(first, second); }`,
		"types.ts":    "// distinct source offsets 😀\n" + `export interface Box { value: number; } function size(value: number): number { return value; } const result = size(8);`,
		"unused.ts":   `invalid unreachable source`,
	}
	original := make(map[string]string, len(sources))
	for name, source := range sources {
		original[name] = source
	}
	result := tsox.MutationSitesModules("app/main.ts", sources)
	if len(result.Diagnostics) != 0 || len(result.Files) != 4 {
		t.Fatalf("module sites: %+v", result)
	}
	for index, name := range []string{"app/main.ts", "barrel.ts", "shared.ts", "types.ts"} {
		file := result.Files[index]
		if file.SourcePath != name {
			t.Fatalf("file %s, want %s", file.SourcePath, name)
		}
		switch name {
		case "shared.ts":
			if len(file.Sites.Calls) != 1 {
				t.Fatalf("runtime dependency calls: %+v", file.Sites.Calls)
			}
			assertText(t, sources[name], file.Sites.Calls[0].Span, "add(first, second)")
			for _, argument := range file.Sites.Calls[0].Arguments {
				found := false
				for _, binding := range file.Sites.Bindings {
					if binding.ID == argument.Binding {
						found = true
						assertText(t, sources[name], binding.Initializer, map[string]string{"first": "{ value: 1 }", "second": "{ value: 2 }"}[binding.Name])
					}
				}
				if !found {
					t.Fatalf("argument has no source-local binding: %+v", argument)
				}
			}
		case "types.ts":
			if len(file.Sites.Calls) != 1 {
				t.Fatalf("erased dependency sites missing: %+v", file.Sites.Calls)
			}
			assertText(t, sources[name], file.Sites.Calls[0].Span, "size(8)")
		}
	}
	if !reflect.DeepEqual(sources, original) || !reflect.DeepEqual(result, tsox.MutationSitesModules("app/main.ts", original)) {
		t.Fatal("source map or deterministic mutation snapshot changed")
	}
}
