package tsox

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/internal/tsox/extract"
	"github.com/microsoft/typescript-go/tsox/graph"
)

func ScratchRecoverScopedBody(config, entry, name string) (*graph.RecoveredSourceBody, []graph.Diagnostic) {
	snapshot, e := checked.ReadCJSBodySourceSnapshot(config, entry, checked.CJSExportBoundary{PristineObjectPrototype: true, IntrinsicDefineProperty: true, DescriptorPrototypeClean: true, IntrinsicObjectCreate: true, IntrinsicObjectPrototypeToString: true})
	if e != nil {
		return nil, []graph.Diagnostic{{Message: e.Error()}}
	}
	scope, e := checked.NewSourceRecoveryScope(snapshot)
	if e != nil {
		return nil, []graph.Diagnostic{{Message: e.Error()}}
	}
	for _, node := range snapshot.Program.Entry.Statements.Nodes {
		if node.Kind == ast.KindFunctionDeclaration && node.Name() != nil && node.Name().Text() == name {
			return extract.RecoverScopedSourceBody(scope, node)
		}
	}
	return nil, []graph.Diagnostic{{Message: "actual selected source function missing"}}
}
