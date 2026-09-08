package checked

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestCJSStartupDescriptorSource(t *testing.T) {
	cases := []struct {
		Name, Source, Observe string
		Complete              bool
	}{
		{"marker", `Object.defineProperty(exports,"__esModule",{value:true});exports.call=call;function call(){return "body"}`, `({keys:Object.keys(ns.default),marker:Object.getOwnPropertyDescriptor(ns.default,'__esModule'),result:ns.call()})`, true},
		{"hidden-callable", `Object.defineProperty(exports,"call",{value:function hidden(){return "hidden"}});`, `({keys:Object.keys(ns.default),result:ns.call(),same:ns.call===ns.default.call})`, true},
		{"target-alias", `const first=exports;Object.defineProperty(exports,"call",{value:(exports={call:function detached(){return "detached"}}).call});`, `({result:ns.default.call(),keys:Object.keys(ns.default)})`, true},
		{"sloppy-write", `Object.defineProperty(exports,"x",{value:1});exports.x=2;`, `ns.default.x`, true},
		{"strict-write", `"use strict";Object.defineProperty(exports,"x",{value:1});exports.x=2;`, `null`, false},
		{"shadow", `const Object={defineProperty:function(target,key,value){target[key]=value.value}};Object.defineProperty(exports,"x",{value:1});`, `ns.default.x`, false},
		{"redefine", `Object.defineProperty(exports,"x",{value:1});Object.defineProperty(exports,"x",{value:2});`, `null`, false},
		{"accessor", `Object.defineProperty(exports,"x",{get:function(){return 1}});`, `ns.default.x`, false},
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
			state, err := analyzeCJSExportState(plan, c, CJSExportBoundary{PristineObjectPrototype: true, IntrinsicDefineProperty: true, DescriptorPrototypeClean: true})
			if err != nil {
				t.Fatal(err)
			}
			if state.Complete != test.Complete {
				t.Fatalf("complete=%v obligations=%+v", state.Complete, state.Obligations)
			}
			if len(state.PendingOperations()) != len(plan.Initialization) || len(state.Steps) != len(plan.Initialization) {
				t.Fatal("lost original operations")
			}
			if test.Name == "marker" {
				attrs := state.Objects[state.RecordExport.Object].Descriptors["__esModule"]
				if attrs != (CJSDataAttributes{}) {
					t.Fatal(attrs)
				}
				ref, ok := state.CallableReference("call")
				if !ok || !ref.NeedsBodyProof || !ref.NeedsInstanceBinding || ref.ModuleEnvironment != plan.Module || !strings.Contains(test.Source[ref.Template.Body.Start:ref.Template.Body.End], `return "body"`) {
					t.Fatal(ref, ok)
				}
			}
			count := 0
			for _, effect := range state.Effects {
				if effect.Kind == "define-own-data-property" {
					count++
				}
			}
			if test.Complete && count != 1 {
				t.Fatal("descriptor execution omitted", state.Effects)
			}
			// Missing incoming descriptor prototype proof cannot borrow the positive branch's result.
			negative, _ := analyzeCJSExportState(plan, c, CJSExportBoundary{PristineObjectPrototype: true, IntrinsicDefineProperty: true})
			if negative.Complete {
				t.Fatal("missing descriptor premise admitted")
			}
			node := packageNode(t, dir, `try{const ns=await import('./subject.cjs');console.log(JSON.stringify({value:`+test.Observe+`}))}catch(e){console.log(JSON.stringify({error:e.name}))}`)
			if test.Complete {
				assertCJSOwnDescriptorsNode(t, dir, state)
			}
			reports[test.Name] = map[string]any{"state": state, "node": json.RawMessage(node), "source": test.Source}
		})
	}
	data, _ := json.MarshalIndent(reports, "", "  ")
	if err := os.WriteFile(filepath.Join(sourcefixture.Get(t, "package-output"), "source-state-report.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestCJSStartupActualPackages(t *testing.T) {
	portfolio := sourcefixture.Get(t, "portfolio")
	project, err := captureDependencyProject(filepath.Join(portfolio, "tsconfig.json"), "cookie-route/route.ts", ProjectOptions{DependencyTypesInferJS})
	if err != nil {
		t.Fatal(err)
	}
	if len(project.Diagnostics) > 0 {
		t.Fatal(project.Diagnostics)
	}
	if len(project.Config.FileNames()) != 10 {
		t.Fatal("configured roots changed", len(project.Config.FileNames()))
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
		state, err := analyzeCJSExportState(plan, c, CJSExportBoundary{PristineObjectPrototype: true, IntrinsicDefineProperty: true, DescriptorPrototypeClean: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(state.PendingOperations()) != len(plan.Initialization) {
			t.Fatal("initializer dropped")
		}
		if strings.HasSuffix(module, "/cookie/dist/index.js") {
			if len(plan.Initialization) != 16 || state.Complete {
				t.Fatalf("cookie source state %+v", state)
			}
			// Function binding instantiation precedes every evaluated initializer.
			hoisted := len(plan.HoistedFunctions)
			if hoisted != 6 || len(state.Effects) < hoisted {
				t.Fatal("original hoisted declarations missing")
			}
			for i, declaration := range plan.HoistedFunctions {
				effect := state.Effects[i]
				if effect.Kind != "instantiate-binding-cell" || effect.Value.Function == nil || *effect.Value.Function != declaration.Body || !effect.NeedsNativeExecution {
					t.Fatal("source hoisting order/identity changed", effect)
				}
			}
			if err := cookiePrefixEffects(plan, state); err != nil {
				t.Fatal(err)
			}
			// Same-kind reorderings must fail this same assertion, not merely a
			// separate comparison between Node and a hand-authored constant.
			for _, pair := range [][2]int{{hoisted + 1, hoisted + 2}, {hoisted + 3, hoisted + 5}, {hoisted + 4, hoisted + 6}} {
				state.Effects[pair[0]], state.Effects[pair[1]] = state.Effects[pair[1]], state.Effects[pair[0]]
				err := cookiePrefixEffects(plan, state)
				state.Effects[pair[0]], state.Effects[pair[1]] = state.Effects[pair[1]], state.Effects[pair[0]]
				if err == nil {
					t.Fatal("same-kind source-effect swap escaped assertion", pair)
				}
			}
			for _, mutate := range []func(*CJSStartupEffect){
				func(e *CJSStartupEffect) { e.Source.Start++ },
				func(e *CJSStartupEffect) { e.Source.SourceSHA256 = "changed" },
				func(e *CJSStartupEffect) { e.Key = "serialize" },
				func(e *CJSStartupEffect) { e.Value = state.Effects[hoisted+2].Value },
				func(e *CJSStartupEffect) { e.Cell = state.Effects[hoisted+4].Cell },
			} {
				saved := state.Effects[hoisted+1]
				mutate(&state.Effects[hoisted+1])
				err := cookiePrefixEffects(plan, state)
				state.Effects[hoisted+1] = saved
				if err == nil {
					t.Fatal("source effect identity/value mutation escaped assertion")
				}
			}
			marker := state.Effects[hoisted]
			if marker.Key != "__esModule" || marker.Attributes != (CJSDataAttributes{}) || strings.TrimSpace(plan.File.Text()[marker.Source.Start:marker.Source.End]) != `Object.defineProperty(exports, "__esModule", { value: true })` {
				t.Fatal("actual descriptor source/flags changed", marker)
			}
			node := packageNode(t, portfolio, `const ns=await import('cookie');console.log(JSON.stringify({keys:Object.keys(ns.default),marker:Object.getOwnPropertyDescriptor(ns.default,'__esModule'),parse:ns.parse===ns.default.parse,serialize:ns.serialize===ns.default.serialize}));`)
			if node != `{"keys":["parse","serialize"],"marker":{"value":true,"writable":false,"enumerable":false,"configurable":false},"parse":true,"serialize":true}` {
				t.Fatal("actual Node startup descriptor/export identity", node)
			}

			if _, ok := state.CallableReference("parse"); ok {
				t.Fatal("prefix callable escaped unresolved startup")
			}
			if len(state.Obligations) == 0 {
				t.Fatal(state.Obligations)
			}
		}
		if strings.HasSuffix(module, "/escape-html/index.js") {
			if len(plan.Initialization) != 4 || !state.Complete {
				t.Fatal(state)
			}
			ref, ok := state.CallableReference("default")
			if !ok || ref.Template.Name != "escapeHtml" {
				t.Fatal(ref)
			}
			if !strings.Contains(plan.File.Text(), "var matchHtmlRegExp =") {
				t.Fatal("original regex absent")
			}
		}
		// Every source statement and function body remains linked by immutable hash+span.
		reports[module] = map[string]any{"state": state, "plan": plan}
	}
	data, _ := json.MarshalIndent(reports, "", "  ")
	if err := os.WriteFile(filepath.Join(sourcefixture.Get(t, "package-output"), "actual-package-report.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

// Compare the finite actual export-property facts against Node's descriptors;
// original call bodies execute only in Node here, not in the source analyzer.
func assertCJSOwnDescriptorsNode(t *testing.T, dir string, state *CJSExportState) {
	t.Helper()
	if state.RecordExport.Kind != "object" {
		t.Fatal("fixture expects exported object")
	}
	props := map[string]any{}
	for key, value := range state.Objects[state.RecordExport.Object].Own {
		var normalized any
		switch value.Kind {
		case "literal":
			raw := strings.TrimSpace(state.Plan.File.Text()[value.Literal.Start:value.Literal.End])
			if err := json.Unmarshal([]byte(raw), &normalized); err != nil {
				t.Fatal(raw, err)
			}
		case "function":
			name := ""
			for _, f := range state.Plan.Functions {
				if f.Body == *value.Function {
					name = f.Name
				}
			}
			normalized = map[string]any{"function": name}
		case "undefined":
			normalized = map[string]any{"undefined": true}
		default:
			t.Fatal("fixture unmodeled data", value)
		}
		attrs, exists := state.Objects[state.RecordExport.Object].Descriptors[key]
		if !exists {
			attrs = CJSDataAttributes{true, true, true}
		}
		props[key] = map[string]any{"value": normalized, "writable": attrs.Writable, "enumerable": attrs.Enumerable, "configurable": attrs.Configurable}
	}
	text := packageNode(t, dir, `const ns=await import('./subject.cjs');console.log(JSON.stringify(Object.fromEntries(Object.entries(Object.getOwnPropertyDescriptors(ns.default)).map(([k,d])=>[k,{...d,value:typeof d.value==='function'?{function:d.value.name}:d.value===undefined?{undefined:true}:d.value}]))));`)
	var node map[string]any
	if err := json.Unmarshal([]byte(text), &node); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(props, node) {
		t.Fatalf("actual source property facts %v != Node %v", props, node)
	}
}
