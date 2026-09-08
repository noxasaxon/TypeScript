package checked

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func startupRealm() CJSExportBoundary {
	return CJSExportBoundary{PristineObjectPrototype: true, IntrinsicDefineProperty: true, DescriptorPrototypeClean: true, IntrinsicObjectCreate: true, IntrinsicObjectPrototypeToString: true}
}
func TestCJSStartupInstancesNode(t *testing.T) {
	cases := []struct {
		Name, Source, Observe, Expected string
		Complete                        bool
	}{
		{"constructor-iife", `const C=(()=>{const C=function(){};C.prototype=Object.create(null);return C})();module.exports=C;`, `[typeof ns.default,Object.getPrototypeOf(ns.default.prototype)===null,Object.keys(ns.default.prototype)]`, `["function",true,[]]`, true},
		{"captured-cell-write", `const make=()=>{let value="before";const read=()=>value;value="after";return read};exports.call=make();exports.result=exports.call();`, `ns.default.result`, `"after"`, true},
		{"separate-instances", `const make=()=>{let value="owned";return ()=>value};exports.a=make();exports.b=make();`, `[ns.a===ns.b,ns.a(),ns.b()]`, `[false,"owned","owned"]`, true},
		{"module-cell-write", `let value="before";exports.call=()=>value;value="after";exports.result=exports.call();`, `ns.default.result`, `"after"`, true},
		{"return-keeps-tail", `module.exports=(()=>{return function actual(){return "returned"};throw new Error("unreachable")})();`, `ns.default()`, `"returned"`, true},
		{"shadowed-create", `const Object={create:()=>({wrong:true})};const C=(()=>{const C=function(){};C.prototype=Object.create(null);return C})();module.exports=C;`, `ns.default.prototype.wrong`, `true`, false},
		{"argument-obligation", `module.exports=((x)=>()=>x)("argument");`, `ns.default()`, `"argument"`, false},

		{"strict-iife", `Object.defineProperty(exports,"x",{value:1});(()=>{"use strict";exports.x=2})();`, `ns.default.x`, `"TypeError"`, false},
		{"sloppy-called-from-strict", `function sloppy(){exports.x=2}const strict=()=>{"use strict";sloppy()};Object.defineProperty(exports,"x",{value:1});strict();`, `ns.default.x`, `1`, true},
		{"pure-comment-effects", `module.exports=/* @__PURE__ */(()=>{console.log("init");return ()=>"result"})();`, `ns.default()`, "init\n\"result\"", false},
		{"capture-intrinsic", `const toString=Object.prototype.toString;exports.call=()=>toString;`, `ns.call()===Object.prototype.toString`, `true`, true},
	}
	reports := map[string]any{}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			dir := packagePrototypeDir(t)
			packageWrite(t, filepath.Join(dir, "package.json"), `{"type":"module"}`)
			packageWrite(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"strict":true,"module":"NodeNext","allowImportingTsExtensions":true,"noEmit":true},"files":["entry.ts"]}`)
			packageWrite(t, filepath.Join(dir, "entry.ts"), `import * as ns from './subject.cjs';export {ns};`)
			packageWrite(t, filepath.Join(dir, "subject.cjs"), test.Source)
			project, err := captureDependencyProject(filepath.Join(dir, "tsconfig.json"), "entry.ts", ProjectOptions{DependencyTypesInferJS})
			if err != nil {
				t.Fatal(err)
			}
			if len(project.Diagnostics) > 0 {
				t.Fatal(project.Diagnostics)
			}
			c, done := project.Program.GetTypeChecker(context.Background())
			defer done()
			plans, err := planRuntimeSources(project, c)
			if err != nil {
				t.Fatal(err)
			}
			plan := plans.Modules[filepath.Join(dir, "subject.cjs")]
			state, err := analyzeCJSExportState(plan, c, startupRealm())
			if err != nil {
				t.Fatal(err)
			}
			if state.Complete != test.Complete {
				t.Fatalf("complete=%v obligations=%+v", state.Complete, state.Obligations)
			}
			if len(state.Steps) != len(plan.Initialization) {
				t.Fatal("source initializer omitted")
			}
			node := packageNode(t, dir, `try{const ns=await import('./subject.cjs');console.log(JSON.stringify(`+test.Observe+`));}catch(error){console.log(JSON.stringify(error.name))}`)
			if node != test.Expected {
				t.Fatal(node, test.Expected)
			}
			if test.Name == "captured-cell-write" || test.Name == "module-cell-write" {
				value, ok := state.FinalOwnExport("result")
				if !ok || value.Literal == nil || strings.TrimSpace(test.Source[value.Literal.Start:value.Literal.End]) != `"after"` {
					t.Fatal(value, ok)
				}
				ref, ok := state.CallableReference("call")
				if !ok || !ref.NeedsBodyProof {
					t.Fatal(ref)
				}
			}
			if test.Name == "separate-instances" {
				a, _ := state.CallableReference("a")
				b, _ := state.CallableReference("b")
				if a.Instance == b.Instance || a.Environment == b.Environment || a.Template.Body != b.Template.Body {
					t.Fatal(a, b)
				}
				ca := state.Environments[a.Environment].Cells
				cb := state.Environments[b.Environment].Cells
				if len(ca) != 1 || len(cb) != 1 || ca[0] == cb[0] {
					t.Fatal(ca, cb)
				}
			}
			if test.Name == "constructor-iife" {
				ref, ok := state.CallableReference("default")
				if !ok {
					t.Fatal(state)
				}
				instance := state.Instances[ref.Instance]
				prototype := state.Objects[instance.Object].Own["prototype"]
				if state.Objects[prototype.Object].Prototype != "null" {
					t.Fatal(prototype)
				}
			}
			if test.Name == "return-keeps-tail" {
				if len(state.Invocations) != 1 || len(state.Invocations[0].Body) != 2 || len(state.Invocations[0].Executed) != 1 {
					t.Fatal(state.Invocations)
				}
			}
			reports[test.Name] = map[string]any{"source": test.Source, "node": node, "state": state}
		})
	}
	data, _ := json.MarshalIndent(reports, "", "  ")
	if err := os.WriteFile(filepath.Join(sourcefixture.Get(t, "package-output"), "instance-node-report.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestCJSStartupInstancesActualPackages(t *testing.T) {
	portfolio := sourcefixture.Get(t, "portfolio")
	project, err := captureDependencyProject(filepath.Join(portfolio, "tsconfig.json"), "cookie-route/route.ts", ProjectOptions{DependencyTypesInferJS})
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Diagnostics) > 0 || len(project.Config.FileNames()) != 10 {
		t.Fatal(project.Diagnostics)
	}
	c, done := project.Program.GetTypeChecker(context.Background())
	defer done()
	plans, err := planRuntimeSources(project, c)
	if err != nil {
		t.Fatal(err)
	}
	reports := map[string]any{}
	for module, plan := range plans.Modules {
		if plan.Format != "commonjs" {
			continue
		}
		state, err := analyzeCJSExportState(plan, c, startupRealm())
		if err != nil {
			t.Fatal(err)
		}
		if !state.Complete {
			t.Fatalf("%s: %+v", module, state.Obligations)
		}
		if strings.HasSuffix(module, "/cookie/dist/index.js") {
			if len(plan.Initialization) != 16 || len(state.Invocations) != 1 {
				t.Fatal("cookie source plan changed")
			}
			for _, name := range []string{"parse", "serialize"} {
				ref, ok := state.CallableReference(name)
				if !ok || ref.Template.Name != name || ref.Environment != 0 || !ref.NeedsBodyProof {
					t.Fatal(ref, ok)
				}
			}
			marker := state.Objects[state.RecordExport.Object].Descriptors["__esModule"]
			if marker != (CJSDataAttributes{}) {
				t.Fatal(marker)
			}
		}
		if strings.HasSuffix(module, "/escape-html/index.js") {
			if len(plan.Initialization) != 4 {
				t.Fatal("escape source changed")
			}
			ref, ok := state.CallableReference("default")
			if !ok || ref.Template.Name != "escapeHtml" || ref.Environment != 0 {
				t.Fatal(ref)
			}
		}
		reports[module] = map[string]any{"state": state, "plan": plan}
	}
	data, _ := json.MarshalIndent(reports, "", "  ")
	if err := os.WriteFile(filepath.Join(sourcefixture.Get(t, "package-output"), "instance-package-report.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
