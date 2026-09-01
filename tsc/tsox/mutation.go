package tsox

import (
	"github.com/microsoft/typescript-go/internal/tsox/mutationsites"
	"github.com/microsoft/typescript-go/tsox/mutation"
)

// MutationSites checks source and returns the pointer-free call-interaction
// coordinates used by offline mutation tools.
func MutationSites(sourcePath string, source string) mutation.Result {
	return mutationsites.Extract(sourcePath, source)
}
