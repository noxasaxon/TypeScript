package extract

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/tsox/graph"
)

func (b *builder) numberGlobal(node *ast.Node) bool {
	if node == nil || node.Kind != ast.KindIdentifier || node.Text() != "Number" {
		return false
	}
	global := b.checker.GetGlobalSymbol("Number", ast.SymbolFlagsValue, nil)
	return global != nil && b.checker.GetSymbolAtLocation(node) == global
}

func (b *builder) numericExpression(node *ast.Node) (*graph.Expression, *fenceError, bool) {
	if node.Kind == ast.KindNewExpression && b.numberGlobal(node.AsNewExpression().Expression) {
		return nil, b.fenceDiagnostic(node, "NumberBuiltin", "unsupported new Number: boxed number identity and coercion are outside the implemented primitive domain"), true
	}
	if node.Kind != ast.KindCallExpression {
		return nil, nil, false
	}
	call := node.AsCallExpression()
	kind := graph.ExpressionKind("")
	switch {
	case b.numberGlobal(call.Expression):
		kind = graph.ExpressionNumberConvert
	case b.libraryMemberCall(node, "Number", "isFinite"):
		kind = graph.ExpressionNumberIsFinite
	case b.libraryMemberCall(node, "Number", "isInteger"):
		kind = graph.ExpressionNumberIsInteger
	default:
		return nil, nil, false
	}
	if call.QuestionDotToken != nil || call.TypeArguments != nil || (call.Expression.Kind == ast.KindPropertyAccessExpression && call.Expression.AsPropertyAccessExpression().QuestionDotToken != nil) {
		return nil, b.fenceDiagnostic(node, "NumberBuiltin", "unsupported optional or generic Number builtin call"), true
	}
	arguments := make([]*graph.Expression, 0, len(call.Arguments.Nodes))
	for _, argument := range call.Arguments.Nodes {
		if argument.Kind == ast.KindSpreadElement {
			return nil, b.fenceDiagnostic(argument, "NumberBuiltin", "unsupported spread Number builtin argument"), true
		}
		var value *graph.Expression
		var fence *fenceError
		if argument.Kind == ast.KindNullKeyword {
			value = &graph.Expression{Kind: graph.ExpressionNull, Position: b.position(argument), Type: graph.Type{Kind: graph.TypeNull}}
		} else {
			value, fence = b.expression(argument)
		}
		if fence != nil {
			return nil, fence, true
		}
		arguments = append(arguments, value)
	}
	if kind == graph.ExpressionNumberConvert && len(arguments) != 0 {
		if b.jsonValues {
			// A narrowed use requests a primitive projection. The original
			// unknown storage survives inside the obligation, which checked
			// control-flow evidence must discharge before native emission.
			arguments[0] = unknownProjection(arguments[0], b.jsonProjectionType(call.Arguments.Nodes[0]))
		}
		first := arguments[0]
		switch first.Type.Kind {
		case graph.TypeNumber, graph.TypeString, graph.TypeBoolean, graph.TypeNull:
		default:
			if first.Kind != graph.ExpressionUndefined {
				return nil, b.fenceDiagnostic(node, "NumberBuiltin", "unsupported Number conversion input: implemented domain is number, string, boolean, null and undefined; object coercion and unknown input require separate evidence"), true
			}
		}
	}
	result := graph.Type{Kind: graph.TypeBoolean}
	if kind == graph.ExpressionNumberConvert {
		result.Kind = graph.TypeNumber
	}
	return &graph.Expression{Kind: kind, Position: b.position(node), Type: result, Arguments: arguments}, nil, true
}
