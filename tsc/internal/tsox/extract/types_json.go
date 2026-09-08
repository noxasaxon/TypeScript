package extract

import (
	"unicode/utf8"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/internal/jsnum"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// JSON layouts are keyed by the actual instantiated checker type. An alias
// symbol alone cannot distinguish Result<A> from Result<B>. This describes
// storage obligations only; it never converts unknown input into that layout.
func (b *builder) jsonLayoutType(value *checker.Type, node *ast.Node) (graph.Type, *fenceError, bool) {
	flags := value.Flags()
	union := flags&checker.TypeFlagsUnion != 0
	if union {
		for _, part := range value.Types() {
			if part.Flags()&checker.TypeFlagsObject == 0 {
				return graph.Type{}, nil, false
			}
		}
	} else if flags&checker.TypeFlagsObject == 0 || b.checker.IsArrayType(value) || len(b.checker.GetSignaturesOfType(value, checker.SignatureKindCall)) != 0 {
		return graph.Type{}, nil, false
	}
	if b.jsonShapeIDs == nil {
		b.jsonShapeIDs = map[*checker.Type]graph.ShapeID{}
		b.jsonShapeBuilding = map[*checker.Type]bool{}
	}
	kind := graph.TypeObject
	if union {
		kind = graph.TypeClosedUnion
	}
	if id, ok := b.jsonShapeIDs[value]; ok {
		if b.jsonShapeBuilding[value] {
			return graph.Type{}, b.fenceWithMessage(node, "unsupported recursive closed layout"), true
		}
		return graph.Type{Kind: kind, Shape: id}, nil, true
	}
	id := graph.ShapeID(len(b.shapeIDs) + len(b.jsonShapeIDs) + 1)
	b.jsonShapeIDs[value] = id
	b.jsonShapeBuilding[value] = true
	shape := graph.Shape{ID: id, Position: b.position(node), Name: b.checker.TypeToString(value)}
	if union {
		for _, part := range value.Types() {
			typ, fence := b.graphType(part, node)
			if fence != nil {
				return graph.Type{}, fence, true
			}
			if typ.Kind != graph.TypeObject || typ.Optional {
				return graph.Type{}, b.fenceWithMessage(node, "closed alternatives require record layouts"), true
			}
			shape.Alternatives = append(shape.Alternatives, typ.Shape)
		}
		first, _ := b.shapeByID(shape.Alternatives[0])
		for _, field := range first.Fields {
			if field.Literal == nil || field.Type.Optional {
				continue
			}
			seen := []*graph.Literal{}
			unique := true
			for _, alternative := range shape.Alternatives {
				item, ok := b.shapeField(alternative, field.Name)
				if !ok || item.Literal == nil || item.Type.Optional {
					unique = false
					break
				}
				for _, literal := range seen {
					if *literal == *item.Literal {
						unique = false
					}
				}
				seen = append(seen, item.Literal)
			}
			if unique {
				shape.Discriminant = field.Name
				break
			}
		}
		if shape.Discriminant == "" {
			return graph.Type{}, b.fenceWithMessage(node, "closed union needs distinct required literal discriminants"), true
		}
	} else {
		if len(b.checker.GetIndexInfosOfType(value)) != 0 {
			return graph.Type{}, b.fenceWithMessage(node, "closed layout cannot contain index signatures"), true
		}
		for _, property := range b.checker.GetPropertiesOfType(value) {
			if len(property.Declarations) != 1 {
				return graph.Type{}, b.fenceWithMessage(node, "closed field needs one source declaration"), true
			}
			declaration := property.Declarations[0]
			if !b.ownsFile(ast.GetSourceFileOfNode(declaration)) || declaration.Kind != ast.KindPropertySignature {
				return graph.Type{}, b.fenceWithMessage(declaration, "closed layout fields require source property signatures"), true
			}
			member := declaration.AsPropertySignatureDeclaration()
			if member.Name() == nil || member.Name().Kind != ast.KindIdentifier || member.Initializer != nil || member.Type == nil {
				return graph.Type{}, b.fence(declaration), true
			}
			actual := b.checker.GetTypeOfPropertyOfType(value, property.Name)
			typ, fence := b.graphType(actual, declaration)
			if fence != nil {
				return graph.Type{}, fence, true
			}
			if property.Flags&ast.SymbolFlagsOptional != 0 {
				typ.Optional = true
			}
			shape.Fields = append(shape.Fields, graph.Field{Position: b.position(declaration), Name: property.Name, Type: typ, Literal: jsonTypeLiteral(actual)})
		}
	}
	b.jsonShapeBuilding[value] = false
	b.shapes = append(b.shapes, shape)
	return graph.Type{Kind: kind, Shape: id}, nil, true
}

func jsonTypeLiteral(value *checker.Type) *graph.Literal {
	switch {
	case value.Flags()&checker.TypeFlagsBooleanLiteral != 0:
		return &graph.Literal{Kind: graph.TypeBoolean, Boolean: value.AsLiteralType().Value().(bool)}
	case value.Flags()&checker.TypeFlagsStringLiteral != 0:
		text := value.AsLiteralType().Value().(string)
		if utf8.ValidString(text) {
			return &graph.Literal{Kind: graph.TypeString, String: text}
		}
	case value.Flags()&checker.TypeFlagsNumberLiteral != 0:
		return &graph.Literal{Kind: graph.TypeNumber, Number: float64(value.AsLiteralType().Value().(jsnum.Number))}
	}
	return nil
}

func matchesJSONLiteral(value *graph.Expression, literal *graph.Literal) bool {
	if value == nil || literal == nil {
		return false
	}
	switch literal.Kind {
	case graph.TypeBoolean:
		return value.Kind == graph.ExpressionBoolean && value.Boolean == literal.Boolean
	case graph.TypeString:
		return value.Kind == graph.ExpressionString && value.StringUnits == nil && value.String == literal.String
	case graph.TypeNumber:
		return value.Kind == graph.ExpressionNumber && value.Number == literal.Number
	}
	return false
}

func (b *builder) jsonObjectLiteral(node *ast.Node, typ graph.Type) (*graph.Expression, *fenceError) {
	shape, ok := b.shapeByID(typ.Shape)
	if !ok {
		return nil, b.fenceWithMessage(node, "closed literal layout was not registered")
	}
	if typ.Kind == graph.TypeClosedUnion {
		var tag *ast.Node
		for _, property := range node.AsObjectLiteralExpression().Properties.Nodes {
			if property.Kind == ast.KindPropertyAssignment && property.Name() != nil && property.Name().Text() == shape.Discriminant {
				tag = property.AsPropertyAssignment().Initializer
			}
		}
		if tag == nil {
			return nil, b.fenceWithMessage(node, "closed construction requires an explicit literal discriminant")
		}
		value, fence := b.expression(tag)
		if fence != nil {
			return nil, fence
		}
		for _, id := range shape.Alternatives {
			field, _ := b.shapeField(id, shape.Discriminant)
			if matchesJSONLiteral(value, field.Literal) {
				payload, fence := b.jsonObjectLiteral(node, graph.Type{Kind: graph.TypeObject, Shape: id})
				if fence != nil {
					return nil, fence
				}
				return &graph.Expression{Kind: graph.ExpressionClosedValue, Position: b.position(node), Type: typ, Operand: payload}, nil
			}
		}
		return nil, b.fenceWithMessage(tag, "closed construction discriminant does not select a declared alternative")
	}
	if typ.Kind != graph.TypeObject {
		return nil, b.fenceWithMessage(node, "object literal requires a closed record layout")
	}
	fields := map[string]graph.Field{}
	for _, f := range shape.Fields {
		fields[f.Name] = f
	}
	seen := map[string]bool{}
	values := []graph.PropertyValue{}
	for _, property := range node.AsObjectLiteralExpression().Properties.Nodes {
		var name, source *ast.Node
		switch property.Kind {
		case ast.KindPropertyAssignment:
			name = property.Name()
			source = property.AsPropertyAssignment().Initializer
		case ast.KindShorthandPropertyAssignment:
			name = property.Name()
			source = name
		default:
			return nil, b.fence(property)
		}
		if name == nil || name.Kind != ast.KindIdentifier {
			return nil, b.fence(property)
		}
		if name.Text() == "__proto__" {
			return nil, b.fenceWithMessage(property, "closed construction requires explicit prototype versus data-property semantics")
		}
		field, ok := fields[name.Text()]
		if !ok || seen[field.Name] {
			return nil, b.fenceWithMessage(property, "closed construction has an unknown or duplicate field")
		}
		seen[field.Name] = true
		value, fence := b.expressionForSlot(source, field.Type)
		if fence != nil {
			return nil, fence
		}
		if !slotAccepts(field.Type, value) {
			return nil, b.typeFlowFence(source, field.Type, value.Type)
		}
		if field.Literal != nil && !matchesJSONLiteral(value, field.Literal) {
			return nil, b.fenceWithMessage(source, "closed literal field requires its actual literal value")
		}
		adaptUndefinedToSlot(field.Type, value)
		values = append(values, graph.PropertyValue{Position: b.position(property), Name: field.Name, Value: value})
	}
	for _, field := range shape.Fields {
		if seen[field.Name] {
			continue
		}
		if !field.Type.Optional {
			return nil, b.fenceWithMessage(node, "closed construction omitted a required field")
		}
		values = append(values, graph.PropertyValue{Position: b.position(node), Name: field.Name, Omitted: true, Value: &graph.Expression{Kind: graph.ExpressionUndefined, Position: b.position(node), Type: field.Type}})
	}
	return &graph.Expression{Kind: graph.ExpressionObject, Position: b.position(node), Type: typ, Properties: values}, nil
}
