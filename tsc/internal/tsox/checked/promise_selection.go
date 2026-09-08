package checked

import (
	"context"
	"fmt"

	"github.com/microsoft/typescript-go/internal/ast"
)

// SelectSourcePromiseAnalysis only chooses an analysis path from the captured
// original syntax and symbols. It supplies no native intrinsic/value authority
// and never reacts to an ordinary extraction failure.
func (p *Project) SelectSourcePromiseAnalysis(export string) (bool, error) {
	if p == nil || p.dependency == nil {
		return false, fmt.Errorf("captured project required")
	}
	if err := p.sourceSyntax.validate(p.dependency.Program); err != nil {
		return false, err
	}
	program := p.dependency.Program
	file := program.GetSourceFile(p.Entry)
	if file == nil {
		return false, fmt.Errorf("captured entry required")
	}
	c, done := program.GetTypeChecker(context.Background())
	defer done()
	module := file.AsNode().Symbol()
	if module == nil {
		return false, nil
	}
	resolve := func(node *ast.Node) *ast.Symbol {
		symbol := lexicalSymbol(node, c)
		if symbol != nil && symbol.Flags&ast.SymbolFlagsAlias != 0 {
			symbol = c.GetAliasedSymbol(symbol)
		}
		return symbol
	}
	var entry *ast.Node
	for _, symbol := range c.GetExportsOfModule(module) {
		if ast.SymbolName(symbol) == export {
			if symbol.Flags&ast.SymbolFlagsAlias != 0 {
				symbol = c.GetAliasedSymbol(symbol)
			}
			if symbol != nil && len(symbol.Declarations) == 1 {
				entry = symbol.Declarations[0]
			}
			break
		}
	}
	if entry == nil || entry.Kind != ast.KindFunctionDeclaration || entry.Body() == nil {
		return false, nil
	}
	selected := false
	seen := map[*ast.Node]bool{}
	var body func(*ast.Node)
	body = func(fn *ast.Node) {
		if seen[fn] {
			return
		}
		seen[fn] = true
		var visit func(*ast.Node) bool
		visit = func(node *ast.Node) bool {
			if node != fn.Body() && (node.Kind == ast.KindFunctionDeclaration || node.Kind == ast.KindFunctionExpression || node.Kind == ast.KindArrowFunction) {
				return false
			}
			if node.Kind == ast.KindCallExpression {
				call := node.AsCallExpression()
				symbol := resolve(call.Expression)
				if symbol != nil && len(symbol.Declarations) == 1 {
					target := symbol.Declarations[0]
					targetFile := ast.GetSourceFileOfNode(target)
					if target.Kind == ast.KindFunctionDeclaration && target.Body() != nil && targetFile != nil && !targetFile.IsDeclarationFile && program.GetSourceFile(targetFile.FileName()) == targetFile {
						async := false
						if modifiers := target.Modifiers(); modifiers != nil {
							for _, m := range modifiers.Nodes {
								if m.Kind == ast.KindAsyncKeyword {
									async = true
								}
							}
						}
						if async {
							parent := node.Parent
							for parent != nil && parent.Kind == ast.KindParenthesizedExpression {
								parent = parent.Parent
							}
							if parent == nil || parent.Kind != ast.KindAwaitExpression {
								selected = true
							}
							body(target)
						}
					}
				}
				// Exact builtin authority is discharged later. Here only actual bundled
				// declarations can select the new Promise intrinsic analysis.
				if call.Expression.Kind == ast.KindPropertyAccessExpression {
					property := call.Expression.AsPropertyAccessExpression()
					if property.Name().Kind == ast.KindIdentifier && property.Name().Text() == "all" {
						member := resolve(property.Name())
						if sourceBundledIntrinsicSymbol(member) {
							receiver := resolve(property.Expression)
							global := c.GetGlobalSymbol("Promise", ast.SymbolFlagsValue, nil)
							if receiver != nil && receiver == global && sourceBundledIntrinsicSymbol(global) {
								selected = true
							}
						}
					}
				}
			}
			return node.ForEachChild(visit)
		}
		fn.Body().ForEachChild(visit)
	}
	body(entry)
	return selected, nil
}
