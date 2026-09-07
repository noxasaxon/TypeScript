package tsox_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/typescript-go/tsox"
)

func TestProjectPublicSnapshotAndAsyncExtraction(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"type":"module"}`)
	write("tsconfig.json", `{"compilerOptions":{"verbatimModuleSyntax":true,"strict":true,"module":"NodeNext","noEmit":true,"allowImportingTsExtensions":true},"files":["main.ts"]}`)
	write("main.ts", `interface Input { key:string; }
interface Output { value:string; }
declare function read(key:string):Promise<string>;
export async function handle(input:Input):Promise<Output> {
 const value = await read(input.key);
 return {value:value};
}`)
	project, ds := tsox.ReadProject(filepath.Join(directory, "tsconfig.json"), "main.ts")
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if project.ConfigPath != filepath.Join(directory, "tsconfig.json") || project.Entry != filepath.Join(directory, "main.ts") {
		t.Fatal("noncanonical public identities")
	}
	write("main.ts", `invalid changed source`)
	write("tsconfig.json", `invalid changed config`)
	project.SourceFiles()[project.Entry] = "invalid accidental public mutation"
	result := tsox.ExtractAsyncProject(project, "handle", "read")
	if len(result.Diagnostics) != 0 || result.Program == nil {
		t.Fatalf("public snapshot changed: %+v", result.Diagnostics)
	}
	if result.Program.Position.Line != 4 {
		t.Fatalf("lost source position: %+v", result.Program.Position)
	}
	if result.Program.Module.SourcePath != project.Entry {
		t.Fatal("lost source path")
	}
}

func TestProjectPublicOrdinaryExtractionAndInvalidSnapshot(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, text := range map[string]string{
		"package.json":  `{"type":"module"}`,
		"tsconfig.json": `{"compilerOptions":{"verbatimModuleSyntax":true,"strict":true,"module":"NodeNext","noEmit":true,"allowImportingTsExtensions":true},"files":["main.ts"]}`,
		"main.ts":       `export const answer:number = 42;`,
	} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, ds := tsox.ReadProject(filepath.Join(directory, "tsconfig.json"), "main.ts")
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if result := tsox.ExtractProject(p); result.Program == nil || len(result.Diagnostics) != 0 {
		t.Fatal(result.Diagnostics)
	}
	for _, p := range []*tsox.Project{nil, {}} {
		if result := tsox.ExtractProject(p); len(result.Diagnostics) == 0 {
			t.Fatal("invalid project accepted")
		}
		if result := tsox.ExtractAsyncProject(p, "handle", "read"); len(result.Diagnostics) == 0 {
			t.Fatal("invalid async project accepted")
		}
	}
}

func TestAsyncProjectRequiresActualExportAndResolvesAlias(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source := `interface Input { key:string; } interface Output { value:string; }
declare function read(key:string):Promise<string>;
async function handle(input:Input):Promise<Output> { const value = await read(input.key); return {value:value}; }
`
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(directory, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"type":"module"}`)
	write("tsconfig.json", `{"compilerOptions":{"strict":true,"module":"NodeNext","noEmit":true,"allowImportingTsExtensions":true},"files":["main.ts"]}`)
	write("main.ts", source)
	p, ds := tsox.ReadProject(filepath.Join(directory, "tsconfig.json"), "main.ts")
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if r := tsox.ExtractAsyncProject(p, "handle", "read"); r.Program != nil || len(r.Diagnostics) == 0 {
		t.Fatal("unexported function accepted as public entry")
	}
	write("main.ts", source+`export {handle as serve};`)
	p, ds = tsox.ReadProject(filepath.Join(directory, "tsconfig.json"), "main.ts")
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if r := tsox.ExtractAsyncProject(p, "serve", "read"); r.Program == nil {
		t.Fatal(r.Diagnostics)
	}
}
