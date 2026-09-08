package checked

import (
	"crypto/sha256"
	_ "embed"
	"fmt"
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/tsoptions"
	"github.com/microsoft/typescript-go/internal/tspath"
)

// Standard library declarations belong to the compiler, not to a caller's source
// map. The reserved path is checked for collisions before creating the checker.
const StandardNodeDeclarationPath = "/__tsox_lib__/node24.20.0/fs-promises.d.ts"
const StandardNodeGlobalsPath = "/__tsox_lib__/node24.20.0/globals.d.ts"

//go:embed standard/node24/fs-promises.d.ts
var standardNodeDeclarations string

//go:embed standard/node24/globals.d.ts
var standardNodeGlobals string

func StandardModule(name string) bool { return name == "node:fs/promises" }

// Registered declarations describe actual host module exports. Other ambient
// declarations remain erased source declarations, including same-named stubs.
func standardRuntimeSymbol(symbol *ast.Symbol) bool {
	if symbol == nil || symbol.Name != "readFile" || len(symbol.Declarations) == 0 {
		return false
	}
	for _, declaration := range symbol.Declarations {
		if ast.GetSourceFileOfNode(declaration).FileName() != StandardNodeDeclarationPath {
			return false
		}
	}
	return true
}
func StandardLibraryFingerprint() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(standardNodeDeclarations+"\x00"+standardNodeGlobals)))
}
func standardConfig(config *tsoptions.ParsedCommandLine) *tsoptions.ParsedCommandLine {
	names := append(append([]string{}, config.FileNames()...), StandardNodeDeclarationPath, StandardNodeGlobalsPath)
	result := tsoptions.NewParsedCommandLine(config.CompilerOptions(), names, tspath.ComparePathsOptions{UseCaseSensitiveFileNames: config.UseCaseSensitiveFileNames(), CurrentDirectory: config.GetCurrentDirectory()})
	parsed := *config.ParsedConfig
	parsed.FileNames = names
	result.ParsedConfig = &parsed
	result.ConfigFile, result.Errors, result.Raw, result.CompileOnSave = config.ConfigFile, config.Errors, config.Raw, config.CompileOnSave
	return result
}
