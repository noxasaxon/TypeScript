package checked

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestPublicDependencyProjectFrozenInventory(t *testing.T) {
	portfolio := sourcefixture.Get(t, "portfolio")
	if portfolio == "" {
		t.Fatal("explicit unchanged frozen portfolio required")
	}
	config := filepath.Join(portfolio, "tsconfig.json")
	if _, ds := ReadProjectWithOptions(config, "inventory/route.ts", ProjectOptions{}); len(ds) == 0 {
		t.Fatal("default policy discarded missing dependency types")
	}
	p, ds := ReadProjectWithOptions(config, "inventory/route.ts", ProjectOptions{DependencyTypesInferJS})
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if len(p.config.FileNames()) != 10 || len(p.dependency.Manifest.Policy.ConfiguredRoots) != 10 {
		t.Fatal("configured roots changed")
	}
	checked, ds := p.Check()
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if checked.Compiler != p.dependency.Program {
		t.Fatal("checker Program identity changed")
	}
	if len(checked.RuntimeFiles) != 6 {
		t.Fatalf("runtime files: %d", len(checked.RuntimeFiles))
	}
	if checked.RuntimeFiles[len(checked.RuntimeFiles)-1] != checked.Entry {
		t.Fatal("entry initialized before dependencies")
	}
	source := p.SourceFiles()
	source[p.Entry] = "changed"
	metadata := p.ResolutionManifest()
	metadata[0] = '!'
	if !json.Valid(p.ResolutionManifest()) || p.SourceFiles()[p.Entry] == "changed" {
		t.Fatal("public snapshot is mutable")
	}
	// Actual package bodies are present for checking; declaration aliases do not
	// become executable CJS bodies until the runtime-binding seam is available.
	cookie, ds := ReadProjectWithOptions(config, "cookie-route/route.ts", ProjectOptions{DependencyTypesInferJS})
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if _, ds = cookie.Check(); len(ds) == 0 {
		t.Fatal("CJS body silently treated as declaration")
	}
}

func TestPublicDependencyProjectRetainsConfiguredErrorsAndSnapshot(t *testing.T) {
	dir := packagePrototypeDir(t)
	write := func(name, source string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"type":"module"}`)
	write("tsconfig.json", `{"compilerOptions":{"strict":true,"module":"NodeNext","noEmit":true,"allowImportingTsExtensions":true},"files":["entry.ts","unrelated.ts"]}`)
	write("entry.ts", `export function run(): number { return 1; }`)
	write("unrelated.ts", `const bad: string = 2; export {};`)
	if _, ds := ReadProjectWithOptions(filepath.Join(dir, "tsconfig.json"), "entry.ts", ProjectOptions{DependencyTypesInferJS}); len(ds) == 0 || !strings.Contains(ds[0].String(), "unrelated.ts") {
		t.Fatalf("whole configured project diagnostic lost: %v", ds)
	}
	write("unrelated.ts", `export const valid: number = 2;`)
	p, ds := ReadProjectWithOptions(filepath.Join(dir, "tsconfig.json"), "entry.ts", ProjectOptions{DependencyTypesInferJS})
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	write("entry.ts", `this is invalid TypeScript`)
	if _, ds = p.Check(); len(ds) != 0 {
		t.Fatal("snapshot consulted changed filesystem", ds)
	}
	if !strings.Contains(p.SourceFiles()[p.Entry], "return 1") {
		t.Fatal("snapshot bytes changed")
	}
}
