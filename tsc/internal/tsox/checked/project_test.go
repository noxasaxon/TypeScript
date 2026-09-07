package checked

import (
	"github.com/microsoft/typescript-go/internal/core"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const projectOptions = `"strict":true,"module":"NodeNext","noEmit":true,"allowImportingTsExtensions":true`

func projectFixture(t *testing.T, files map[string]string) (string, func(string, string)) {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		t.Helper()
		name = filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"type":"module"}`)
	write("tsconfig.json", `{"compilerOptions":{`+projectOptions+`},"include":["src/**/*.ts"]}`)
	for name, text := range files {
		write(name, text)
	}
	return directory, write
}

func TestProjectJSONCExtendsRootsAndSnapshot(t *testing.T) {
	directory, write := projectFixture(t, map[string]string{
		"base.json": `{ // inherited compiler options and trailing commas
    "compilerOptions": {` + projectOptions + `,},
  }`,
		"tsconfig.json":   `{"extends":"./base.json","include":["src/**/*.ts"],"exclude":["src/excluded.ts"]}`,
		"src/main.ts":     `import { value } from "./excluded.ts"; export const answer = value;`,
		"src/excluded.ts": `export const value = 42;`,
		"src/other.ts":    `export const other = 7;`,
		"unselected.ts":   `this should not be parsed`,
	})
	p, ds := ReadProject(filepath.Join(directory, "tsconfig.json"), "src/main.ts")
	if len(ds) != 0 {
		t.Fatalf("read: %+v", ds)
	}
	for _, name := range []string{"base.json", "tsconfig.json", "package.json", "src/main.ts", "src/excluded.ts", "src/other.ts"} {
		if _, ok := p.SourceFiles()[filepath.Join(directory, name)]; !ok {
			t.Errorf("missing fingerprint input %s", name)
		}
	}
	if _, ok := p.SourceFiles()[filepath.Join(directory, "unselected.ts")]; ok {
		t.Fatal("read non-root unreachable source")
	}
	program, ds := p.Check()
	if len(ds) != 0 {
		t.Fatalf("check: %+v", ds)
	}
	if len(program.RuntimeFiles) != 2 || filepath.Base(program.RuntimeFiles[0].FileName()) != "excluded.ts" {
		t.Fatal("dependency evaluation order changed")
	}
	if program.Compiler.Options().Module != core.ModuleKindNodeNext {
		t.Fatal("checker discarded project options")
	}
	write("src/excluded.ts", `export const value:number = "changed";`)
	write("base.json", `broken config`)
	write("package.json", `{"type":"commonjs"}`)
	if _, ds := p.Check(); len(ds) != 0 {
		t.Fatalf("snapshot reread disk: %+v", ds)
	}
}

func TestProjectCheckerHonorsConfiguredOptionAndAllRoots(t *testing.T) {
	directory, write := projectFixture(t, map[string]string{
		"src/main.ts":  `export const answer = 42;`,
		"src/other.ts": `export function other():number { const unused = 3; return 7; }`,
	})
	check := func() []string {
		t.Helper()
		p, ds := ReadProject(filepath.Join(directory, "tsconfig.json"), "src/main.ts")
		if len(ds) != 0 {
			t.Fatalf("read: %+v", ds)
		}
		_, ds = p.Check()
		var result []string
		for _, d := range ds {
			result = append(result, d.SourcePath+":"+d.Message)
		}
		return result
	}
	if ds := check(); len(ds) != 0 {
		t.Fatal(ds)
	}
	write("tsconfig.json", `{"compilerOptions":{`+projectOptions+`,"noUnusedLocals":true},"include":["src/**/*.ts"]}`)
	if ds := check(); len(ds) == 0 || !strings.Contains(ds[0], "other.ts") || !strings.Contains(ds[0], "unused") {
		t.Fatalf("configured noUnusedLocals ignored on non-entry root: %v", ds)
	}
}

func TestProjectEntrySelection(t *testing.T) {
	directory, _ := projectFixture(t, map[string]string{
		"tsconfig.json": `{"compilerOptions":{` + projectOptions + `},"include":["src/*.ts"],"exclude":["src/excluded.ts"]}`,
		"src/main.ts":   `export const value=1;`, "src/excluded.ts": `export const value=2;`,
	})
	for _, entry := range []string{"", "src/excluded.ts", "missing.ts"} {
		if _, ds := ReadProject(filepath.Join(directory, "tsconfig.json"), entry); len(ds) == 0 || ds[0].Construct != "ProjectEntry" {
			t.Fatalf("entry %q: %+v", entry, ds)
		}
	}
}

func TestProjectConfigDiagnosticsKeepExtendedSourcePosition(t *testing.T) {
	for _, base := range []string{"{\n \"compilerOptions\": {\n \"strict\": \"yes\"\n}\n}", "{\n \"compilerOptions\": {\n \"strict\": true,,\n}\n}"} {
		t.Run(base, func(t *testing.T) {
			directory, _ := projectFixture(t, map[string]string{"base.json": base, "tsconfig.json": `{"extends":"./base.json","include":["src/*.ts"]}`, "src/main.ts": `export const value=1;`})
			_, ds := ReadProject(filepath.Join(directory, "tsconfig.json"), "src/main.ts")
			if len(ds) == 0 || filepath.Base(ds[0].SourcePath) != "base.json" || ds[0].Position.Line < 2 {
				t.Fatalf("lost extends diagnostic provenance: %+v", ds)
			}
		})
	}
}

func TestProjectPackageBoundaryAndUnsupportedResolution(t *testing.T) {
	for _, tc := range []struct{ name, config, pkg string }{
		{"commonjs", "", `{"type":"commonjs"}`},
		{"malformed-package", "", "{\n bad}"},
		{"strict-disabled", `{"strict":false,"module":"NodeNext","noEmit":true,"allowImportingTsExtensions":true}`, ""},
		{"null-disabled", `{` + projectOptions + `,"strictNullChecks":false}`, ""},
		{"bundler", `{"strict":true,"module":"ESNext","moduleResolution":"Bundler","noEmit":true,"allowImportingTsExtensions":true}`, ""},
		{"suffix", `{` + projectOptions + `,"moduleSuffixes":[".native",""]}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			directory, write := projectFixture(t, map[string]string{"src/main.ts": `import {value} from "./nested/value.ts"; export const answer=value;`, "src/nested/value.ts": `export const value=1;`})
			if tc.config != "" {
				write("tsconfig.json", `{"compilerOptions":`+tc.config+`,"include":["src/**/*.ts"]}`)
			}
			if tc.pkg != "" {
				write("src/nested/package.json", tc.pkg)
			}
			if _, ds := ReadProject(filepath.Join(directory, "tsconfig.json"), "src/main.ts"); len(ds) == 0 {
				t.Fatal("unsupported config/package silently accepted")
			}
		})
	}
}

func TestProjectRequiresNodeCompatibleTypeImports(t *testing.T) {
	directory, write := projectFixture(t, map[string]string{
		"src/main.ts":  `import { Item } from "./types.ts"; export function value(item: Item): number { return item.value; }`,
		"src/types.ts": `export interface Item { value: number; }`,
	})
	p, ds := ReadProject(filepath.Join(directory, "tsconfig.json"), "src/main.ts")
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if _, ds := p.Check(); len(ds) == 0 || ds[0].Construct != "ModuleRuntimeName" {
		t.Fatalf("accepted Node-invalid type import: %+v", ds)
	}
	write("src/main.ts", `import type { Item } from "./types.ts"; export function value(item: Item): number { return item.value; }`)
	p, ds = ReadProject(filepath.Join(directory, "tsconfig.json"), "src/main.ts")
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if _, ds := p.Check(); len(ds) != 0 {
		t.Fatal(ds)
	}
	if p.config.CompilerOptions().VerbatimModuleSyntax != core.TSUnknown {
		t.Fatal("overrode verbatimModuleSyntax")
	}
}

func TestProjectRuntimeReexportsUseCheckerValues(t *testing.T) {
	for _, tc := range []struct {
		statement string
		valid     bool
	}{
		{`export { value as renamed } from "./values.ts";`, true},
		{`export { Item } from "./values.ts";`, false},
		{`export type { Item } from "./values.ts";`, true},
		{`import type { Item } from "./values.ts"; export { Item };`, false},
		{`import { value } from "./values.ts"; export { value as renamed };`, true},
	} {
		t.Run(tc.statement, func(t *testing.T) {
			directory, _ := projectFixture(t, map[string]string{"src/main.ts": tc.statement, "src/values.ts": `export const value=3; export interface Item {value:number;}`})
			p, ds := ReadProject(filepath.Join(directory, "tsconfig.json"), "src/main.ts")
			if len(ds) != 0 {
				t.Fatal(ds)
			}
			_, ds = p.Check()
			if tc.valid && len(ds) != 0 {
				t.Fatal(ds)
			}
			if !tc.valid && (len(ds) == 0 || ds[0].Construct != "ModuleRuntimeName") {
				t.Fatalf("erased export accepted: %+v", ds)
			}
		})
	}
}

func TestProjectRejectsRetainedAmbientFunctionImport(t *testing.T) {
	directory, _ := projectFixture(t, map[string]string{
		"src/main.ts": `import { read } from "./host.ts"; export const value = read;`,
		"src/host.ts": `export declare function read(key:string):Promise<string>;`,
	})
	p, ds := ReadProject(filepath.Join(directory, "tsconfig.json"), "src/main.ts")
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	_, ds = p.Check()
	if len(ds) == 0 || ds[0].Construct != "ModuleRuntimeName" {
		t.Fatalf("Node-invalid ambient import accepted: %+v", ds)
	}
}
