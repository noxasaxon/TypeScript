package extract

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/tsox/graph"
)

func (b *builder) closedValueProperty(node *ast.Node, receiver *graph.Expression) (*graph.Expression, *fenceError) {
	data := node.AsPropertyAccessExpression()
	if receiver.Type.Optional || data.QuestionDotToken != nil || node.Flags&ast.NodeFlagsOptionalChain != 0 {
		return nil, b.fenceWithMessage(node, "closed optional receiver requires a presence contract")
	}
	shape, ok := b.shapeByID(receiver.Type.Shape)
	if !ok || len(shape.Alternatives) == 0 {
		return nil, b.fenceWithMessage(node, "closed read has no declared alternatives")
	}
	name := data.Name().Text()
	var typ *graph.Type
	for _, id := range shape.Alternatives {
		field, ok := b.shapeField(id, name)
		if !ok {
			continue
		}
		if typ != nil && !sameType(*typ, field.Type) {
			return nil, b.fenceWithMessage(node, "closed read has incompatible alternative field storage")
		}
		value := field.Type
		typ = &value
	}
	if typ == nil {
		return nil, b.fenceWithMessage(node, "closed read names no declared alternative field")
	}
	kind := graph.ExpressionClosedProperty
	if name == shape.Discriminant {
		kind = graph.ExpressionClosedTag
	}
	return &graph.Expression{Kind: kind, Position: b.position(node), Type: *typ, Receiver: receiver, Name: name}, nil
}
