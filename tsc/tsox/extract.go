package tsox

import (
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/internal/tsox/extract"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// ExtractGraph checks source and returns the first-slice semantic graph or a
// clean fence diagnostic. The returned graph contains no compiler pointers.
func ExtractGraph(sourcePath string, source string) graph.Result {
	return extract.Extract(sourcePath, source)
}

// ReadSourceFiles snapshots the entry and explicit relative .ts dependencies.
// The returned entry and keys are absolute paths after resolving the entry's
// realpath. No package/config files are loaded; symlink dependencies and
// unsupported import forms are diagnosed.
func ReadSourceFiles(sourcePath string) (string, map[string]string, []graph.Diagnostic) {
	return checked.ReadSources(sourcePath)
}

// ExtractGraphFiles checks a complete immutable source snapshot. Keys may be
// absolute or relative to a virtual root; entry and dependencies use the same
// namespace. Files outside the reachable module graph have no effect.
func ExtractGraphFiles(entry string, sources map[string]string) graph.Result {
	return extract.ExtractFiles(entry, sources)
}

// ExtractGraphFile checks an entry and its bounded module dependencies.
func ExtractGraphFile(sourcePath string) graph.Result {
	entry, sources, diagnostics := ReadSourceFiles(sourcePath)
	if len(diagnostics) != 0 {
		return graph.Result{Diagnostics: diagnostics}
	}
	// ReadSourceFiles resolves filesystem paths against cwd.
	result := extract.ExtractFiles(entry, sources)
	if result.Program != nil {
		result.Program.SourcePath = sourcePath
	}
	return result
}

// ExtractAsyncGraphFiles checks the separate bounded asynchronous host surface.
func ExtractAsyncGraphFiles(entry string, sources map[string]string, entryName, hostName string) graph.AsyncResult {
	return extract.ExtractAsyncFiles(entry, sources, entryName, hostName)
}

func ExtractAsyncGraphFile(sourcePath, entryName, hostName string) graph.AsyncResult {
	entry, sources, diagnostics := ReadSourceFiles(sourcePath)
	if len(diagnostics) != 0 {
		return graph.AsyncResult{Diagnostics: diagnostics}
	}
	return extract.ExtractAsyncFiles(entry, sources, entryName, hostName)
}
