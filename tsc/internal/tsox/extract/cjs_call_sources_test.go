package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func cjsDomainSnapshot(t *testing.T, config, entry string) *checked.CJSBodySourceSnapshot {
	t.Helper()
	p, e := checked.ReadCJSBodySourceSnapshot(config, entry, cjsBodyRealm())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func cjsDomainReport(t *testing.T, name string, value any) {
	t.Helper()
	root := sourcefixture.Get(t, "cjs-domain-output")
	if root == "" {
		t.Fatal("explicit output")
	}
	data, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, name), data, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestCJSActualCallSourceObligations(t *testing.T) {
	portfolio := sourcefixture.Get(t, "portfolio")
	if portfolio == "" {
		t.Fatal("explicit portfolio")
	}
	p := cjsDomainSnapshot(t, filepath.Join(portfolio, "tsconfig.json"), "cookie-route/route.ts")
	if p.ConfiguredRoots != 10 {
		t.Fatal("configured root lost")
	}
	calls, e := checked.CJSCallSources(p)
	if e != nil {
		t.Fatal(e)
	}
	if len(calls) != 3 {
		t.Fatal("original call omitted", len(calls))
	}
	seen := map[string]bool{}
	for _, call := range calls {
		name := call.Import.Requested
		seen[name] = true
		if call.EvaluatedCertificate || len(call.Pending) == 0 || call.EnclosingFunction == nil || call.ThisArgument != "undefined-direct-import" {
			t.Fatal("linkage falsely certified")
		}
		if call.CalleeFirst.Start >= call.Arguments[0].Source.Start {
			t.Fatal("callee order lost")
		}
		if call.Callable.Callable.Template.Node.Kind != ast.KindFunctionDeclaration || call.Callable.Callable.Template.Declaration.Module != call.Import.Module {
			t.Fatal("declaration/body authority mixed")
		}
		for _, capture := range call.Captures {
			if len(capture.Pending) == 0 || !capture.Cell.NeedsRuntimeBinding {
				t.Fatal("capture value promoted")
			}
		}
		switch name {
		case "serialize":
			if len(call.Arguments) != 3 || call.Arguments[0].ScalarOnNormalEvaluation != "string" || call.Arguments[1].ScalarOnNormalEvaluation != "" || len(call.Arguments[2].ConstructorOwnFields) != 2 {
				t.Fatal("serialize source arguments changed")
			}
			fields := call.Arguments[2].ConstructorOwnFields
			if fields[0].Name != "httpOnly" || fields[0].Value.ScalarOnNormalEvaluation != "boolean" || fields[1].Name != "maxAge" || fields[1].Value.ScalarOnNormalEvaluation != "number" {
				t.Fatal("literal own-field order changed")
			}
			if len(call.Controls) != 1 || call.Controls[0].Edge != "then" {
				t.Fatal("lazy source branch lost")
			}
		case "parse":
			if len(call.Arguments) != 1 || call.Arguments[0].ScalarOnNormalEvaluation != "" || len(call.MissingFormalPositions) != 1 || call.MissingFormalPositions[0] != 1 {
				t.Fatal("actual omission lost")
			}
		case "default":
			if call.Callable.Callable.Template.Name != "escapeHtml" || len(call.Arguments) != 1 || call.Arguments[0].ScalarOnNormalEvaluation != "" || len(call.Controls) != 1 || call.Controls[0].Edge != "truthy" {
				t.Fatal("parse result/guard falsely certified")
			}
		default:
			t.Fatal(name)
		}
	}
	if !seen["parse"] || !seen["serialize"] || !seen["default"] {
		t.Fatal(seen)
	}
	cjsDomainReport(t, "actual-call-obligations.json", calls)
}
func TestCJSCallSourceSymbolControls(t *testing.T) {
	root := sourcefixture.Get(t, "cjs-domain-output")
	if root == "" {
		t.Fatal("explicit output")
	}
	dir := filepath.Join(root, "symbol-control")
	cjsBodyWrite(t, filepath.Join(dir, "package.json"), `{"type":"module"}`)
	cjsBodyWrite(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"strict":true,"module":"NodeNext","allowImportingTsExtensions":true,"noEmit":true},"files":["entry.ts"]}`)
	cjsBodyWrite(t, filepath.Join(dir, "entry.ts"), `import {f as renamed} from './subject.cjs';import other from './subject.cjs';export function run(){function f(){return 9};f();return renamed("a",true,{n:2*3})}export function shadow(renamed:(x:string)=>number){return renamed("local")}export function spread(){return renamed(...(["spread",true,{}] as const))}`)
	cjsBodyWrite(t, filepath.Join(dir, "subject.cjs"), `exports.f=f;function f(x,y,z){return x}`)
	p := cjsDomainSnapshot(t, filepath.Join(dir, "tsconfig.json"), "entry.ts")
	calls, e := checked.CJSCallSources(p)
	if e != nil {
		t.Fatal(e)
	}
	if len(calls) != 2 || calls[0].Import.Requested != "f" || calls[0].Import.Local.Module != filepath.Join(dir, "entry.ts") || len(calls[0].Arguments) != 3 {
		t.Fatal("shadow/unused import or ordered arguments lost", calls)
	}
	if calls[0].Arguments[0].ScalarOnNormalEvaluation != "string" || calls[0].Arguments[1].ScalarOnNormalEvaluation != "boolean" || len(calls[0].Arguments[2].ConstructorOwnFields) != 1 {
		t.Fatal("literal observations lost")
	}
	if !calls[1].ArgumentMappingPending || len(calls[1].MissingFormalPositions) != 0 {
		t.Fatal("spread mapped to false omitted formals")
	}
	cjsDomainReport(t, "symbol-control.json", calls)
}
