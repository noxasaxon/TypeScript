package extract

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/tsox/graph"
)

func (b *builder) conditionalExpression(node *ast.Node) (*graph.Expression, *fenceError) {
	c := node.AsConditionalExpression()
	condition, fence := b.expression(c.Condition)
	if fence != nil {
		return nil, fence
	}
	if condition.Type.Kind != graph.TypeBoolean || condition.Type.Optional {
		return nil, b.fenceDiagnostic(c.Condition, "ConditionalCondition", "conditional requires an actual boolean condition")
	}
	var result graph.Type
	contextual := b.checker.GetContextualType(node, checker.ContextFlagsNone)
	if contextual != nil {
		result, fence = b.graphType(contextual, node)
	}
	if contextual == nil || fence != nil || (result.Kind != graph.TypeNumber && result.Kind != graph.TypeString && result.Kind != graph.TypeBoolean && result.Kind != graph.TypeObject && result.Kind != graph.TypeClosedUnion && result.Kind != graph.TypeArray) {
		result, fence = b.checkedType(node)
	}
	if fence != nil {
		return nil, fence
	}
	result.Optional = checkerTypeIncludesUndefined(b.checker.GetTypeAtLocation(node))
	switch result.Kind {
	case graph.TypeNumber, graph.TypeString, graph.TypeBoolean, graph.TypeObject, graph.TypeArray, graph.TypeClosedUnion:
	default:
		return nil, b.fenceDiagnostic(node, "ConditionalResult", "conditional result requires a supported common value domain")
	}
	yes, fence := b.expressionForSlot(c.WhenTrue, result)
	if fence != nil {
		return nil, fence
	}
	no, fence := b.expressionForSlot(c.WhenFalse, result)
	if fence != nil {
		return nil, fence
	}
	if !slotAccepts(result, yes) {
		return nil, b.typeFlowFence(c.WhenTrue, result, yes.Type)
	}
	if !slotAccepts(result, no) {
		return nil, b.typeFlowFence(c.WhenFalse, result, no.Type)
	}
	adaptUndefinedToSlot(result, yes)
	adaptUndefinedToSlot(result, no)
	return &graph.Expression{Kind: graph.ExpressionConditional, Position: b.position(node), Type: result, Operand: condition, Left: yes, Right: no}, nil
}
