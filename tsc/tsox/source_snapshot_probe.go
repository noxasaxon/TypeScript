package tsox

import (
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/internal/tsox/extract"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// RegisteredSourceBody is a concrete capability issued only by actual frontend
// snapshot capture. Graph data and user implementations of a resolver cannot
// construct its private owner. The zero value is invalid.
type RegisteredSourceBody struct{ owner extract.ScopedTypedBodyHandle }

func (b RegisteredSourceBody) ResolveSourceBody() (*graph.TypedSourceBody, error) {
	return b.owner.ResolveSourceBody()
}

// Scratch bridge captures the compiler Program and issues its live body handles.
// There is intentionally no public constructor accepting graphs or resolvers.
func ScratchRegisteredSourceBodies(config, entry, export string) ([]RegisteredSourceBody, error) {
	snap, e := checked.ReadCJSBodySourceSnapshot(config, entry, checked.CJSExportBoundary{})
	if e != nil {
		return nil, e
	}
	scope, e := checked.NewSourceRecoveryScope(snap)
	if e != nil {
		return nil, e
	}
	registry, e := extract.RegisterScopedTypedBodies(scope, export)
	if e != nil {
		return nil, e
	}
	var out []RegisteredSourceBody
	for _, h := range registry.Handles() {
		out = append(out, RegisteredSourceBody{owner: h})
	}
	return out, nil
}
