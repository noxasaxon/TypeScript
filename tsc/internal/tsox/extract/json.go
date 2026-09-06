package extract

import (
	"fmt"
	"math"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// Resolve both receiver and member to the library symbols. A local JSON binding
// or a source function named stringify does not acquire builtin semantics.
func (b *builder) isJSONStringify(node *ast.Node) bool {
	if node.Kind != ast.KindCallExpression {
		return false
	}
	target := node.AsCallExpression().Expression
	if target.Kind != ast.KindPropertyAccessExpression {
		return false
	}
	property := target.AsPropertyAccessExpression()
	if property.Expression.Kind != ast.KindIdentifier || property.Name() == nil || property.Name().Text() != "stringify" {
		return false
	}
	global := b.checker.GetGlobalSymbol("JSON", ast.SymbolFlagsValue, nil)
	if global == nil || b.checker.GetSymbolAtLocation(property.Expression) != global {
		return false
	}
	member := b.checker.GetPropertyOfType(b.checker.GetTypeOfSymbol(global), "stringify")
	return member != nil && b.checker.GetSymbolAtLocation(property.Name()) == member
}

func (b *builder) jsonFence(node *ast.Node, reason string) *fenceError {
	return b.fenceDiagnostic(node, "JSONStringify", "unsupported JSON.stringify: "+reason)
}

// Validate JSON domains before declarations are lowered so recursive types,
// dynamic values and callable toJSON fields report the actual JSON limitation
// at its call site, rather than an incidental declaration/emitter failure.
func (b *builder) checkJSONCalls(node *ast.Node) *fenceError {
	if b.isJSONStringify(node) {
		call := node.AsCallExpression()
		if call.QuestionDotToken != nil || call.Expression.AsPropertyAccessExpression().QuestionDotToken != nil || call.TypeArguments != nil {
			return b.jsonFence(node, "optional or generic builtin calls are outside the bounded contract")
		}
		if len(call.Arguments.Nodes) != 1 {
			return b.jsonFence(node, "exactly one value argument is supported; replacer and space are unsupported")
		}
		argument := call.Arguments.Nodes[0]
		typ := b.checker.GetTypeAtLocation(argument)
		if b.jsonHasHook(typ, make(map[*checker.Type]bool)) {
			return b.jsonFence(node, "callable toJSON hooks and their effects are unsupported")
		}
		if typ.Flags()&checker.TypeFlagsUndefined == 0 {
			valueType, fence := b.graphType(typ, argument)
			if fence != nil {
				return b.jsonFence(node, "value domain requires supported scalars, dense arrays and acyclic named plain records: "+fence.diagnostic.Message)
			}
			if reason := b.jsonTypeReason(valueType, make(map[graph.ShapeID]bool)); reason != "" {
				return b.jsonFence(node, reason)
			}
		}
	}
	var fence *fenceError
	node.ForEachChild(func(child *ast.Node) bool { fence = b.checkJSONCalls(child); return fence != nil })
	return fence
}

func (b *builder) jsonTypeReason(value graph.Type, visiting map[graph.ShapeID]bool) string {
	switch value.Kind {
	case graph.TypeNumber, graph.TypeString, graph.TypeBoolean:
		return ""
	case graph.TypeArray:
		if value.Element == nil || value.Element.Optional {
			return "optional or unknown array elements are outside the supported dense-array domain"
		}
		return b.jsonTypeReason(*value.Element, visiting)
	case graph.TypeObject:
		if visiting[value.Shape] {
			return "recursive/cycle-capable record domains are unsupported"
		}
		visiting[value.Shape] = true
		defer delete(visiting, value.Shape)
		shape, ok := b.shapeByID(value.Shape)
		if !ok {
			return "unknown named record shape"
		}
		for _, field := range shape.Fields {
			if field.Name == "__proto__" {
				return "__proto__ property/prototype semantics are outside the plain-record proof"
			}
			if field.Name == "toJSON" && field.Type.Kind == graph.TypeFunction {
				return "callable toJSON hooks and their effects are unsupported"
			}
			if reason := b.jsonTypeReason(field.Type, visiting); reason != "" {
				return reason
			}
		}
		return ""
	default:
		return "dynamic, null, function and other unsupported value domains require explicit serialization support"
	}
}

func (b *builder) jsonStringify(node *ast.Node) (*graph.Expression, *fenceError) {
	argumentNode := node.AsCallExpression().Arguments.Nodes[0]
	var argument *graph.Expression
	var fence *fenceError
	if b.checker.GetTypeAtLocation(argumentNode).Flags()&checker.TypeFlagsUndefined != 0 {
		argument, fence = b.expression(argumentNode)
	} else {
		valueType, typeFence := b.graphType(b.checker.GetTypeAtLocation(argumentNode), argumentNode)
		if typeFence != nil {
			return nil, b.jsonFence(node, typeFence.diagnostic.Message)
		}
		// JSON observes undefined directly even when the checker narrowed a
		// syntactic optional chain. Its input is not a required storage slot.
		valueType.Optional = true
		argument, fence = b.expressionForSlot(argumentNode, valueType)
	}
	if fence != nil {
		return nil, b.jsonFence(node, fence.diagnostic.Message)
	}
	unprovedPrototype := false
	walkGraphExpressions([]*graph.Statement{{Value: argument}}, func(value *graph.Expression) {
		// An intermediate receiver can supply an inherited field even when
		// the serialized result itself has a plain shape.
		if value.Type.Kind == graph.TypeObject {
			if _, hasPrototype := b.shapeField(value.Type.Shape, "__proto__"); hasPrototype {
				unprovedPrototype = true
			}
		}
	})
	if unprovedPrototype {
		return nil, b.jsonFence(node, "an argument expression shape contains __proto__; prototype lookup is outside the plain-record proof")
	}
	return &graph.Expression{Kind: graph.ExpressionJSONStringify, Position: b.position(node), Type: graph.Type{Kind: graph.TypeString, Optional: argument.Type.Optional || argument.Kind == graph.ExpressionUndefined}, Operand: argument}, nil
}

type jsonField struct {
	shape graph.ShapeID
	name  string
}

// Struct fields preserve declaration order only while omitted properties stay
// absent. This deliberately uses a whole-program shape+field overapproximation:
// a write on an unrelated object of the same shape also prevents the proof.
// It applies only to shapes reachable from JSON calls, leaving existing syntax
// accepted. Present-undefined properties retain their initial insertion order.
func (b *builder) finishJSON(program *graph.Program) graph.Result {
	omitted, written := make(map[jsonField]bool), make(map[jsonField]bool)
	var calls []*graph.Expression
	walkGraphExpressions(program.Statements, func(value *graph.Expression) {
		switch value.Kind {
		case graph.ExpressionObject:
			for _, field := range value.Properties {
				if field.Omitted {
					omitted[jsonField{value.Type.Shape, field.Name}] = true
				}
			}
		case graph.ExpressionAssignment, graph.ExpressionUpdate:
			target := value.Left
			if value.Kind == graph.ExpressionUpdate {
				target = value.Operand
			}
			if target != nil && target.Kind == graph.ExpressionProperty && target.Receiver != nil {
				written[jsonField{target.Receiver.Type.Shape, target.Name}] = true
			}
		case graph.ExpressionJSONStringify:
			calls = append(calls, value)
		}
	})
	var unsafeOrder func(graph.Type) string
	unsafeOrder = func(value graph.Type) string {
		if value.Kind == graph.TypeArray && value.Element != nil {
			return unsafeOrder(*value.Element)
		}
		if value.Kind != graph.TypeObject {
			return ""
		}
		shape, _ := b.shapeByID(value.Shape)
		for _, field := range shape.Fields {
			key := jsonField{value.Shape, field.Name}
			if omitted[key] && written[key] {
				return fmt.Sprintf("property insertion order is unproved: omitted %s.%s may be written (whole-program shape/field analysis)", shape.Name, field.Name)
			}
			if reason := unsafeOrder(field.Type); reason != "" {
				return reason
			}
		}
		return ""
	}
	for _, call := range calls {
		if reason := unsafeOrder(call.Operand.Type); reason != "" {
			source := program.SourcePath
			if call.Position.SourcePath != "" {
				source = call.Position.SourcePath
			}
			return diagnosticResult(graph.Diagnostic{SourcePath: source, Position: call.Position, Construct: "JSONStringify", Message: "unsupported JSON.stringify: " + reason})
		}
	}
	return graph.Result{Program: program}
}

// TypeScript method syntax can reject ordinary extraction before the property
// becomes a graph field; identify toJSON hooks through checker symbols first.
func (b *builder) jsonHasHook(value *checker.Type, seen map[*checker.Type]bool) bool {
	if value == nil || seen[value] {
		return false
	}
	seen[value] = true
	if value.Flags()&checker.TypeFlagsUnion != 0 {
		for _, part := range value.Types() {
			if b.jsonHasHook(part, seen) {
				return true
			}
		}
		return false
	}
	if value.Flags()&checker.TypeFlagsObject == 0 {
		return false
	}
	if hook := b.checker.GetPropertyOfType(value, "toJSON"); hook != nil && b.jsonCallableType(b.checker.GetTypeOfSymbol(hook)) {
		return true
	}
	if b.checker.IsArrayType(value) {
		return b.jsonHasHook(b.checker.GetElementTypeOfArrayType(value), seen)
	}
	for _, property := range b.checker.GetPropertiesOfType(value) {
		if b.jsonHasHook(b.checker.GetTypeOfSymbol(property), seen) {
			return true
		}
	}
	return false
}

func (b *builder) jsonCallableType(value *checker.Type) bool {
	if value.Flags()&checker.TypeFlagsUnion != 0 {
		for _, part := range value.Types() {
			if b.jsonCallableType(part) {
				return true
			}
		}
	}
	return len(b.checker.GetSignaturesOfType(value, checker.SignatureKindCall)) != 0
}

// These immutable global number values are resolved by symbol, like JSON itself.
// Source bindings with the same names retain their ordinary mutable semantics.
func (b *builder) builtinNumber(node *ast.Node) (float64, bool) {
	if node.Kind != ast.KindIdentifier || (node.Text() != "NaN" && node.Text() != "Infinity") {
		return 0, false
	}
	symbol := b.checker.GetGlobalSymbol(node.Text(), ast.SymbolFlagsValue, nil)
	if symbol == nil || b.checker.GetSymbolAtLocation(node) != symbol {
		return 0, false
	}
	if node.Text() == "NaN" {
		return math.NaN(), true
	}
	return math.Inf(1), true
}
