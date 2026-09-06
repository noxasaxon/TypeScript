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
		if fence := b.checkJSONCalls(file.AsNode()); fence != nil {
			return diagnosticResult(fence.diagnostic)
		}
	}
	for _, file := range program.RuntimeFiles {
		b.file = file
		body, fence := b.statements(file.Statements.Nodes, true)
		if fence != nil {
			return diagnosticResult(fence.diagnostic)
		}
		statements = append(statements, body...)
	}
	return b.finishJSON(&graph.Program{SourcePath: entry, Shapes: b.shapes, Statements: statements, EntryExports: b.entryExports()})
}

// entryExports runs after extraction so every supported runtime declaration
// already has its original binding. Type exports never become host aliases.
func (b *builder) entryExports() []graph.Export {
	var exports []graph.Export
	add := func(name *ast.Node, symbol *ast.Symbol) {
		if symbol != nil && symbol.Flags&ast.SymbolFlagsAlias != 0 {
			symbol = b.checker.GetAliasedSymbol(symbol)
		}
		if binding, ok := b.bindings[symbol]; ok {
			exports = append(exports, graph.Export{Name: name.Text(), Binding: binding, Position: b.position(name)})
		}
	}
	var declarationName func(*ast.Node)
	declarationName = func(name *ast.Node) {
		if name.Kind == ast.KindIdentifier {
			add(name, b.sourceSymbol(name))
			return
		}
		if name.Kind == ast.KindObjectBindingPattern {
			for _, element := range name.AsBindingPattern().Elements.Nodes {
				declarationName(element.Name())
			}
		}
	}
	for _, statement := range b.entryFile.Statements.Nodes {
		if statement.Kind == ast.KindExportDeclaration {
			declaration := statement.AsExportDeclaration()
			if declaration.IsTypeOnly {
				continue
			}
			for _, node := range declaration.ExportClause.AsNamedExports().Elements.Nodes {
				specifier := node.AsExportSpecifier()
				if !specifier.IsTypeOnly {
					add(specifier.Name(), b.checker.GetExportSpecifierLocalTargetSymbol(node))
				}
			}
			continue
		}
		modifiers := statement.Modifiers()
		if modifiers == nil || len(modifiers.Nodes) != 1 || modifiers.Nodes[0].Kind != ast.KindExportKeyword {
			continue
		}
		switch statement.Kind {
		case ast.KindFunctionDeclaration:
			declarationName(statement.Name())
		case ast.KindVariableStatement:
			for _, declaration := range statement.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
				declarationName(declaration.Name())
			}
		}
	}
	return exports
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
