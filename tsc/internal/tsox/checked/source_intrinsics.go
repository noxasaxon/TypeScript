package checked

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/bundled"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// SourceIntrinsicCall is an original syntax/symbol observation. Invariant,
// operand domains and effects remain proof obligations, not fields of this view.
type SourceIntrinsicCall struct {
	Site      graph.SourceSite
	Arguments []graph.SourceSite
}

func (s *SourceRecoveryScope) OriginalConsoleOutput(source ScopedSourceHandle) (SourceIntrinsicCall, error) {
	if _, err := s.SourceSite(source); err != nil {
		return SourceIntrinsicCall{}, err
	}
	node := source.node
	if node.Kind == ast.KindExpressionStatement {
		node = node.AsExpressionStatement().Expression
	}
	fail := func() (SourceIntrinsicCall, error) {
		return SourceIntrinsicCall{}, fmt.Errorf("original bundled console.log call required")
	}
	if node.Kind != ast.KindCallExpression {
		return fail()
	}
	call := node.AsCallExpression()
	if call.QuestionDotToken != nil || call.TypeArguments != nil || call.Expression.Kind != ast.KindPropertyAccessExpression {
		return fail()
	}
	member := call.Expression.AsPropertyAccessExpression()
	if member.QuestionDotToken != nil || member.Expression.Kind != ast.KindIdentifier || member.Expression.Text() != "console" || member.Name().Text() != "log" {
		return fail()
	}
	c, done := s.snapshot.Program.Compiler.GetTypeChecker(context.Background())
	defer done()
	global := c.GetGlobalSymbol("console", ast.SymbolFlagsValue, nil)
	owner := c.GetGlobalSymbol("Console", ast.SymbolFlagsType, nil)
	if !sourceBundledIntrinsicSymbol(global) || !sourceBundledIntrinsicSymbol(owner) || c.GetSymbolAtLocation(member.Expression) != global {
		return fail()
	}
	target := c.GetPropertyOfType(c.GetDeclaredTypeOfSymbol(owner), "log")
	if !sourceBundledIntrinsicSymbol(target) || c.GetSymbolAtLocation(member.Name()) != target {
		return fail()
	}
	result := SourceIntrinsicCall{Site: s.site(node)}
	if call.Arguments != nil {
		for _, arg := range call.Arguments.Nodes {
			result.Arguments = append(result.Arguments, s.site(arg))
		}
	}
	return result, nil
}
func sourceBundledIntrinsicSymbol(symbol *ast.Symbol) bool {
	if symbol == nil || len(symbol.Declarations) == 0 {
		return false
	}
	for _, decl := range symbol.Declarations {
		file := ast.GetSourceFileOfNode(decl)
		if file == nil || !file.IsDeclarationFile {
			return false
		}
		name := path.Clean(file.FileName())
		if path.Dir(name) != path.Clean(bundled.LibPath()) || !strings.HasPrefix(path.Base(name), "lib.") || !strings.HasSuffix(name, ".d.ts") {
			return false
		}
	}
	return true
}
