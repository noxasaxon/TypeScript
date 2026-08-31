// Package tsox is the exported entry point to TSOX's in-fork code. It exists
// so code outside this module (the tsox repo) can consume what TSOX extracts
// from compiler internals.
package tsox

import "github.com/microsoft/typescript-go/internal/core"

// Marker proves the fork→submodule→workspace chain: it returns a string
// derived from a compiler-internal symbol.
func Marker() string {
	return "tsox-ok ts=" + core.Version()
}
