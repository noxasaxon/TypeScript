package tsox

import (
	"github.com/microsoft/typescript-go/internal/tsox/extract"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// ExtractGraph checks source and returns the first-slice semantic graph or a
// clean fence diagnostic. The returned graph contains no compiler pointers.
func ExtractGraph(sourcePath string, source string) graph.Result {
	return extract.Extract(sourcePath, source)
}
