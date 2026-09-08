package extract

import (
	"github.com/microsoft/typescript-go/internal/ast"
)

type CJSBodyOperation struct {
	Source                    SourceBodyNode
	Kind, Text, CheckedType   string
	Operands                  []SourceBodyNode
	ResolvedDeclarations      []SourceBodyNode
	NeedsEffectAndDomainProof bool
}

// Syntax/checker observations only, not a second executable flow or callee proof.
func (b *builder) cjsBodyOperation(node *ast.Node) *CJSBodyOperation {
	var operands []*ast.Node
	target := node
	typed := true
	switch node.Kind {
	case ast.KindCallExpression:
		x := node.AsCallExpression()
		target = x.Expression
		operands = append(operands, target)
		if x.Arguments != nil {
			operands = append(operands, x.Arguments.Nodes...)
		}
	case ast.KindNewExpression:
		x := node.AsNewExpression()
		target = x.Expression
		operands = append(operands, target)
		if x.Arguments != nil {
			operands = append(operands, x.Arguments.Nodes...)
		}
	case ast.KindPropertyAccessExpression:
		x := node.AsPropertyAccessExpression()
		operands = append(operands, x.Expression)
		target = x.Name()
	case ast.KindElementAccessExpression:
		x := node.AsElementAccessExpression()
		operands = append(operands, x.Expression, x.ArgumentExpression)
	case ast.KindBinaryExpression:
		x := node.AsBinaryExpression()
		operands = append(operands, x.Left, x.Right)
	case ast.KindConditionalExpression:
		x := node.AsConditionalExpression()
		operands = append(operands, x.Condition, x.WhenTrue, x.WhenFalse)
	case ast.KindPrefixUnaryExpression:
		operands = append(operands, node.AsPrefixUnaryExpression().Operand)
	case ast.KindPostfixUnaryExpression:
		operands = append(operands, node.AsPostfixUnaryExpression().Operand)
	case ast.KindRegularExpressionLiteral:
	case ast.KindDoStatement, ast.KindWhileStatement, ast.KindForStatement, ast.KindSwitchStatement, ast.KindBreakStatement, ast.KindContinueStatement, ast.KindTryStatement, ast.KindThrowStatement, ast.KindVariableDeclarationList:
		typed = false
	default:
		return nil
	}
	file := ast.GetSourceFileOfNode(node)
	out := &CJSBodyOperation{Source: b.sourceBodyNode(node), Kind: node.Kind.String(), Text: file.Text()[node.Pos():node.End()], NeedsEffectAndDomainProof: true}
	if typed {
		out.CheckedType = b.checker.TypeToString(b.checker.GetTypeAtLocation(node))
	}
	for _, operand := range operands {
		out.Operands = append(out.Operands, b.sourceBodyNode(operand))
	}
	if typed {
		if symbol := b.checker.GetSymbolAtLocation(target); symbol != nil {
			for _, declaration := range symbol.Declarations {
				out.ResolvedDeclarations = append(out.ResolvedDeclarations, b.sourceBodyNode(declaration))
			}
		}
	}
	return out
}
