package extract

import (
	"path/filepath"
	"testing"

	"github.com/microsoft/typescript-go/internal/tsox/checked"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestComposedPromiseStageMembership(t *testing.T) {
	root := sourcefixture.Get(t, "promise")
	snapshot, e := checked.ReadCJSBodySourceSnapshot(filepath.Join(root, "tsconfig.json"), "handler.ts", checked.CJSExportBoundary{})
	if e != nil {
		t.Fatal(e)
	}
	scope, e := checked.NewSourceRecoveryScope(snapshot)
	if e != nil {
		t.Fatal(e)
	}
	bodies, e := RegisterScopedTypedBodies(scope, "handle")
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	for _, h := range bodies.Handles() {
		b, e := h.ResolveSourceBody()
		if e != nil {
			t.Fatal(e)
		}
		if b.Async == nil {
			continue
		}
		for i := range b.Async.Stages {
			stage := &b.Async.Stages[i]
			x := stage.Await.Promise
			if x == nil {
				continue
			}
			count++
			copied := *x
			stage.Await.Promise = &copied
			_, err := h.ResolveSourceBody()
			stage.Await.Promise = x
			if err == nil {
				t.Error("stage-only identical promise-expression clone accepted by body owner")
			}
			if _, e := h.ResolveSourceBody(); e != nil {
				t.Fatal("restored original rejected", e)
			}
		}
	}
	if count == 0 {
		t.Fatal("no actual first-source promise stage")
	}
}
