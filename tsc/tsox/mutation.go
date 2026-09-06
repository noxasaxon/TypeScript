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
// Use MutationSitesModules to enumerate dependency-source spans as well.
func MutationSitesFiles(entry string, sources map[string]string) mutation.Result {
	return mutationsites.ExtractFiles(entry, sources)
}

// MutationSitesModules checks the complete immutable source snapshot and
// reports file-local sites for every reachable source in stable path order.
func MutationSitesModules(entry string, sources map[string]string) mutation.ModulesResult {
	return mutationsites.ExtractModules(entry, sources)
}
