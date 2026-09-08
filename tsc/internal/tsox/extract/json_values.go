package extract

import (
	"unicode/utf16"
	"unicode/utf8"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/internal/stringutil"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// libraryMemberCall resolves both sides. Source bindings, even with identical
// names/signatures, do not acquire standard-library behavior.
func (b *builder) libraryMemberCall(node *ast.Node, globalName, memberName string) bool {
	if node.Kind != ast.KindCallExpression {
		return false
	}
	target := node.AsCallExpression().Expression
	if target.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	property := target.AsPropertyAccessExpression()
	if property.Expression.Kind != ast.KindIdentifier || property.Name() == nil || property.Name().Text() != memberName {
		return false
	}
	global := b.checker.GetGlobalSymbol(globalName, ast.SymbolFlagsValue, nil)
	if global == nil || b.checker.GetSymbolAtLocation(property.Expression) != global {
		return false
	}
	member := b.checker.GetPropertyOfType(b.checker.GetTypeOfSymbol(global), memberName)
	return member != nil && b.checker.GetSymbolAtLocation(property.Name()) == member
}

// unknownProjection records a proof obligation, not a successful runtime guard.
// Shape coercion is deliberately absent: checking object never establishes a
// typed record layout, nor that copying that object would preserve its identity.
func unknownProjection(value *graph.Expression, target graph.Type) *graph.Expression {
	if value == nil || value.Type.Kind != graph.TypeUnknown {
		return value
	}
	// A checked present leaf may flow into an optional slot. This requests the
	// nonoptional leaf proof; optional annotations still prove nothing about it.
	target.Optional = false
	switch target.Kind {
	case graph.TypeString, graph.TypeNumber, graph.TypeBoolean:
	case graph.TypeArray:
		if target.Element == nil || target.Element.Kind != graph.TypeUnknown {
			return value
		}
	default:
		return value
	}
	return &graph.Expression{Kind: graph.ExpressionUnknownProjection, Position: value.Position, Type: target, Operand: value}
}

// jsonProjectionType uses the checker only to describe what a use requests.
// Evidence still must discharge UnknownProjection against actual control flow.
func (b *builder) jsonProjectionType(node *ast.Node) graph.Type {
	value := b.checker.GetTypeAtLocation(node)
	switch {
	case value.Flags()&checker.TypeFlagsStringLike != 0:
		return graph.Type{Kind: graph.TypeString}
	case value.Flags()&checker.TypeFlagsNumberLike != 0:
		return graph.Type{Kind: graph.TypeNumber}
	case value.Flags()&checker.TypeFlagsBooleanLike != 0:
		return graph.Type{Kind: graph.TypeBoolean}
	case b.checker.IsArrayType(value):
		return graph.Type{Kind: graph.TypeArray, Element: &graph.Type{Kind: graph.TypeUnknown}}
	default:
		return graph.Type{Kind: graph.TypeUnknown}
	}
}

func (b *builder) jsonValueExpression(node *ast.Node) (*graph.Expression, *fenceError, bool) {
	unknown := graph.Type{Kind: graph.TypeUnknown}
	boolean := graph.Type{Kind: graph.TypeBoolean}
	stringType := graph.Type{Kind: graph.TypeString}
	switch node.Kind {
	case ast.KindNullKeyword:
		return &graph.Expression{Kind: graph.ExpressionNull, Position: b.position(node), Type: graph.Type{Kind: graph.TypeNull}}, nil, true
	case ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral:
		if !utf8.ValidString(node.Text()) {
			units := make([]uint16, 0, len(node.Text()))
			for text := node.Text(); len(text) != 0; {
				unit, size := stringutil.DecodeJSStringRune(text)
				if unit == utf8.RuneError && size == 1 {
					return nil, b.fenceDiagnostic(node, "StringRepresentation", "unsupported malformed string encoding"), true
				}
				if unit <= 0xffff {
					units = append(units, uint16(unit))
				} else {
					high, low := utf16.EncodeRune(unit)
					units = append(units, uint16(high), uint16(low))
				}
				text = text[size:]
			}
			return &graph.Expression{Kind: graph.ExpressionString, Position: b.position(node), Type: stringType, StringUnits: units}, nil, true
		}
	case ast.KindTypeOfExpression:
		operand, fence := b.expression(node.AsTypeOfExpression().Expression)
		if fence != nil {
			return nil, fence, true
		}
		return &graph.Expression{Kind: graph.ExpressionTypeOf, Position: b.position(node), Type: stringType, Operand: operand}, nil, true
	case ast.KindCallExpression:
		parse := b.libraryMemberCall(node, "JSON", "parse")
		array := b.libraryMemberCall(node, "Array", "isArray")
		if parse || array {
			call := node.AsCallExpression()
			if call.QuestionDotToken != nil || call.Expression.AsPropertyAccessExpression().QuestionDotToken != nil || call.TypeArguments != nil || len(call.Arguments.Nodes) != 1 {
				return nil, b.fenceDiagnostic(node, "JSONBoundaryCall", "unsupported JSON boundary builtin: exactly one argument and a direct nongeneric call are required"), true
			}
			operand, fence := b.expression(call.Arguments.Nodes[0])
			if fence != nil {
				return nil, fence, true
			}
			kind, result := graph.ExpressionIsArray, boolean
			if parse {
				operand = unknownProjection(operand, stringType)
				if operand.Type.Kind != graph.TypeString || operand.Type.Optional {
					return nil, b.fenceDiagnostic(node, "JSONParse", "unsupported JSON.parse text coercion: a string value is required"), true
				}
				kind, result = graph.ExpressionJSONParse, unknown
			}
			return &graph.Expression{Kind: kind, Position: b.position(node), Type: result, Operand: operand}, nil, true
		}
		call := node.AsCallExpression()
		if call.Expression.Kind == ast.KindPropertyAccessExpression {
			property := call.Expression.AsPropertyAccessExpression()
			if property.Name() != nil && property.Name().Text() == "trim" && call.QuestionDotToken == nil && property.QuestionDotToken == nil && call.TypeArguments == nil && len(call.Arguments.Nodes) == 0 {
				receiver, fence := b.expression(property.Expression)
				if fence != nil {
					return nil, fence, true
				}
				receiver = unknownProjection(receiver, b.jsonProjectionType(property.Expression))
				if receiver.Type.Kind == graph.TypeString && !receiver.Type.Optional {
					return &graph.Expression{Kind: graph.ExpressionStringTrim, Position: b.position(node), Type: stringType, Operand: receiver}, nil, true
				}
			}
		}
	case ast.KindBinaryExpression:
		data := node.AsBinaryExpression()
		if data.OperatorToken.Kind == ast.KindInKeyword {
			key, fence := b.expression(data.Left)
			if fence != nil {
				return nil, fence, true
			}
			receiver, fence := b.expression(data.Right)
			if fence != nil {
				return nil, fence, true
			}
			if receiver.Type.Kind != graph.TypeUnknown || key.Type.Kind != graph.TypeString || key.Type.Optional {
				return nil, b.fenceDiagnostic(node, "UnknownProperty", "unsupported in domain: unknown receiver and string key required"), true
			}
			return &graph.Expression{Kind: graph.ExpressionHasProperty, Position: b.position(node), Type: boolean, Index: key, Receiver: receiver}, nil, true
		}

	}
	return nil, nil, false
}

// The ordinary extractor evaluates receivers once before this domain hook.
func (b *builder) jsonValueProperty(node *ast.Node, receiver *graph.Expression) (*graph.Expression, *fenceError, bool) {
	unknown := graph.Type{Kind: graph.TypeUnknown}
	stringType := graph.Type{Kind: graph.TypeString}
	data := node.AsPropertyAccessExpression()
	if data.QuestionDotToken != nil || node.Flags&ast.NodeFlagsOptionalChain != 0 || data.Name() == nil {
		return nil, nil, false
	}
	if data.Name().Text() == "length" {
		projected := receiver
		if receiver.Type.Kind == graph.TypeUnknown {
			projected = unknownProjection(receiver, b.jsonProjectionType(data.Expression))
		}
		if projected.Type.Kind == graph.TypeString && !projected.Type.Optional {
			return &graph.Expression{Kind: graph.ExpressionStringLength, Position: b.position(node), Type: graph.Type{Kind: graph.TypeNumber}, Operand: projected}, nil, true
		}
		if receiver.Type.Kind == graph.TypeUnknown && projected.Type.Kind == graph.TypeArray {
			return &graph.Expression{Kind: graph.ExpressionArrayLength, Position: b.position(node), Type: graph.Type{Kind: graph.TypeNumber}, Receiver: projected}, nil, true
		}
	}
	if receiver.Type.Kind == graph.TypeUnknown {
		key := &graph.Expression{Kind: graph.ExpressionString, Position: b.position(data.Name()), Type: stringType, String: data.Name().Text()}
		return &graph.Expression{Kind: graph.ExpressionUnknownProperty, Position: b.position(node), Type: unknown, Receiver: receiver, Index: key}, nil, true
	}
	return nil, nil, false
}

// Both operands are already extracted; retain the normal binary path for
// static values without recursively extracting its children a second time.
func (b *builder) jsonValueBinary(node *ast.Node, operator string, left, right *graph.Expression) (*graph.Expression, *fenceError, bool) {
	data := node.AsBinaryExpression()
	boolean := graph.Type{Kind: graph.TypeBoolean}
	if left.Type.Kind != graph.TypeUnknown && right.Type.Kind != graph.TypeUnknown && left.Type.Kind != graph.TypeNull && right.Type.Kind != graph.TypeNull {
		return nil, nil, false
	}
	if operator == "===" || operator == "!==" {
		return &graph.Expression{Kind: graph.ExpressionBinary, Position: b.position(node), Type: boolean, Operator: operator, Left: left, Right: right}, nil, true
	}
	left = unknownProjection(left, b.jsonProjectionType(data.Left))
	right = unknownProjection(right, b.jsonProjectionType(data.Right))
	result, fence := b.checkedType(node)
	if fence != nil {
		return nil, fence, true
	}
	if !validBinary(operator, left.Type, right.Type, result) {
		return nil, b.fence(node), true
	}
	return &graph.Expression{Kind: graph.ExpressionBinary, Position: b.position(node), Type: result, Operator: operator, Left: left, Right: right}, nil, true
}
