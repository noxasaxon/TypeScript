package extract

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/tsox/graph"
)

func (b *builder) errorIntrinsic(node *ast.Node) string {
	if node == nil || node.Kind != ast.KindIdentifier {
		return ""
	}
	switch node.Text() {
	case "Error", "TypeError", "RangeError", "ReferenceError", "SyntaxError", "EvalError", "URIError":
	default:
		return ""
	}
	global := b.checker.GetGlobalSymbol(node.Text(), ast.SymbolFlagsValue, nil)
	if global != nil && b.checker.GetSymbolAtLocation(node) == global {
		return node.Text()
	}
	return ""
}

// Avoid re-extracting arbitrary ordinary property chains while deciding whether
// this hook owns a receiver. Only actual selected storage or constructors qualify.
func (b *builder) errorReceiver(node *ast.Node) bool {
	if node == nil {
		return false
	}
	for node.Kind == ast.KindParenthesizedExpression {
		node = node.AsParenthesizedExpression().Expression
	}
	switch node.Kind {
	case ast.KindIdentifier:
		binding, exists := b.bindings[b.sourceSymbol(node)]
		if !exists {
			return false
		}
		kind := b.bindingTypes[binding].Kind
		return kind == graph.TypeError || kind == graph.TypeThrown
	case ast.KindCallExpression:
		return b.errorIntrinsic(node.AsCallExpression().Expression) != ""
	case ast.KindNewExpression:
		return b.errorIntrinsic(node.AsNewExpression().Expression) != ""
	}
	return false
}

func (b *builder) errorExpression(node *ast.Node) (*graph.Expression, *fenceError, bool) {
	if !b.asyncThrow {
		return nil, nil, false
	}
	if node.Kind == ast.KindIdentifier {
		symbol := b.sourceSymbol(node)
		if binding, exists := b.bindings[symbol]; exists {
			valueType := b.bindingTypes[binding]
			if valueType.Kind == graph.TypeError || valueType.Kind == graph.TypeThrown {
				return &graph.Expression{Kind: graph.ExpressionIdentifier, Position: b.position(node), Type: valueType, Binding: binding, Name: node.Text()}, nil, true
			}
		}
	}
	if node.Kind == ast.KindBinaryExpression && node.AsBinaryExpression().OperatorToken.Kind == ast.KindEqualsToken {
		data := node.AsBinaryExpression()
		if data.Left.Kind == ast.KindPropertyAccessExpression {
			property := data.Left.AsPropertyAccessExpression()
			if !b.errorReceiver(property.Expression) {
				return nil, nil, false
			}
			receiver, receiverFence := b.expression(property.Expression)
			if receiverFence == nil && (receiver.Type.Kind == graph.TypeError || receiver.Type.Kind == graph.TypeThrown) {
				if property.Name().Text() != "name" || property.QuestionDotToken != nil || receiver.Type.Kind != graph.TypeError || receiver.Kind != graph.ExpressionIdentifier {
					return nil, b.fenceDiagnostic(node, "ErrorWrite", "error mutation requires an owned ordinary intrinsic and implemented writable name"), true
				}
				value, fence := b.expression(data.Right)
				if fence != nil {
					return nil, fence, true
				}
				if value.Type.Kind != graph.TypeString || value.Type.Optional {
					return nil, b.fenceDiagnostic(data.Right, "ErrorWrite", "writable error name currently requires a string value"), true
				}
				left := &graph.Expression{Kind: graph.ExpressionErrorField, Position: b.position(data.Left), Type: value.Type, Name: "name", Receiver: receiver}
				return &graph.Expression{Kind: graph.ExpressionAssignment, Position: b.position(node), Type: value.Type, Operator: "=", Left: left, Right: value}, nil, true
			}
		}
	}
	if node.Kind == ast.KindBinaryExpression && node.AsBinaryExpression().OperatorToken.Kind == ast.KindInstanceOfKeyword {
		data := node.AsBinaryExpression()
		name := b.errorIntrinsic(data.Right)
		if name == "" {
			return nil, nil, false
		}
		receiver, fence := b.expression(data.Left)
		if fence != nil {
			return nil, fence, true
		}
		switch receiver.Type.Kind {
		case graph.TypeError, graph.TypeThrown, graph.TypeString, graph.TypeNumber, graph.TypeBoolean:
		default:
			return nil, b.fenceDiagnostic(node, "ErrorInstanceOf", "intrinsic membership requires an implemented source value domain"), true
		}
		return &graph.Expression{Kind: graph.ExpressionErrorInstanceOf, Position: b.position(node), Type: graph.Type{Kind: graph.TypeBoolean}, Name: name, Receiver: receiver}, nil, true
	}
	if node.Kind == ast.KindPropertyAccessExpression {
		data := node.AsPropertyAccessExpression()
		if !b.errorReceiver(data.Expression) {
			return nil, nil, false
		}
		receiver, fence := b.expression(data.Expression)
		if fence == nil && (receiver.Type.Kind == graph.TypeError || receiver.Type.Kind == graph.TypeThrown) {
			if data.QuestionDotToken != nil || (data.Name().Text() != "name" && data.Name().Text() != "message") {
				return nil, b.fenceDiagnostic(node, "ErrorProperty", "source error property requires separate storage and observation proof"), true
			}
			return &graph.Expression{Kind: graph.ExpressionErrorField, Position: b.position(node), Type: graph.Type{Kind: graph.TypeString}, Name: data.Name().Text(), Receiver: receiver}, nil, true
		}
	}
	var target *ast.Node
	var arguments []*ast.Node
	switch node.Kind {
	case ast.KindNewExpression:
		data := node.AsNewExpression()
		target = data.Expression
		if data.Arguments != nil {
			arguments = data.Arguments.Nodes
		}
		if data.TypeArguments != nil && b.errorIntrinsic(target) != "" {
			return nil, b.fence(node), true
		}
	case ast.KindCallExpression:
		data := node.AsCallExpression()
		target = data.Expression
		arguments = data.Arguments.Nodes
		if (data.TypeArguments != nil || data.QuestionDotToken != nil) && b.errorIntrinsic(target) != "" {
			return nil, b.fence(node), true
		}
	default:
		return nil, nil, false
	}
	name := b.errorIntrinsic(target)
	if name == "" {
		return nil, nil, false
	}
	result := &graph.Expression{Kind: graph.ExpressionErrorConstruct, Position: b.position(node), Type: graph.Type{Kind: graph.TypeError}, Name: name}
	for i, argument := range arguments {
		if argument.Kind == ast.KindSpreadElement {
			return nil, b.fenceDiagnostic(argument, "ErrorConstructor", "spread constructor arguments require separate evaluation proof"), true
		}
		value, fence := b.expression(argument)
		if fence != nil {
			return nil, fence, true
		}
		if i == 0 && value.Type.Kind != graph.TypeString && value.Kind != graph.ExpressionUndefined {
			return nil, b.fenceDiagnostic(argument, "ErrorConstructor", "source error message conversion currently requires string or undefined"), true
		}
		// Error options may execute has/get traps and install cause. No object
		// options are erased merely because this checkpoint does not read cause.
		if i == 1 && value.Kind != graph.ExpressionUndefined && value.Type.Kind != graph.TypeString && value.Type.Kind != graph.TypeNumber && value.Type.Kind != graph.TypeBoolean {
			return nil, b.fenceDiagnostic(argument, "ErrorConstructor", "object error options require cause/property evaluation semantics"), true
		}
		result.Arguments = append(result.Arguments, value)
	}
	return result, nil, true
}

func (b *builder) errorCatchBinding(node *ast.Node) (*graph.Parameter, *fenceError) {
	if node == nil {
		return nil, nil
	}
	name := node.AsVariableDeclaration().Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return nil, b.fenceDiagnostic(node, "ErrorCatch", "catch destructuring requires separate observation semantics")
	}
	binding, fence := b.binding(name)
	if fence != nil {
		return nil, fence
	}
	valueType := graph.Type{Kind: graph.TypeThrown}
	b.bindingTypes[binding] = valueType
	return &graph.Parameter{Position: b.position(node), Binding: binding, Name: name.Text(), Type: valueType}, nil
}
