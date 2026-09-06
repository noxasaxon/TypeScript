package extract

import (
	"context"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
)

func ExtractFiles(entry string, sources map[string]string) graph.Result {
	program, diagnostics := checked.New(entry, sources)
	if len(diagnostics) != 0 {
		return graph.Result{Diagnostics: diagnostics}
	}
	typeChecker, done := program.Compiler.GetTypeChecker(context.Background())
	defer done()
	b := &builder{sourcePath: entry, file: program.Entry, checker: typeChecker,
		bindings: make(map[*ast.Symbol]graph.BindingID), bindingTypes: make(map[graph.BindingID]graph.Type), nextBinding: 1,
		shapeIDs: make(map[*ast.Symbol]graph.ShapeID), shapeBuilding: make(map[*ast.Symbol]bool), moduleFiles: program.Files, entryFile: program.Entry}
	var statements []*graph.Statement
	for _, file := range program.RuntimeFiles {
		b.file = file
		body, fence := b.statements(file.Statements.Nodes, true)
		if fence != nil {
			return diagnosticResult(fence.diagnostic)
		}
		statements = append(statements, body...)
	}
	return graph.Result{Program: &graph.Program{SourcePath: entry, Shapes: b.shapes, Statements: statements}}
}

func (b *builder) sourceSymbol(node *ast.Node) *ast.Symbol {
	symbol := b.checker.GetSymbolAtLocation(node)
	if b.moduleFiles != nil && symbol != nil && symbol.Flags&ast.SymbolFlagsAlias != 0 {
		return b.checker.GetAliasedSymbol(symbol)
	}
	return symbol
}

func (b *builder) ownsFile(file *ast.SourceFile) bool {
	if b.moduleFiles == nil {
		return file == b.file
	}
	_, ok := b.moduleFiles[file]
	return ok
}

func (b *builder) declarationModifiers(modifiers *ast.ModifierList) bool {
	if modifiers == nil {
		return true
	}
	return b.moduleFiles != nil && len(modifiers.Nodes) == 1 && modifiers.Nodes[0].Kind == ast.KindExportKeyword
}
