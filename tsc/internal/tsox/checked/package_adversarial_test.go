package checked

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/internal/vfs/osvfs"
	"github.com/microsoft/typescript-go/tsox/graph"
)

type nodeIdentity struct{ Module, Format, Error string }

func nodeResolutionResult(t *testing.T, importer, specifier string, mode RuntimeMode) nodeIdentity {
	t.Helper()
	code := fmt.Sprintf(`import{createRequire}from'node:module';import{fileURLToPath,pathToFileURL}from'node:url';import fs from'node:fs';const require=createRequire(pathToFileURL(%q));try{const url=%q==='require'?pathToFileURL(require.resolve(%q)).href:import.meta.resolve(%q,pathToFileURL(%q).href);const Module=fileURLToPath(url);const Format=await createRequire(import.meta.url)('internal/modules/esm/get_format').defaultGetFormat(new URL(url),{source:fs.readFileSync(Module)});console.log(JSON.stringify({Module,Format}));}catch(e){console.log(JSON.stringify({Error:e.code??e.name}));}`, importer, mode, specifier, specifier, importer)
	cmd := exec.Command("node", "--expose-internals", "--experimental-import-meta-resolve", "--input-type=module", "-e", code)
	cmd.Dir = filepath.Dir(importer)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err, stderr.String())
	}
	var result nodeIdentity
	if err = json.Unmarshal(out, &result); err != nil {
		t.Fatal(err, string(out))
	}
	return result
}
func TestPackagePrototypeConditionalTargets(t *testing.T) {
	dir := packagePrototypeDir(t)
	packageWrite(t, filepath.Join(dir, "package.json"), `{"type":"module"}`)
	entry := filepath.Join(dir, "entry.ts")
	packageWrite(t, entry, `export{}`)
	cases := []struct {
		name, exports, main string
		bad                 bool
	}{
		{"array-invalid", `["../bad.js","./main.js"]`, "", false},
		{"array-unselected", `[{"browser":"./other.js"},"./main.js"]`, "", false},
		{"array-null-fallback", `[null,"./main.js"]`, "", false},
		{"nested-unselected", `{"node":{"browser":"./other.js"},"default":"./main.js"}`, "", false},
		{"null-blocks-condition", `{"node":null,"default":"./main.js"}`, "", true},
		{"array-last-invalid", `[null,42]`, "", true},
		{"array-reset-error", `[42,null]`, "", true},
		{"array-missing-not-fallback", `["./missing.js","./main.js"]`, "", true},
		{"empty-array", `[]`, "", true},
		{"root-null-main", `null`, "main.js", false},
		{"duplicate-order", `{"node":"./other.js","default":"./main.js","node":"./last.js"}`, "", false},
		{"numeric-condition", `{"0":"./other.js","default":"./main.js"}`, "", true},
		{"mixed-condition-path", `{".":"./main.js","default":"./other.js"}`, "", true},
		{"mode-selection", `{"import":"./main.js","require":"./other.js"}`, "", false},
		{"node-addons", `{"node-addons":"./other.js","default":"./main.js"}`, "", false},
		{"subpath-private", `{".":"./main.js"}`, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base := filepath.Join(dir, "node_modules", c.name)
			packageWrite(t, filepath.Join(base, "package.json"), fmt.Sprintf(`{"type":"module","exports":%s,"main":%q}`, c.exports, c.main))
			for _, f := range []string{"main.js", "other.js", "last.js"} {
				packageWrite(t, filepath.Join(base, f), `export const value=1;`)
			}
			for _, mode := range []RuntimeMode{RuntimeImportESM, RuntimeRequire} {
				oracle := nodeResolutionResult(t, entry, c.name, mode)
				fs := capturePackages(osvfs.FS())
				edge := RuntimeImport{entry, c.name, mode, graph.Position{Line: 3, Column: 5}}
				result, err := (&packageResolver{fs}).ResolveRuntime(edge)
				if c.bad {
					if err == nil || oracle.Error == "" {
						t.Fatalf("invalid target accepted: native%+v/%v Node%+v", result, err, oracle)
					}
					continue
				}
				if err != nil || oracle.Error != "" || result.Module != oracle.Module || result.Format != oracle.Format {
					t.Fatalf("native%+v/%v Node%+v", result, err, oracle)
				}
				snapshot, err := fs.Freeze()
				if err != nil {
					t.Fatal(err)
				}
				again, err := (&packageResolver{replayPackages(snapshot)}).ResolveRuntime(edge)
				if err != nil || again != result {
					t.Fatal("resolution replay", again, err)
				}
			}
		})
	}
}
func TestPackagePrototypeImportsAndSelf(t *testing.T) {
	dir := packagePrototypeDir(t)
	entry := filepath.Join(dir, "entry.ts")
	packageWrite(t, entry, `export{}`)
	packageWrite(t, filepath.Join(dir, "package.json"), `{"name":"self","type":"module","exports":{".":"./main.js","./sub":"./other.js"},"imports":{"#local":{"import":"./main.js","require":"./other.js"},"#external":"dep","#blocked":null}}`)
	for _, f := range []string{"main.js", "other.js"} {
		packageWrite(t, filepath.Join(dir, f), `export const value=1;`)
	}
	packageWrite(t, filepath.Join(dir, "node_modules/dep/package.json"), `{"main":"index.js","type":"commonjs"}`)
	packageWrite(t, filepath.Join(dir, "node_modules/dep/index.js"), `module.exports=1;`)
	for _, name := range []string{"#local", "#external", "self", "self/sub", "self/private", "#blocked", "#missing"} {
		for _, mode := range []RuntimeMode{RuntimeImportESM, RuntimeRequire} {
			t.Run(name+string(mode), func(t *testing.T) {
				oracle := nodeResolutionResult(t, entry, name, mode)
				result, err := (&packageResolver{capturePackages(osvfs.FS())}).ResolveRuntime(RuntimeImport{entry, name, mode, graph.Position{Line: 1, Column: 1}})
				if oracle.Error != "" {
					if err == nil {
						t.Fatal("Node rejected", oracle, result)
					}
					return
				}
				if err != nil || result.Module != oracle.Module || result.Format != oracle.Format {
					t.Fatal(result, err, oracle)
				}
			})
		}
	}
}

func TestPackagePrototypeSourceFormats(t *testing.T) {
	dir := packagePrototypeDir(t)
	entry := filepath.Join(dir, "entry.ts")
	packageWrite(t, filepath.Join(dir, "package.json"), `{"type":"module"}`)
	packageWrite(t, entry, `export{}`)
	cases := []struct {
		name, kind, extension, source string
		boundary                      bool
	}{
		{"plain", "", ".js", `module.exports=1;`, false},
		{"syntax-export", "", ".js", `export const value=1;`, false},
		{"syntax-import-meta", "", ".js", `console.log(import.meta.url);`, false},
		{"dynamic-only", "", ".js", `void import("node:path");`, false},
		{"top-return", "", ".js", `return;`, false},
		{"mjs", "", ".mjs", `export const value=1;`, false},
		{"cjs", "", ".cjs", `module.exports=1;`, false},
		{"explicit-module", "module", ".js", `console.log("no imports");`, false},
		{"explicit-commonjs", "commonjs", ".js", `module.exports=1;`, false},
		{"nested-await", "", ".js", `async function run(){await 1;}module.exports=run;`, true},
		{"top-await", "", ".js", `await 1;`, true},
		{"wrapper-binding", "", ".js", `const require=1;`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base := filepath.Join(dir, "node_modules", c.name)
			filename := filepath.Join(base, "index"+c.extension)
			packageWrite(t, filepath.Join(base, "package.json"), fmt.Sprintf(`{"main":%q,"type":%q}`, "index"+c.extension, c.kind))
			packageWrite(t, filename, c.source)
			oracle := nodeResolutionResult(t, entry, c.name, RuntimeImportESM)
			if oracle.Error != "" {
				t.Fatal("Node format probe", oracle)
			}
			got, err := packageSourceFormat(filename, c.source, c.kind)
			if c.boundary {
				if err == nil {
					t.Fatal("unfinished syntax became proved format", got, oracle)
				}
				return
			}
			if err != nil || got != oracle.Format {
				t.Fatal(got, err, oracle)
			}
		})
	}
}
