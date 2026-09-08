package checked

import (
	"fmt"
	"path/filepath"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/parser"
	"github.com/microsoft/typescript-go/internal/tspath"
)

// Explicit Node format metadata selects the grammar. No-type JS requires a
// source certificate: ordinary CJS syntax is not a declaration-file property.
// Await and wrapper-binding ambiguity is a named unfinished syntax boundary.
func packageSourceFormat(name, source, kind string) (string, error) {
	switch filepath.Ext(name) {
	case ".cjs":
		return "commonjs", nil
	case ".mjs":
		return "module", nil
	case ".ts":
		if kind == "module" {
			return "module-typescript", nil
		}
		return "", fmt.Errorf("ambiguous TypeScript format is not implemented")
	}
	if kind == "module" || kind == "commonjs" {
		return kind, nil
	}
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: name, Path: tspath.Path(name)}, source, core.ScriptKindJS)
	if len(file.Diagnostics()) != 0 || len(file.JSDiagnostics()) != 0 {
		return "", fmt.Errorf("source format parse diagnostics")
	}
	if ast.IsExternalModule(file) {
		return "module", nil
	}
	var ambiguous bool
	var visit func(*ast.Node) bool
	visit = func(n *ast.Node) bool {
		if n == nil {
			return false
		}
		if n.Kind == ast.KindAwaitExpression {
			ambiguous = true
			return true
		}
		return n.ForEachChild(visit)
	}
	file.AsNode().ForEachChild(visit)
	for _, statement := range file.Statements.Nodes {
		switch statement.Kind {
		case ast.KindVariableStatement:
			list := statement.AsVariableStatement().DeclarationList.AsVariableDeclarationList()
			if list.Flags&ast.NodeFlagsBlockScoped != 0 {
				for _, d := range list.Declarations.Nodes {
					if d.Name().Kind != ast.KindIdentifier {
						ambiguous = true
						continue
					}
					switch d.Name().Text() {
					case "require", "module", "exports", "__dirname", "__filename":
						ambiguous = true
					}
				}
			}
		case ast.KindClassDeclaration:
			if statement.Name() != nil {
				switch statement.Name().Text() {
				case "require", "module", "exports", "__dirname", "__filename":
					ambiguous = true
				}
			}
		}
	}
	if ambiguous {
		return "", fmt.Errorf("source format await/wrapper-binding syntax requires further proof")
	}
	return "commonjs", nil
}
