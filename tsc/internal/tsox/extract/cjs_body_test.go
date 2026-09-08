package extract

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/internal/tsox/checked"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func cjsBodyRealm() checked.CJSExportBoundary {
	return checked.CJSExportBoundary{PristineObjectPrototype: true, IntrinsicDefineProperty: true, DescriptorPrototypeClean: true, IntrinsicObjectCreate: true, IntrinsicObjectPrototypeToString: true}
}
func cjsBodyRoot(t *testing.T) string {
	t.Helper()
	root := sourcefixture.Get(t, "cjs-body-output")
	if root == "" {
		t.Fatal("explicit scratch output required")
	}
	return root
}
func cjsBodyWrite(t *testing.T, path, text string) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(path, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
}
func cjsBodyRecord(t *testing.T, name string, value any) {
	t.Helper()
	data, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	cjsBodyWrite(t, filepath.Join(cjsBodyRoot(t), name), string(data))
}
func cjsBodyNode(t *testing.T, dir, observe, expected string) string {
	t.Helper()
	oracle := `import assert from 'node:assert/strict';assert.equal(process.versions.node,'24.20.0');const ns=await import('./subject.cjs');console.log(JSON.stringify(` + observe + `));`
	cjsBodyWrite(t, filepath.Join(dir, "oracle.mjs"), oracle)
	cmd := exec.Command("mise", "exec", "--", "node", filepath.Join(dir, "oracle.mjs"))
	cmd.Dir = dir
	raw, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatal(e, string(raw))
	}
	if strings.TrimSpace(string(raw)) != expected {
		t.Fatal(string(raw), expected)
	}
	return string(raw)
}
func TestCJSOrdinaryBodyControls(t *testing.T) {
	cases := []struct {
		Name, Source, Observe, Expected string
		Wrappers                        int
		Graph                           bool
	}{
		{"shared-cell", `let n=1;exports.call=read;exports.other=write;function read(){return n}function write(){n+=1;return n}`, `[ns.call(),ns.other(),ns.call()]`, `[1,2,2]`, 0, true},
		{"distinct-factory-cells", `const make=()=>{let n=1;return function read(){return n}};exports.call=make();exports.other=make()`, `[ns.call===ns.other,ns.call(),ns.other()]`, `[false,1,1]`, 0, false},
		{"wrapper-var", `var module;exports.call=read;function read(){return typeof module}`, `ns.call()`, `"object"`, 0, false},
		{"wrapper-function", `function module(){return 3}exports.call=read;function read(){return module()}`, `ns.call()`, `3`, 0, false},
		{"module-cell", `let n=1;exports.call=read;function read(){return n};n=2;`, `ns.call()`, `2`, 0, true},
		{"captured-write", `let n=1;exports.call=read;function read(){n+=1;return n}`, `[ns.call(),ns.call()]`, `[2,3]`, 0, true},
		{"exports-read", `exports.value=1;function read(){return exports.value};exports.call=read;exports.value=2;`, `ns.call()`, `2`, 1, false},
		{"detached-exports", `exports.old=1;module.exports={call:read};exports.old=3;function read(){return exports.old}`, `ns.default.call()`, `3`, 1, false},
		{"module-rebind", `module.exports={call:read};module={exports:{value:4}};function read(){return module.exports.value}`, `ns.default.call()`, `4`, 1, false},
		{"wrapper-three", `exports.call=read;function read(){return [typeof require,typeof __filename,typeof __dirname]}`, `ns.call()`, `["function","string","string"]`, 3, false},
		{"nested-var-local", `exports.call=read;function read(){var require=7;return require}`, `ns.call()`, `7`, 0, false},
		{"shadow-parameter", `exports.call=read;/** @param {number} module */function read(module){return module+1}`, `ns.call(2)`, `3`, 0, false},
		{"property-key", `exports.call=read;function read(){return {module:1}}`, `ns.call().module`, `1`, 0, true},
		{"shorthand", `exports.call=read;function read(){return {exports}}`, `ns.call().exports===ns.default`, `true`, 1, false},
		{"strict-this", `"use strict";exports.call=read;function read(){return this===undefined}`, `Reflect.apply(ns.call,undefined,[])`, `true`, 0, false},
		{"sloppy-this", `exports.call=read;function read(){return this===undefined}`, `Reflect.apply(ns.call,undefined,[])`, `false`, 0, false},
	}
	reports := map[string]any{}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			dir := filepath.Join(cjsBodyRoot(t), "controls", test.Name)
			cjsBodyWrite(t, filepath.Join(dir, "package.json"), `{"type":"module"}`)
			cjsBodyWrite(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"strict":true,"module":"NodeNext","allowImportingTsExtensions":true,"noEmit":true},"files":["entry.ts"]}`)
			cjsBodyWrite(t, filepath.Join(dir, "entry.ts"), `import * as ns from './subject.cjs';export {ns};`)
			cjsBodyWrite(t, filepath.Join(dir, "subject.cjs"), test.Source)
			p, states, roots, e := checked.ReadCJSBodySourceProject(filepath.Join(dir, "tsconfig.json"), "entry.ts", cjsBodyRealm())
			if e != nil {
				t.Fatal(e)
			}
			if roots != 1 {
				t.Fatal(roots)
			}
			state := states[filepath.Join(dir, "subject.cjs")]
			if !state.Complete {
				if test.Name != "wrapper-var" {
					t.Fatal("unexpected source startup boundary", state.Obligations)
				}
				if _, err := checked.CJSBodyInput(p, state, "call"); err == nil {
					t.Fatal("incomplete wrapper startup certified")
				}
				reports[test.Name] = map[string]any{"source": test.Source, "node": cjsBodyNode(t, dir, test.Observe, test.Expected), "startup": state, "body": nil}
				return
			}
			input, e := checked.CJSBodyInput(p, state, "call")
			if e != nil {
				t.Fatal(e)
			}
			if len(input.Wrappers) != test.Wrappers {
				t.Fatalf("wrappers=%+v", input.Wrappers)
			}
			body := sourceCJSOrdinaryBody(input)
			if (len(body.Source.Diagnostics) == 0) != test.Graph {
				t.Fatalf("graph=%v diagnostics=%+v", test.Graph, body.Source.Diagnostics)
			}
			if len(body.WrapperUses) != test.Wrappers {
				t.Fatal("wrapper graph linkage omitted")
			}
			for _, use := range body.WrapperUses {
				cell, ok := body.WrapperCells[use.Binding]
				if !ok || !cell.NeedsRuntimeBinding || cell.Module != state.Plan.Module {
					t.Fatal("wrapper cell value copied/identity lost")
				}
			}
			if test.Name == "shared-cell" || test.Name == "distinct-factory-cells" {
				other, e := checked.CJSBodyInput(p, state, "other")
				if e != nil {
					t.Fatal(e)
				}
				find := func(x *checked.CJSCallableBodyInput) checked.CJSBodyCell {
					for _, cell := range x.Cells {
						if cell.Symbol.Name == "n" {
							return cell
						}
					}
					t.Fatal("missing n cell")
					return checked.CJSBodyCell{}
				}
				a, z := find(input), find(other)
				same := a.Module == z.Module && a.Environment == z.Environment && a.Cell == z.Cell
				if same != (test.Name == "shared-cell") {
					t.Fatal("captured environment identity mismatch", a, z)
				}
			}
			if len(body.Syntax) == 0 || !body.NeedsInvocationProof {
				t.Fatal("missing source or invocation boundary")
			}
			for _, cell := range body.Captures {
				if !cell.NeedsRuntimeBinding || cell.Symbol == nil {
					t.Fatal("capture flattened")
				}
			}
			raw := cjsBodyNode(t, dir, test.Observe, test.Expected)
			// Byte equality is not immutable Program identity.
			p2, _, _, e := checked.ReadCJSBodySourceProject(filepath.Join(dir, "tsconfig.json"), "entry.ts", cjsBodyRealm())
			if e != nil {
				t.Fatal(e)
			}
			if _, e = checked.CJSBodyInput(p2, state, "call"); e == nil {
				t.Fatal("foreign Program accepted")
			}
			reports[test.Name] = map[string]any{"source": test.Source, "node": string(raw), "body": body}
		})
	}
	cjsBodyRecord(t, "controls-report.json", reports)
}
func TestCJSOrdinaryBodyActualPackages(t *testing.T) {
	portfolio := sourcefixture.Get(t, "portfolio")
	if portfolio == "" {
		t.Fatal("actual portfolio required")
	}
	p, states, roots, e := checked.ReadCJSBodySourceProject(filepath.Join(portfolio, "tsconfig.json"), "cookie-route/route.ts", cjsBodyRealm())
	if e != nil {
		t.Fatal(e)
	}
	if roots != 10 {
		t.Fatal(roots)
	}
	reports := map[string]any{}
	for module, state := range states {
		names := []string{"default"}
		if strings.HasSuffix(module, "/cookie/dist/index.js") {
			names = []string{"parse", "serialize"}
			if len(state.Plan.Initialization) != 16 {
				t.Fatal("changed cookie initialization")
			}
		} else if len(state.Plan.Initialization) != 4 {
			t.Fatal("changed escape initialization")
		}
		for _, name := range names {
			input, e := checked.CJSBodyInput(p, state, name)
			if e != nil {
				t.Fatal(e)
			}
			body := sourceCJSOrdinaryBody(input)
			if len(input.Wrappers) != 0 || !input.Strict || len(body.Syntax) == 0 || len(body.Source.Diagnostics) == 0 {
				t.Fatal("actual pending source body misclassified", name, body.Source.Diagnostics)
			}
			t.Log(name, body.CheckedTypes, body.Source.Diagnostics)
			reports[module+"#"+name] = body
		}
	}
	if len(reports) != 3 {
		t.Fatal("actual exports omitted", len(reports))
	}
	cjsBodyRecord(t, "actual-package-bodies.json", reports)
}
