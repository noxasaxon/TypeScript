package extract

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
	"testing"
)

func TestIndependentGraphNeighbors(t *testing.T) {
	scope, r := scopedWeb(t)
	h := r.Handles()[0]
	b, e := h.ResolveSourceBody()
	if e != nil {
		t.Fatal(e)
	}
	reject := func(t *testing.T) {
		t.Helper()
		if _, e := h.ResolveSourceBody(); e == nil {
			t.Fatal("mutated graph accepted")
		}
	}
	t.Run("parameter", func(t *testing.T) {
		old := b.Parameters[0].Name
		defer func() { b.Parameters[0].Name = old }()
		b.Parameters[0].Name = "foreign"
		reject(t)
	})
	t.Run("registry-ledger", func(t *testing.T) {
		old := b.Registry.Obligations
		defer func() { b.Registry.Obligations = old }()
		b.Registry.Obligations = nil
		reject(t)
	})
	t.Run("bindings", func(t *testing.T) {
		for id, v := range b.Bindings {
			old := append([]graph.SourceLexicalID(nil), v...)
			defer func() { b.Bindings[id] = old }()
			b.Bindings[id][0]++
			reject(t)
			return
		}
		t.Fatal("no bindings")
	})
	t.Run("cloned-expression-replacement", func(t *testing.T) {
		x := b.Function.Body[0].Value
		if x == nil {
			t.Fatal("no value")
		}
		clone := *x
		defer func() { b.Function.Body[0].Value = x }()
		b.Function.Body[0].Value = &clone
		reject(t)
	})
	t.Run("cloned-body", func(t *testing.T) {
		old := b.Function
		copy := *old
		defer func() { b.Function = old }()
		b.Function = &copy
		reject(t)
	})
	t.Run("cloned-program", func(t *testing.T) {
		old := b.Program
		copy := *old
		defer func() { b.Program = old }()
		b.Program = &copy
		reject(t)
	})
	t.Run("unmapped-synthetic", func(t *testing.T) {
		count := 0
		for _, hh := range r.Handles() {
			bb, e := hh.ResolveSourceBody()
			if e != nil {
				t.Fatal(e)
			}
			for _, x := range bb.GraphExpressions {
				if _, ok := bb.Expressions[x]; !ok {
					count++
					if _, e := r.SourceOf(hh, x); e == nil {
						t.Fatal("synthetic gained source identity")
					}
				}
			}
		}
		if count == 0 {
			t.Fatal("no actual synthetic")
		}
		t.Log(count, "actual unmapped expressions rejected")
	})
	t.Run("foreign-source-node", func(t *testing.T) {
		foreign, _ := scopedWeb(t)
		if _, e := scope.SourceHandle(foreign.ActualProgram().Entry.AsNode()); e == nil {
			t.Fatal("foreign AST accepted")
		}
		if _, e := scope.SourceSite(checked.ScopedSourceHandle{}); e == nil {
			t.Fatal("zero handle accepted")
		}
	})
	t.Run("capture-mutation", func(t *testing.T) {
		for _, hh := range r.Handles() {
			bb, e := hh.ResolveSourceBody()
			if e != nil {
				t.Fatal(e)
			}
			if len(bb.Captures) > 0 {
				old := bb.Captures
				defer func() { bb.Captures = old }()
				bb.Captures = nil
				if _, e := hh.ResolveSourceBody(); e == nil {
					t.Fatal("capture mutation accepted")
				}
				return
			}
		}
		t.Fatal("no captures")
	})
	t.Run("view-is-copy", func(t *testing.T) {
		v := scope.View()
		v.Modules[0].StartupStatus = "proved"
		v.Files[0].OwnedSchema = true
		if _, e := h.ResolveSourceBody(); e != nil {
			t.Fatal(e)
		}
		if scope.View().Modules[0].StartupStatus == "proved" {
			t.Fatal("view aliases authority")
		}
	})
}

func TestIndependentSourceMutation(t *testing.T) {
	scope, r := scopedWeb(t)
	h := r.Handles()[0]
	var fn *ast.Node
	for _, f := range scope.ActualProgram().RuntimeFiles {
		for _, n := range f.Statements.Nodes {
			if n.Kind == ast.KindFunctionDeclaration && n.Name().Text() == "authenticate" {
				fn = n
			}
		}
	}
	if fn == nil {
		t.Fatal("actual authenticate missing")
	}
	originalHandle, e := scope.SourceHandle(fn)
	if e != nil {
		t.Fatal(e)
	}
	t.Run("identifier-text", func(t *testing.T) {
		name := fn.Name().AsIdentifier()
		old := name.Text
		defer func() { name.Text = old }()
		name.Text = "changedAuthenticate"
		if _, e := h.ResolveSourceBody(); e == nil {
			t.Error("changed actual AST identifier accepted by body resolver")
		}
		if _, e := scope.SourceSite(originalHandle); e == nil {
			t.Error("changed actual AST still resolves source handle")
		}
	})
	t.Run("body-statement-membership", func(t *testing.T) {
		ss := fn.Body().AsBlock().Statements
		old := ss.Nodes[0]
		copy := old.Clone(ast.NewNodeFactory(ast.NodeFactoryHooks{}))
		copy.Parent = old.Parent
		defer func() { ss.Nodes[0] = old }()
		ss.Nodes[0] = copy
		if _, e := h.ResolveSourceBody(); e == nil {
			t.Error("byte-identical foreign AST statement replacement accepted")
		}
		if _, e := scope.SourceHandle(old); e == nil {
			t.Error("detached original AST statement still accepted as actual member")
		}
		if _, e := scope.SourceHandle(copy); e == nil {
			t.Error("new AST clone accepted")
		}
	})
	t.Run("parameter-node-replacement", func(t *testing.T) {
		ps := fn.FunctionLikeData().Parameters
		old := ps.Nodes[0]
		copy := old.Clone(ast.NewNodeFactory(ast.NodeFactoryHooks{}))
		copy.Parent = old.Parent
		defer func() { ps.Nodes[0] = old }()
		ps.Nodes[0] = copy
		if _, e := h.ResolveSourceBody(); e == nil {
			t.Error("byte-identical foreign AST parameter replacement accepted")
		}
	})
	t.Run("startup-order", func(t *testing.T) {
		file := scope.ActualProgram().Entry
		ss := file.Statements.Nodes
		if len(ss) < 2 {
			t.Fatal("no startup neighbors")
		}
		ss[0], ss[1] = ss[1], ss[0]
		defer func() { ss[0], ss[1] = ss[1], ss[0] }()
		if _, e := h.ResolveSourceBody(); e == nil {
			t.Error("reordered actual selected startup accepted")
		}
	})
	if _, e := h.ResolveSourceBody(); e != nil {
		t.Fatal("restored scope invalid", e)
	}
}
