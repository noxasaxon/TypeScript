package checked

import (
	"path/filepath"
	"testing"

	"github.com/microsoft/typescript-go/internal/ast"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestSourceSyntaxOwnerAndCaches(t *testing.T) {
	snapshot, e := ReadCJSBodySourceSnapshot(filepath.Join(sourcefixture.Get(t, "portfolio"), "tsconfig.json"), "web-api/route.ts", CJSExportBoundary{})
	if e != nil {
		t.Fatal(e)
	}
	scope, e := NewSourceRecoveryScope(snapshot)
	if e != nil {
		t.Fatal(e)
	}
	old := snapshot.Program
	copy := *old
	snapshot.Program = &copy
	if scope.ValidateSourceCoverage() == nil {
		t.Fatal("same numeric/source pointers in foreign Program container accepted")
	}
	snapshot.Program = old
	compiler := old.Compiler
	old.Compiler = nil
	if scope.ValidateSourceCoverage() == nil {
		t.Fatal("missing compiler owner accepted")
	}
	old.Compiler = compiler
	// Mutating both exposed runtime lists cannot rewrite captured module order.
	old.RuntimeFiles[0], old.RuntimeFiles[1] = old.RuntimeFiles[1], old.RuntimeFiles[0]
	snapshot.RuntimeModules[0], snapshot.RuntimeModules[1] = snapshot.RuntimeModules[1], snapshot.RuntimeModules[0]
	if scope.ValidateSourceCoverage() == nil {
		t.Fatal("coordinated runtime order mutation accepted")
	}
	old.RuntimeFiles[0], old.RuntimeFiles[1] = old.RuntimeFiles[1], old.RuntimeFiles[0]
	snapshot.RuntimeModules[0], snapshot.RuntimeModules[1] = snapshot.RuntimeModules[1], snapshot.RuntimeModules[0]
	// Subtree facts are lazy upstream syntax summaries, not source mutations.
	for n := range scope.nodes {
		_ = n.SubtreeFacts()
	}
	if e := scope.ValidateSourceCoverage(); e != nil {
		t.Fatal("restored source or legitimate subtree cache rejected", e)
	}
	t.Log(len(scope.nodes), "original nodes", len(scope.integrity.fields), "syntax fields")
	var node *ast.Node
	for n := range scope.nodes {
		if n.Kind == ast.KindIdentifier {
			node = n
			break
		}
	}
	if _, e := scope.SourceHandle(node); e != nil {
		t.Fatal(e)
	}
}
