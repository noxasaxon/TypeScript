package checked

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/internal/vfs/osvfs"
)

func loaderRepairProject(t *testing.T, source string) string {
	t.Helper()
	dir := packagePrototypeDir(t)
	packageWrite(t, filepath.Join(dir, "package.json"), `{"type":"module"}`)
	packageWrite(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"strict":true,"module":"NodeNext","allowImportingTsExtensions":true,"noEmit":true},"files":["entry.ts"]}`)
	packageWrite(t, filepath.Join(dir, "entry.ts"), source)
	return dir
}

func TestLoaderRepairRequireBinding(t *testing.T) {
	for name, source := range map[string]string{
		"exact-function": "function require(value: number): number { return value + 1; }\nconsole.log(require(41));\nexport {};\n",
		"parameter":      `function call(require:(n:number)=>number):number{return require(41);}function next(n:number):number{return n+1;}console.log(call(next));export {};`,
		"block":          `{const require=(n:number):number=>n+1;console.log(require(41));}export {};`,
		"parenthesized":  `function require(n:number):number{return n+1;}console.log((require)(41));export {};`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := loaderRepairProject(t, source)
			if got := packageNode(t, dir, `await import("./entry.ts");`); got != "42" {
				t.Fatal(got)
			}
			for _, policy := range []DependencyTypes{DependencyTypesConfig, DependencyTypesInferJS} {
				p, ds := ReadProjectWithOptions(filepath.Join(dir, "tsconfig.json"), "entry.ts", ProjectOptions{policy})
				if p == nil || len(ds) != 0 {
					t.Fatal(policy, ds)
				}
				if _, ds := p.checkDependencyProject(); len(ds) != 0 {
					t.Fatal(ds)
				}
			}
		})
	}
}

func TestLoaderRepairTrueLoaderBoundary(t *testing.T) {
	for name, source := range map[string]string{
		"ambient":       "declare function require(name:string):unknown;\nrequire('pkg');export {};",
		"unbound":       "// @ts-ignore actual unbound require\nrequire('pkg');export {};",
		"parenthesized": "declare function require(name:string):unknown;\n(require)('pkg');export {};",
		"dynamic":       "export {};\n// @ts-ignore target intentionally unresolved\nimport('pkg');",
	} {
		t.Run(name, func(t *testing.T) {
			dir := loaderRepairProject(t, source)
			for _, policy := range []DependencyTypes{DependencyTypesConfig, DependencyTypesInferJS} {
				_, ds := ReadProjectWithOptions(filepath.Join(dir, "tsconfig.json"), "entry.ts", ProjectOptions{policy})
				if len(ds) != 1 || ds[0].Construct != "SourceRuntimeDependency" || ds[0].SourcePath != filepath.Join(dir, "entry.ts") || ds[0].Position.Line < 2 {
					t.Fatal(policy, ds)
				}
			}
		})
	}
}

func TestLoaderRepairConfigSymlink(t *testing.T) {
	for _, policy := range []DependencyTypes{DependencyTypesConfig, DependencyTypesInferJS} {
		dir := loaderRepairProject(t, "console.log(42);\nexport {};\n")
		// Cross-directory entry proves config-relative roots use the canonical target.
		requested := filepath.Join(dir, "links", "linked.json")
		if err := os.MkdirAll(filepath.Dir(requested), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../tsconfig.json", requested); err != nil {
			t.Fatal(err)
		}
		p, ds := ReadProjectWithOptions(requested, "entry.ts", ProjectOptions{policy})
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		if p.ConfigPath != filepath.Join(dir, "tsconfig.json") {
			t.Fatal(p.ConfigPath)
		}
		if _, ds := p.checkDependencyProject(); len(ds) != 0 {
			t.Fatal(ds)
		}
		snapshot := p.dependency.Snapshot
		if _, ok := snapshot.Observations["realpath\x00"+requested]; !ok {
			t.Fatal("requested link absent from snapshot")
		}
		packageWrite(t, filepath.Join(dir, "other.json"), `{"compilerOptions":{"strict":true,"module":"NodeNext","allowImportingTsExtensions":true,"noEmit":true},"files":["entry.ts"]}`)
		if err := os.Remove(requested); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../other.json", requested); err != nil {
			t.Fatal(err)
		}
		if snapshot.Revalidate(osvfs.FS()) == nil {
			t.Fatal("retargeted config accepted")
		}
		replay := replayPackages(snapshot)
		if got := replay.Realpath(requested); got != filepath.Join(dir, "tsconfig.json") {
			t.Fatal("replay consulted retargeted link", got)
		}
		if replay.Fault() != nil {
			t.Fatal(replay.Fault())
		}
	}
}

func TestLoaderRepairStructuredDiagnostics(t *testing.T) {
	for _, policy := range []DependencyTypes{DependencyTypesConfig, DependencyTypesInferJS} {
		dir := loaderRepairProject(t, "export {};\n\nimport { value } from \"./dep.ts\";\nconsole.log(value);\n")
		packageWrite(t, filepath.Join(dir, "dep.d.ts"), "export declare const value: number;\n")
		_, ds := ReadProjectWithOptions(filepath.Join(dir, "tsconfig.json"), "entry.ts", ProjectOptions{policy})
		if len(ds) != 1 || ds[0].Construct != "PackageRuntimeResolution" || ds[0].SourcePath != filepath.Join(dir, "entry.ts") || ds[0].Position.Line != 3 || ds[0].Position.Column != 23 || !strings.Contains(ds[0].Message, "missing exact runtime target") {
			t.Fatal(ds)
		}
		_, err := captureDependencyProject(filepath.Join(dir, "tsconfig.json"), "entry.ts", ProjectOptions{policy})
		var positioned *projectDiagnosticError
		if !errors.As(err, &positioned) || positioned.Diagnostic != ds[0] {
			t.Fatal("typed diagnostic changed through API", err, ds)
		}
	}
}

func TestLoaderRepairDependencySymlinkBoundary(t *testing.T) {
	dir := loaderRepairProject(t, `import{value}from"./dep.ts";console.log(value);`)
	packageWrite(t, filepath.Join(dir, "real.ts"), `export const value=42;`)
	if err := os.Symlink("real.ts", filepath.Join(dir, "dep.ts")); err != nil {
		t.Fatal(err)
	}
	for _, policy := range []DependencyTypes{DependencyTypesConfig, DependencyTypesInferJS} {
		_, ds := ReadProjectWithOptions(filepath.Join(dir, "tsconfig.json"), "entry.ts", ProjectOptions{policy})
		if len(ds) != 1 || !strings.Contains(ds[0].Message, "runtime symlink/case identity") {
			t.Fatal(ds)
		}
	}
}

func TestLoaderRepairRuntimeJSBinding(t *testing.T) {
	for name, body := range map[string]string{
		"local":        `function require(n){return n+1;}console.log(require(41));`,
		"node-wrapper": `require("unimplemented-runtime-target");`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := loaderRepairProject(t, `import "./dep.cjs";export {};`)
			packageWrite(t, filepath.Join(dir, "dep.cjs"), body)
			p, ds := ReadProjectWithOptions(filepath.Join(dir, "tsconfig.json"), "entry.ts", ProjectOptions{DependencyTypesInferJS})
			if name == "node-wrapper" {
				if len(ds) != 1 || ds[0].Construct != "SourceRuntimeDependency" || ds[0].SourcePath != filepath.Join(dir, "dep.cjs") {
					t.Fatal(ds)
				}
				return
			}
			if len(ds) != 0 {
				t.Fatal(ds)
			}
			if p.dependency.Program.GetSourceFile(filepath.Join(dir, "dep.cjs")) == nil {
				t.Fatal("actual JS root absent")
			}
			if got := packageNode(t, dir, `await import("./entry.ts");`); got != "42" {
				t.Fatal(got)
			}
		})
	}
}

func TestLoaderRepairExactConfigLink(t *testing.T) {
	for _, policy := range []DependencyTypes{DependencyTypesConfig, DependencyTypesInferJS} {
		dir := loaderRepairProject(t, "console.log(42);\nexport {};\n")
		link := filepath.Join(dir, "linked.json")
		if err := os.Symlink("tsconfig.json", link); err != nil {
			t.Fatal(err)
		}
		legacy, ds := ReadProject(link, "entry.ts")
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		p, ds := ReadProjectWithOptions(link, "entry.ts", ProjectOptions{policy})
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		if p.ConfigPath != legacy.ConfigPath || p.Entry != legacy.Entry {
			t.Fatal("canonical identity changed")
		}
		if _, ds = p.Check(); len(ds) != 0 {
			t.Fatal(ds)
		}
	}
}

func TestLoaderRepairDeclarationOnlyRequire(t *testing.T) {
	for _, ext := range []string{".d.ts", ".d.mts"} {
		dir := loaderRepairProject(t, `import {require} from "./loader`+ext+`";
require("pkg");`)
		packageWrite(t, filepath.Join(dir, "loader"+ext), `export const require: (name:string)=>unknown;`)
		for _, policy := range []DependencyTypes{DependencyTypesConfig, DependencyTypesInferJS} {
			if _, ds := ReadProjectWithOptions(filepath.Join(dir, "tsconfig.json"), "entry.ts", ProjectOptions{policy}); len(ds) == 0 {
				t.Fatal("declaration became implementation")
			}
		}
	}
	dir := loaderRepairProject(t, "/// <reference path=\"./globals.d.ts\" />\nrequire('pkg');export {};\n")
	packageWrite(t, filepath.Join(dir, "globals.d.ts"), `declare function require(name:string):unknown;`)
	for _, policy := range []DependencyTypes{DependencyTypesConfig, DependencyTypesInferJS} {
		_, ds := ReadProjectWithOptions(filepath.Join(dir, "tsconfig.json"), "entry.ts", ProjectOptions{policy})
		if len(ds) != 1 || ds[0].Construct != "SourceRuntimeDependency" || ds[0].Position.Line != 2 {
			t.Fatal(ds)
		}
	}
}
