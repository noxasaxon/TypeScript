package checked

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/parser"
	"github.com/microsoft/typescript-go/internal/vfs/osvfs"
)

func nodeModuleIdentity(t *testing.T, importer, specifier string) (string, string) {
	t.Helper()
	code := fmt.Sprintf(`import{createRequire}from'node:module';import{fileURLToPath,pathToFileURL}from'node:url';import fs from'node:fs';const require=createRequire(import.meta.url);const url=import.meta.resolve(%q,pathToFileURL(%q).href);if(url.startsWith('node:'))console.log(JSON.stringify([url,'builtin']));else console.log(JSON.stringify([fileURLToPath(url),await require('internal/modules/esm/get_format').defaultGetFormat(new URL(url),{source:fs.readFileSync(fileURLToPath(url))})]));`, specifier, importer)
	cmd := exec.Command("node", "--expose-internals", "--experimental-import-meta-resolve", "--input-type=module", "-e", code)
	cmd.Dir = filepath.Dir(importer)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Node identity: %v %s", err, stderr.String())
	}
	var values []string
	if err = json.Unmarshal(out, &values); err != nil || len(values) != 2 {
		t.Fatalf("Node identity %s: %v", out, err)
	}
	return values[0], values[1]
}

func TestPackagePrototypeFourEntryProject(t *testing.T) {
	portfolio := os.Getenv("TSOX_PACKAGE_PORTFOLIO")
	if portfolio == "" {
		t.Fatal("explicit frozen project required")
	}
	results := map[string]any{}
	for _, entry := range []string{"inventory/route.ts", "web-api/route.ts", "bff/route.ts", "cookie-route/route.ts"} {
		t.Run(entry, func(t *testing.T) {
			result, err := captureDependencyProject(filepath.Join(portfolio, "tsconfig.json"), entry, ProjectOptions{DependencyTypesInferJS})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Diagnostics) != 0 {
				t.Fatal(result.Diagnostics)
			}
			if len(result.Manifest.Policy.ConfiguredRoots) != 10 || len(result.Config.FileNames()) != 10 {
				t.Fatal("configured roots changed")
			}
			ids := []string{}
			for _, module := range result.Manifest.Modules {
				ids = append(ids, module.ID)
				if result.Program.GetSourceFile(module.ID) == nil {
					t.Fatalf("runtime implementation not in same checker Program: %s", module.ID)
				}
				path, format := nodeModuleIdentity(t, module.ID, "./"+filepath.Base(module.ID))
				if path != module.ID || format != module.Format {
					t.Fatalf("Node module identity: %s %s vs %+v", path, format, module)
				}
				for _, edge := range module.Dependencies {
					path, format = nodeModuleIdentity(t, edge.Edge.Importer, edge.Edge.Specifier)
					if path != edge.Module || format != edge.Format {
						t.Fatalf("Node dependency identity: %s %s vs %+v", path, format, edge)
					}
				}
			}
			if !slices.Contains(ids, filepath.Join(portfolio, entry)) {
				t.Fatal("entry not retained")
			}
			if entry == "cookie-route/route.ts" {
				for _, name := range []string{"cookie", "escape-html"} {
					runtime := filepath.Join(portfolio, "node_modules", name, "index.js")
					if name == "cookie" {
						runtime = filepath.Join(portfolio, "node_modules", name, "dist/index.js")
					}
					if !slices.Contains(ids, runtime) {
						t.Fatal("actual package not in executable closure", name, ids)
					}
				}
				resolved := result.Program.ResolveModuleName("cookie", filepath.Join(portfolio, entry), core.ModuleKindESNext)
				if !strings.HasSuffix(resolved.ResolvedFileName, ".d.ts") {
					t.Fatal("checker declaration replaced by implementation")
				}
			}
			results[entry] = result.Manifest
		})
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	if err := os.WriteFile(filepath.Join(os.Getenv("TSOX_PACKAGE_IMPLEMENTATION"), "four-entry-report.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPackagePrototypeTypeEdgesAndReplay(t *testing.T) {
	dir := packagePrototypeDir(t)
	packageWrite(t, filepath.Join(dir, "package.json"), `{"type":"module"}`)
	packageWrite(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"strict":true,"module":"NodeNext","allowImportingTsExtensions":true,"noEmit":true},"files":["entry.ts","erased.ts","kept.ts","exported.ts"]}`)
	source := `import type {X} from "./erased.ts";import {type Y} from "./kept.ts";export {type Z} from "./exported.ts";console.log("entry");`
	packageWrite(t, filepath.Join(dir, "entry.ts"), source)
	for _, name := range []string{"erased", "kept", "exported"} {
		symbol := map[string]string{"erased": "X", "kept": "Y", "exported": "Z"}[name]
		packageWrite(t, filepath.Join(dir, name+".ts"), fmt.Sprintf(`export interface %s{};console.log(%q);`, symbol, name))
	}
	result, err := captureDependencyProject(filepath.Join(dir, "tsconfig.json"), "entry.ts", ProjectOptions{DependencyTypesInferJS})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatal(result.Diagnostics)
	}
	if len(result.Manifest.Policy.ConfiguredRoots) != 4 || len(result.Manifest.Modules) != 3 {
		t.Fatal("type/source closures collapsed", result.Manifest)
	}
	for _, module := range result.Manifest.Modules {
		if strings.HasSuffix(module.ID, "/erased.ts") {
			t.Fatal("whole type import executed")
		}
	}
	if out := packageNode(t, dir, `await import("./entry.ts");`); out != "kept\nexported\nentry" {
		t.Fatal(out)
	}
	packageWrite(t, filepath.Join(dir, "kept.ts"), `import "./unobserved.ts";export interface Y{}`)
	if result.Snapshot.Revalidate(osvfs.FS()) == nil {
		t.Fatal("changed implementation epoch accepted")
	}
	replay := replayPackages(result.Snapshot)
	again, _, err := closeRuntimeModules(filepath.Join(dir, "entry.ts"), replay)
	if err != nil || len(again) != 3 || replay.Fault() != nil {
		t.Fatal("replay observed changed filesystem", again, err, replay.Fault())
	}
}

func TestPackagePrototypeRuntimeAttributeBoundary(t *testing.T) {
	for _, source := range []string{`import value from "./data.json" with {type:"json"};`, `export{default}from"./data.json" with {type:"json"};`} {
		file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/virtual/entry.ts", Path: "/virtual/entry.ts"}, source, core.ScriptKindTS)
		if _, err := sourceRuntimeImports(file); err == nil {
			t.Fatal("source dependency effects omitted", source)
		}
	}
}
