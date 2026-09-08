package extract

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// loopStatement shares header evaluation and binding construction between
// synchronous bodies and async bodies whose suspensions are extracted separately.
func (b *builder) loopStatement(node *ast.Node, extractBody func(*ast.Node) ([]*graph.Statement, *fenceError)) ([]*graph.Statement, *fenceError) {
	switch node.Kind {
	case ast.KindWhileStatement:
		data := node.AsWhileStatement()
		condition, fence := b.booleanExpression(data.Expression)
		if fence != nil {
			return nil, fence
		}
		body, fence := extractBody(data.Statement)
		if fence != nil {
			return nil, fence
		}
		return []*graph.Statement{{
			Kind:      graph.StatementWhile,
			Position:  b.position(node),
			Condition: condition,
			Body:      body,
		}}, nil

	case ast.KindForStatement:
		data := node.AsForStatement()
		var init []*graph.Statement
		var fence *fenceError
		if data.Initializer != nil {
			switch data.Initializer.Kind {
			case ast.KindVariableDeclarationList:
				init, fence = b.variableDeclarations(data.Initializer)
			default:
				var expression *graph.Expression
				expression, fence = b.expression(data.Initializer)
				if fence == nil {
					init = []*graph.Statement{{
						Kind:     graph.StatementExpression,
						Position: b.position(data.Initializer),
						Value:    expression,
					}}
				}
			}
			if fence != nil {
				return nil, fence
			}
		}
		var condition *graph.Expression
		if data.Condition != nil {
			condition, fence = b.booleanExpression(data.Condition)
			if fence != nil {
				return nil, fence
			}
		}
		var increment *graph.Expression
		if data.Incrementor != nil {
			increment, fence = b.expression(data.Incrementor)
			if fence != nil {
				return nil, fence
			}
		}
		body, fence := extractBody(data.Statement)
		if fence != nil {
			return nil, fence
		}
		return []*graph.Statement{{
			Kind:      graph.StatementFor,
			Position:  b.position(node),
			Init:      init,
			Condition: condition,
			Increment: increment,
			Body:      body,
		}}, nil

	}
	return nil, b.fence(node)
}
