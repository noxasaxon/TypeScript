package tsox

import (
	"github.com/microsoft/typescript-go/internal/tsox/mutationsites"
	"github.com/microsoft/typescript-go/tsox/mutation"
)

// MutationSites checks source and returns the pointer-free coordinates used by
// offline mutation tools.
func MutationSites(sourcePath string, source string) mutation.Result {
	return mutationsites.Extract(sourcePath, source)
}

// MutationSitesFiles checks all modules but reports only entry-source spans.
// Dependencies are immutable context for this slice's mutation operators.
func MutationSitesFiles(entry string, sources map[string]string) mutation.Result {
	return mutationsites.ExtractFiles(entry, sources)
}
