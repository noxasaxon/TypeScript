package extract

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/tsox/graph"
)

func (b *builder) destructuringFence(node *ast.Node, reason string) *fenceError {
	return b.fenceDiagnostic(node, "ObjectDestructuring", "unsupported object destructuring: "+reason)
}

// Object bindings lower into ordinary ordered declarations. Only plain record
// fields participate: no getters, defaults or computed names can interleave
// effects with the reads. Existing property analysis then decides which field
// values are snapshots and which must retain observable composite identity.
func (b *builder) objectDestructuring(node *ast.Node, mutable bool) ([]*graph.Statement, *fenceError) {
	declaration := node.AsVariableDeclaration()
	if declaration.Initializer == nil || declaration.ExclamationToken != nil {
		return nil, b.destructuringFence(node, "an initialized const or let declaration is required")
	}
	elements := declaration.Name().AsBindingPattern().Elements.Nodes
	for _, elementNode := range elements {
		element := elementNode.AsBindingElement()
		switch {
		case element.DotDotDotToken != nil:
			return nil, b.destructuringFence(elementNode, "rest bindings are unsupported")
		case element.Initializer != nil:
			return nil, b.destructuringFence(elementNode, "default values are unsupported")
		case element.Name() == nil || element.Name().Kind != ast.KindIdentifier:
			return nil, b.destructuringFence(elementNode, "nested binding patterns are unsupported")
		case element.PropertyName != nil && element.PropertyName.Kind != ast.KindIdentifier:
			return nil, b.destructuringFence(element.PropertyName, "property names must be static identifiers; computed and literal names are unsupported")
		}
	}
	if declaration.Type != nil {
		if fence := b.validateTypeNode(declaration.Type); fence != nil {
			return nil, fence
		}
	}
	// Contextual typing may give a fresh literal its named shape. Never feed the
	// pattern annotation to an existing source: its actual shape is its identity.
	source, fence := b.expression(declaration.Initializer)
	if fence != nil {
		return nil, fence
	}
	b.narrowOptionalChainUse(declaration.Initializer, source)
	if source.Type.Kind != graph.TypeObject || source.Type.Optional {
		return nil, b.destructuringFence(declaration.Initializer, "the source must be a non-optional named plain record")
	}
	unprovedPrototype := false
	walkGraphExpressions([]*graph.Statement{{Value: source}}, func(value *graph.Expression) {
		// A __proto__ initializer can make another omitted field inherited,
		// including a receiver field used to obtain the destructuring source.
		// Reject whole shapes; harmless instances are not distinguished here.
		if value.Type.Kind == graph.TypeObject {
			if _, hasPrototype := b.shapeField(value.Type.Shape, "__proto__"); hasPrototype {
				unprovedPrototype = true
			}
		}
	})
	if unprovedPrototype {
		return nil, b.destructuringFence(declaration.Initializer, "a source expression shape contains __proto__; prototype lookup is outside the plain-record proof")
	}
	if len(elements) == 0 {
		return []*graph.Statement{{Kind: graph.StatementExpression, Position: b.position(node), Value: source}}, nil
	}
	result := make([]*graph.Statement, 0, len(elements)+1)
	if declaration.Initializer.Kind != ast.KindIdentifier {
		binding := b.nextBinding
		b.nextBinding++
		b.bindingTypes[binding] = source.Type
		result = append(result, &graph.Statement{Kind: graph.StatementVariable, Position: b.position(node), Binding: binding, Name: "destructuring_source", Type: source.Type, Value: source})
		source = &graph.Expression{Kind: graph.ExpressionIdentifier, Position: b.position(declaration.Initializer), Binding: binding, Name: "destructuring_source", Type: source.Type}
	}
	for _, elementNode := range elements {
		element := elementNode.AsBindingElement()
		name := element.Name()
		property := name
		if element.PropertyName != nil {
			property = element.PropertyName
		}
		field, ok := b.shapeField(source.Type.Shape, property.Text())
		if !ok {
			return nil, b.destructuringFence(property, "property is not declared on the source's named shape")
		}
		valueType, fence := b.checkedType(name)
		if fence != nil {
			return nil, fence
		}
		value := &graph.Expression{Kind: graph.ExpressionProperty, Position: b.position(elementNode), Type: field.Type, Receiver: source, Name: property.Text()}
		if !slotAccepts(valueType, value) {
			return nil, b.typeFlowFence(elementNode, valueType, field.Type)
		}
		binding, fence := b.binding(name)
		if fence != nil {
			return nil, fence
		}
		b.bindingTypes[binding] = valueType
		result = append(result, &graph.Statement{Kind: graph.StatementVariable, Position: b.position(elementNode), Binding: binding, Name: name.Text(), Mutable: mutable, Type: valueType, Value: value})
	}
	return result, nil
}
