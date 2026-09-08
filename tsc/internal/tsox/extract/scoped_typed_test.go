package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func scopedWeb(t *testing.T) (*checked.SourceRecoveryScope, *ScopedTypedBodies) {
	t.Helper()
	p := sourcefixture.Get(t, "portfolio")
	s, e := checked.ReadCJSBodySourceSnapshot(filepath.Join(p, "tsconfig.json"), "web-api/route.ts", checked.CJSExportBoundary{})
	if e != nil {
		t.Fatal(e)
	}
	scope, e := checked.NewSourceRecoveryScope(s)
	if e != nil {
		t.Fatal(e)
	}
	b, e := RegisterScopedTypedBodies(scope, "handle")
	if e != nil {
		t.Fatal(e)
	}
	return scope, b
}
func TestSameSnapshotWebBodies(t *testing.T) {
	scope, b := scopedWeb(t)
	view := scope.View()
	if len(view.ConfiguredRoots) != 10 || len(view.Modules) != 4 {
		t.Fatal("scope lost")
	}
	model := false
	for _, f := range view.Files {
		if filepath.Base(f.Path) == "model.ts" {
			model = f.OwnedSchema && !f.SelectedRuntime
		}
	}
	if !model {
		t.Fatal("model roles")
	}
	for _, m := range view.Modules {
		if m.StartupStatus != "unproved" || m.EffectStatus != "unproved" {
			t.Fatal("startup proof invented")
		}
	}
	if len(b.Handles()) != 7 {
		t.Fatal("four helpers and three actual async templates required")
	}
	report := []any{}
	for _, h := range b.Handles() {
		body, e := h.ResolveSourceBody()
		if e != nil {
			t.Fatal(e)
		}
		if body.Registry.Scope != view.Scope {
			t.Fatal("scope split")
		}
		if body.Program.SourceRecovery == nil {
			t.Fatal("source-only guard absent")
		}
		mapped := 0
		for x := range body.Expressions {
			sh, e := b.SourceOf(h, x)
			if e != nil {
				t.Fatal(e)
			}
			if _, e := scope.SourceSite(sh); e != nil {
				t.Fatal(e)
			}
			copy := *x
			if _, e := b.SourceOf(h, &copy); e == nil {
				t.Fatal("cloned graph accepted")
			}
			mapped++
		}
		if mapped == 0 {
			t.Fatal("body lacks actual source expressions")
		}
		name := "async"
		if body.Function != nil {
			name = body.Function.Name
		}
		report = append(report, map[string]any{"name": name, "template": body.Template, "source": body.Source, "mappedExpressions": mapped, "unmappedExpressions": body.UnmappedExpressions, "bindings": body.Bindings, "obligations": body.Obligations})
		if body.Function != nil && name == "createTask" {
			data, _ := json.MarshalIndent(body, "", "  ")
			os.WriteFile(filepath.Join(sourcefixture.Get(t, "snapshot-output"), "createTask.json"), data, 0600)
		}
	}
	root := sourcefixture.Get(t, "snapshot-output")
	data, _ := json.MarshalIndent(report, "", "  ")
	os.WriteFile(filepath.Join(root, "bodies.json"), data, 0600)
	data, _ = json.MarshalIndent(view, "", "  ")
	os.WriteFile(filepath.Join(root, "scope.json"), data, 0600)
	t.Log("one scope", len(view.ConfiguredRoots), "configured", len(view.Modules), "runtime", len(report), "bodies")
}
func TestSameSnapshotRejectsForeignAndStale(t *testing.T) {
	scope, a := scopedWeb(t)
	other, b := scopedWeb(t)
	h := a.Handles()[0]
	foreign := b.Handles()[0]
	if _, e := a.resolve(foreign); e == nil {
		t.Fatal("foreign body accepted")
	}
	body, e := h.ResolveSourceBody()
	if e != nil {
		t.Fatal(e)
	}
	for x := range body.Expressions {
		s, e := a.SourceOf(h, x)
		if e != nil {
			t.Fatal(e)
		}
		if _, e := other.SourceSite(s); e == nil {
			t.Fatal("foreign source handle accepted")
		}
		break
	}
	if _, e := a.BindingSources(h, graph.BindingID(999999)); e == nil {
		t.Fatal("guessed graph id accepted")
	}
	old := body.Function.Name
	body.Function.Name = "changed"
	if _, e := h.ResolveSourceBody(); e == nil {
		t.Fatal("stale graph accepted")
	}
	body.Function.Name = old
	if _, e := h.ResolveSourceBody(); e != nil {
		t.Fatal(e)
	}
	p := scope.ActualProgram()
	var modelPath string
	for f, path := range p.Files {
		if filepath.Base(path) == "model.ts" {
			modelPath = path
			delete(p.Files, f)
			if _, e := h.ResolveSourceBody(); e == nil {
				t.Fatal("lost schema coverage accepted")
			}
			p.Files[f] = path
			break
		}
	}
	if modelPath == "" {
		t.Fatal("model absent")
	}
	p.RuntimeFiles = append(p.RuntimeFiles, p.Compiler.GetSourceFile(modelPath))
	if _, e := h.ResolveSourceBody(); e == nil {
		t.Fatal("schema file became runtime initializer")
	}
}

func TestSameSnapshotOriginalProgramMembership(t *testing.T) {
	_, r := scopedWeb(t)
	h := r.Handles()[0]
	b, e := h.ResolveSourceBody()
	if e != nil {
		t.Fatal(e)
	}
	original := b.Program.Statements[0]
	clone := *original
	b.Program.Statements[0] = &clone
	if _, e = h.ResolveSourceBody(); e == nil {
		t.Fatal("byte-identical replaced original statement accepted")
	}
	b.Program.Statements[0] = original
	for i, a := range r.Handles() {
		left, _ := a.ResolveSourceBody()
		for _, other := range r.Handles()[i+1:] {
			right, _ := other.ResolveSourceBody()
			for id := range left.Bindings {
				if len(right.Bindings[id]) > 0 {
					binding, e := r.BindingHandle(a, id)
					if e != nil {
						t.Fatal(e)
					}
					if _, e = r.BindingOrigins(other, binding); e == nil {
						t.Fatal("same integer from another body accepted")
					}
					return
				}
			}
		}
	}
	t.Fatal("missing actual colliding graph-local IDs")
}

func TestSameSnapshotActualHelperDeclarationLinks(t *testing.T) {
	_, r := scopedWeb(t)
	helpers := map[checked.ScopedSourceHandle]string{}
	for _, h := range r.Handles() {
		b, e := h.ResolveSourceBody()
		if e != nil {
			t.Fatal(e)
		}
		if b.Function != nil {
			s, e := r.BodySource(h)
			if e != nil {
				t.Fatal(e)
			}
			helpers[s] = b.Function.Name
		}
	}
	linked := map[string]bool{}
	for _, h := range r.Handles() {
		b, e := h.ResolveSourceBody()
		if e != nil {
			t.Fatal(e)
		}
		if b.Async == nil {
			continue
		}
		for id := range b.Bindings {
			handle, e := r.BindingHandle(h, id)
			if e != nil {
				t.Fatal(e)
			}
			sources, e := r.BindingOrigins(h, handle)
			if e != nil {
				t.Fatal(e)
			}
			for _, source := range sources {
				if name, ok := helpers[source]; ok {
					linked[name] = true
				}
			}
		}
	}
	if len(linked) != 4 {
		t.Fatal("actual original declaration handles fail to link async bodies and helpers", linked)
	}
}
