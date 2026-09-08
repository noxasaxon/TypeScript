package extract

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func registryRoot(t *testing.T) string {
	t.Helper()
	root := sourcefixture.Get(t, "registry-output")
	if root == "" {
		t.Fatal("explicit scratch directory required")
	}
	return root
}
func registryScope(t *testing.T, p *checked.CJSBodySourceSnapshot) *checked.SourceRecoveryScope {
	t.Helper()
	s, e := checked.NewSourceRecoveryScope(p)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func registryBinding(t *testing.T, s *checked.SourceRecoveryScope, n *ast.Node) graph.SourceLexicalID {
	t.Helper()
	h, e := s.Binding(n)
	if e != nil {
		t.Fatal(e)
	}
	id, e := s.BindingID(h)
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func registryNodes(file *ast.SourceFile, name string) []*ast.Node {
	var out []*ast.Node
	var walk func(*ast.Node) bool
	walk = func(n *ast.Node) bool {
		if n == nil {
			return false
		}
		if n.Kind == ast.KindIdentifier && n.Text() == name {
			out = append(out, n)
		}
		n.ForEachChild(walk)
		return false
	}
	walk(file.AsNode())
	return out
}
func registrySave(t *testing.T, name string, v any) {
	t.Helper()
	bytes, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	cjsBodyWrite(t, filepath.Join(registryRoot(t), name), string(bytes))
}
func registryOperations(t *testing.T, p *checked.CJSBodySourceSnapshot, v graph.SourceRegistryView) {
	t.Helper()
	for _, m := range v.Modules {
		plan := p.Plans.Modules[m.Identity]
		if plan == nil {
			t.Fatal("lost actual plan")
		}
		if len(m.Startup) != len(plan.Initialization) {
			t.Fatal("dropped initializer")
		}
		for i, op := range plan.Initialization {
			r := m.Startup[i]
			if r.Ordinal != i || r.Phase != op.Phase || r.Site.Start != op.Source.Start || r.Site.End != op.Source.End || v.Files[r.Site.File-1].Path != op.Source.Module || r.Status != "source-retained-unproved" {
				t.Fatal("rewritten initializer", m.Identity, i)
			}
		}
		if len(m.Hoisted) != len(plan.HoistedFunctions) {
			t.Fatal("hoisted template table lost")
		}
		for i, h := range plan.HoistedFunctions {
			if v.Templates[m.Hoisted[i]-1].Declaration.Start != h.Declaration.Start {
				t.Fatal("hoist order lost")
			}
		}
		if len(m.Templates) != len(plan.Functions) || m.StartupStatus != "unproved" || m.EffectStatus != "unproved" {
			t.Fatal("lost body or false proof")
		}
	}
}
func TestSourceRecoveryRegistryIdentity(t *testing.T) {
	dir := filepath.Join(registryRoot(t), "identity")
	cjsBodyWrite(t, filepath.Join(dir, "package.json"), `{"type":"module"}`)
	cjsBodyWrite(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"strict":true,"module":"NodeNext","allowImportingTsExtensions":true,"noEmit":true},"files":["entry.ts","unused.ts"]}`)
	cjsBodyWrite(t, filepath.Join(dir, "entry.ts"), `import {read,a,b} from './subject.cjs';export {read,a,b}`)
	cjsBodyWrite(t, filepath.Join(dir, "unused.ts"), `throw new Error("must not initialize");export {}`)
	cjsBodyWrite(t, filepath.Join(dir, "subject.cjs"), `var count=1;var count=2;exports.read=read;function read(){return count}const make=()=>{let value=1;return function captured(){return value}};exports.a=make();exports.b=make();`)
	p := cjsDomainSnapshot(t, filepath.Join(dir, "tsconfig.json"), "entry.ts")
	s := registryScope(t, p)
	view := s.View()
	registryOperations(t, p, view)
	if len(view.ConfiguredRoots) != 2 {
		t.Fatal("lost configured root")
	}
	var unused bool
	for _, file := range view.Files {
		if strings.HasSuffix(file.Path, "/unused.ts") {
			unused = file.ConfiguredRoot && !file.SelectedRuntime
		}
	}
	if !unused {
		t.Fatal("configured roots conflated with runtime closure")
	}
	for _, module := range view.Modules {
		if strings.HasSuffix(module.Identity, "unused.ts") {
			t.Fatal("invented execution")
		}
	}
	file := p.Program.Compiler.GetSourceFile(filepath.Join(dir, "subject.cjs"))
	counts := registryNodes(file, "count")
	if len(counts) != 3 {
		t.Fatal(len(counts))
	}
	// Two independent builder adapters, sharing the same recovery scope.
	builderA := func(n *ast.Node) graph.SourceLexicalID { return registryBinding(t, s, n) }
	builderB := func(n *ast.Node) graph.SourceLexicalID { return registryBinding(t, s, n) }
	first := builderA(counts[0])
	if first != builderB(counts[0]) || first != builderB(counts[1]) || first != builderA(counts[2]) {
		t.Fatal("var redeclarations split lexical identity")
	}
	if len(s.View().Bindings[first-1].Declarations) != 2 {
		t.Fatal("var declaration source lost")
	}
	h, e := s.Binding(counts[0])
	if e != nil {
		t.Fatal(e)
	}
	other := registryScope(t, p)
	if _, e = other.BindingID(h); e == nil {
		t.Fatal("foreign scope handle accepted")
	}
	if other.View().SnapshotFingerprint != s.View().SnapshotFingerprint {
		t.Fatal("same snapshot fingerprint changed")
	}
	if other.View().Scope == s.View().Scope {
		t.Fatal("scope identities collide")
	}
	p2 := cjsDomainSnapshot(t, filepath.Join(dir, "tsconfig.json"), "entry.ts")
	if _, e = s.Binding(registryNodes(p2.Program.Compiler.GetSourceFile(file.FileName()), "count")[0]); e == nil {
		t.Fatal("foreign actual Program node accepted")
	}
	view = s.View()
	var captured []graph.SourceInstanceRecord
	for _, instance := range view.Instances {
		if view.Templates[instance.Template-1].Name == "captured" {
			captured = append(captured, instance)
		}
	}
	if len(captured) != 2 || captured[0].Template != captured[1].Template || captured[0].Environment == captured[1].Environment {
		t.Fatal("declaration substituted for runtime instance", captured)
	}
	var cells []graph.SourceCellRecord
	for _, cell := range view.Cells {
		if cell.Binding != 0 && view.Bindings[cell.Binding-1].Name == "value" {
			cells = append(cells, cell)
		}
	}
	if len(cells) != 2 || cells[0].Binding != cells[1].Binding || cells[0].Environment == cells[1].Environment || cells[0].ID == cells[1].ID {
		t.Fatal("capture cells collapsed", cells)
	}
	exports := registryBinding(t, s, registryNodes(file, "exports")[0])
	var wrapper, record graph.SourceCellID
	for _, cell := range s.View().Cells {
		if cell.Binding == exports {
			wrapper = cell.ID
		}
		if cell.Role == "module-record-exports" {
			record = cell.ID
		}
	}
	if wrapper == 0 || record == 0 || wrapper == record {
		t.Fatal("exports alias conflated with persistent record")
	}
	view.Files[0].Path = "mutated"
	view.Modules[0].Startup[0].Status = "native-certified"
	if s.View().Files[0].Path == "mutated" || s.View().Modules[0].Startup[0].Status == "native-certified" {
		t.Fatal("view shares mutable registry state")
	}
	cjsBodyNode(t, dir, `[ns.read(),ns.a===ns.b,ns.a(),ns.b()]`, `[2,false,1,1]`)
	registrySave(t, "identity-view.json", s.View())
}
func TestSourceRecoveryRegistryPortfolio(t *testing.T) {
	portfolio := sourcefixture.Get(t, "portfolio")
	if portfolio == "" {
		t.Fatal("explicit frozen portfolio")
	}
	p := cjsDomainSnapshot(t, filepath.Join(portfolio, "tsconfig.json"), "cookie-route/route.ts")
	s := registryScope(t, p)
	v := s.View()
	registryOperations(t, p, v)
	if len(v.ConfiguredRoots) != 10 || len(v.Modules) != 3 {
		t.Fatal("root/runtime inventory changed", len(v.ConfiguredRoots), len(v.Modules))
	}
	counts := map[int]int{}
	for _, module := range v.Modules {
		if module.Format == "commonjs" {
			counts[len(module.Startup)]++
		}
	}
	if counts[16] != 1 || counts[4] != 1 {
		t.Fatal("original package initializer operations lost", counts)
	}
	for _, template := range v.Templates {
		if v.Files[template.Declaration.File-1].DeclarationFile || template.BodyStatus != "unproved" {
			t.Fatal("declaration/native body authority")
		}
	}
	if len(v.Obligations) == 0 || len(v.Instances) == 0 {
		t.Fatal("lost pending source instances")
	}
	if len(v.Imports) != len(p.Plans.Imports) {
		t.Fatal("actual checker import linkage lost")
	}
	for i, row := range v.Imports {
		original := p.Plans.Imports[i]
		if row.Requested != original.Requested || row.Form != original.Form || row.Capture != original.Capture || row.Status != "export-binding-and-invocation-unproved" || v.Modules[row.Target-1].Identity != original.Module || row.Local == 0 {
			t.Fatal("import promoted or identity lost")
		}
		if len(row.TypeDeclarations) != len(original.TypeDeclarations) {
			t.Fatal("declaration linkage lost")
		}
	}
	effects := 0
	for _, state := range p.States {
		effects += len(state.Effects)
	}
	if effects != len(v.ModeledEffects) {
		t.Fatal("source-modeled operation omitted")
	}
	registrySave(t, "portfolio-view.json", v)
}
func TestSourceRecoveryRegistryPlainView(t *testing.T) {
	var check func(reflect.Type)
	check = func(typ reflect.Type) {
		switch typ.Kind() {
		case reflect.Pointer, reflect.Interface, reflect.UnsafePointer, reflect.Func, reflect.Chan:
			t.Fatalf("not plain graph data: %s", typ)
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				check(typ.Field(i).Type)
			}
		case reflect.Slice, reflect.Array:
			check(typ.Elem())
		case reflect.Map:
			check(typ.Key())
			check(typ.Elem())
		}
	}
	check(reflect.TypeOf(graph.SourceRegistryView{}))
}

func TestSourceRecoveryRegistryWrapperControls(t *testing.T) {
	dir := filepath.Join(registryRoot(t), "wrappers")
	cjsBodyWrite(t, filepath.Join(dir, "package.json"), `{"type":"module"}`)
	cjsBodyWrite(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"strict":true,"module":"NodeNext","allowImportingTsExtensions":true,"noEmit":true},"files":["entry.ts"]}`)
	cjsBodyWrite(t, filepath.Join(dir, "entry.ts"), `import first from './subject.cjs';import second from './other.cjs';export {first,second}`)
	cjsBodyWrite(t, filepath.Join(dir, "subject.cjs"), `var module;exports.call=read;function read(){return typeof module}function local(module){return module}function nested(){var module;return module}function lexical(){let exports="local";return exports}exports.local=local;exports.nested=nested;exports.lexical=lexical;`)
	cjsBodyWrite(t, filepath.Join(dir, "other.cjs"), `exports.a=()=>exports;`)
	p := cjsDomainSnapshot(t, filepath.Join(dir, "tsconfig.json"), "entry.ts")
	s := registryScope(t, p)
	file := p.Program.Compiler.GetSourceFile(filepath.Join(dir, "subject.cjs"))
	nodes := registryNodes(file, "module")
	if len(nodes) != 6 {
		t.Fatal("unexpected exact module nodes", len(nodes))
	}
	ids := []graph.SourceLexicalID{}
	for _, n := range nodes {
		ids = append(ids, registryBinding(t, s, n))
	}
	if ids[0] != ids[1] || ids[2] != ids[3] || ids[4] != ids[5] || ids[0] == ids[2] || ids[0] == ids[4] || ids[2] == ids[4] {
		t.Fatal("wrapper/nested variable/parameter collapsed", ids)
	}
	var cells int
	for _, cell := range s.View().Cells {
		if cell.Binding == ids[0] {
			cells++
		}
	}
	if cells != 1 {
		t.Fatal("wrapper var invented second runtime cell", cells)
	}
	exports := registryNodes(file, "exports")
	local := registryBinding(t, s, exports[1])
	if local != registryBinding(t, s, exports[2]) || local == registryBinding(t, s, exports[0]) {
		t.Fatal("lexical exports shadow lost")
	}
	other := p.Program.Compiler.GetSourceFile(filepath.Join(dir, "other.cjs"))
	if registryBinding(t, s, registryNodes(other, "exports")[0]) == registryBinding(t, s, exports[0]) {
		t.Fatal("wrapper parameter reused across module instances")
	}
	view := s.View()
	for _, binding := range view.Bindings {
		for _, did := range binding.Declarations {
			if view.Declarations[did-1].Binding != binding.ID {
				t.Fatal("declaration bound to two cells", binding)
			}
		}
	}
	cjsBodyNode(t, dir, `[ns.call(),ns.local(3),ns.nested()===undefined,ns.lexical()]`, `["object",3,true,"local"]`)
	registrySave(t, "wrapper-view.json", s.View())
}
func TestSourceRecoveryRegistryRejectsMissingOperations(t *testing.T) {
	portfolio := sourcefixture.Get(t, "portfolio")
	if portfolio == "" {
		t.Fatal("explicit portfolio")
	}
	p := cjsDomainSnapshot(t, filepath.Join(portfolio, "tsconfig.json"), "cookie-route/route.ts")
	plan := p.Plans.Modules[p.Program.Entry.FileName()]
	plan.Initialization = plan.Initialization[1:]
	if _, e := checked.NewSourceRecoveryScope(p); e == nil {
		t.Fatal("incomplete initializer list accepted")
	}
}

func TestSourceRecoveryRegistryRejectsMissingBody(t *testing.T) {
	portfolio := sourcefixture.Get(t, "portfolio")
	if portfolio == "" {
		t.Fatal("explicit portfolio")
	}
	p := cjsDomainSnapshot(t, filepath.Join(portfolio, "tsconfig.json"), "cookie-route/route.ts")
	plan := p.Plans.Modules[p.Program.Entry.FileName()]
	plan.Functions = nil
	if _, e := checked.NewSourceRecoveryScope(p); e == nil {
		t.Fatal("source body omission accepted")
	}
}
