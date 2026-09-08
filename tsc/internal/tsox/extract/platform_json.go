package extract

import (
	"fmt"
	"unicode/utf8"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// A fresh data literal supplies its own ordinary storage layout. This never
// narrows unknown data, attributes a producer, or skips property evaluation.
func (b *builder) platformJSONData(node *ast.Node) (*graph.Expression, *fenceError) {
	if node.Kind != ast.KindObjectLiteralExpression {
		return b.expression(node)
	}
	if f := b.objectLiteralPrototypeFence(node); f != nil {
		return nil, f
	}
	var fields []graph.Field
	var properties []graph.PropertyValue
	seen := map[string]bool{}
	for _, item := range node.AsObjectLiteralExpression().Properties.Nodes {
		var name, initializer *ast.Node
		switch item.Kind {
		case ast.KindPropertyAssignment:
			p := item.AsPropertyAssignment()
			name, initializer = p.Name(), p.Initializer
		case ast.KindShorthandPropertyAssignment:
			p := item.AsShorthandPropertyAssignment()
			if p.ObjectAssignmentInitializer != nil {
				return nil, b.fence(item)
			}
			name, initializer = p.Name(), p.Name()
		default:
			return nil, b.fenceDiagnostic(item, "PlatformJSONData", "fresh JSON data requires explicit data properties")
		}
		if name == nil || (name.Kind != ast.KindIdentifier && name.Kind != ast.KindStringLiteral) || !utf8.ValidString(name.Text()) || seen[name.Text()] {
			return nil, b.fenceDiagnostic(item, "PlatformJSONData", "fresh JSON data requires unique static scalar-valid property names")
		}
		seen[name.Text()] = true
		value, f := b.platformJSONData(initializer)
		if f != nil {
			return nil, f
		}
		switch value.Type.Kind {
		case graph.TypeString, graph.TypeNumber, graph.TypeBoolean, graph.TypeObject, graph.TypeArray:
		default:
			return nil, b.fenceDiagnostic(initializer, "PlatformJSONData", "data property requires its ordinary serializable value layout")
		}
		fields = append(fields, graph.Field{Position: b.position(item), Name: name.Text(), Type: value.Type})
		properties = append(properties, graph.PropertyValue{Position: b.position(item), Name: name.Text(), Value: value})
	}
	if b.jsonShapeIDs == nil {
		b.jsonShapeIDs = map[*checker.Type]graph.ShapeID{}
		b.jsonShapeBuilding = map[*checker.Type]bool{}
	}
	typ := b.checker.GetTypeAtLocation(node)
	id, exists := b.jsonShapeIDs[typ]
	if !exists {
		id = graph.ShapeID(len(b.shapeIDs) + len(b.jsonShapeIDs) + 1)
		b.jsonShapeIDs[typ] = id
		b.shapes = append(b.shapes, graph.Shape{ID: id, Position: b.position(node), Name: fmt.Sprintf("Data%d", id), Fields: fields})
	}
	return &graph.Expression{Kind: graph.ExpressionObject, Position: b.position(node), Type: graph.Type{Kind: graph.TypeObject, Shape: id}, Properties: properties}, nil
}
