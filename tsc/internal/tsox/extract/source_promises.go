package extract

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/tsox/graph"
)

func (m *middlewareBodyContext) promiseAll(n *ast.Node) (*graph.Expression, *fenceError, bool) {
	if n.Kind != ast.KindCallExpression {
		return nil, nil, false
	}
	b := m.b
	call := n.AsCallExpression()
	if call.Expression.Kind != ast.KindPropertyAccessExpression {
		return nil, nil, false
	}
	member := call.Expression.AsPropertyAccessExpression()
	if member.Name().Text() != "all" || !b.platformGlobal(member.Expression, "Promise") {
		return nil, nil, false
	}
	if !b.platformMember(member.Name(), "PromiseConstructor", "all") || member.QuestionDotToken != nil || call.QuestionDotToken != nil || call.TypeArguments != nil {
		return nil, b.fenceDiagnostic(n, "SourcePromiseIntrinsic", "source Promise.all needs the actual checked intrinsic reference"), true
	}
	if middlewareContainsAwait(n) {
		return nil, b.fenceDiagnostic(n, "SourcePromiseOrder", "await inside Promise.all operands requires ordered reference/selected-edge lifting"), true
	}
	promised := b.checker.GetPromisedTypeOfPromise(b.checker.GetTypeAtLocation(n))
	if promised == nil {
		return nil, b.fenceDiagnostic(n, "SourcePromiseSchema", "source all result has no fulfillment schema"), true
	}
	fulfillment, f := b.graphType(promised, n)
	if f != nil {
		return nil, f, true
	}
	pending := func(kind string) *graph.SourcePending {
		return &graph.SourcePending{Kind: kind, Obligations: []string{"actual intrinsic evaluation retained; constructor/then/iterator and startup effects are unproved"}}
	}
	receiver := &graph.Expression{Kind: graph.ExpressionPromiseGlobal, Position: b.position(member.Expression), Type: graph.Type{Kind: graph.TypeSourceUnclassified}, Pending: pending("lexical-get-value")}
	callee := &graph.Expression{Kind: graph.ExpressionProperty, Position: b.position(call.Expression), Receiver: receiver, Name: "all", Type: graph.Type{Kind: graph.TypeSourceUnclassified}, Pending: pending("property-get")}
	// These are real constituent source nodes, not synthesized source positions.
	b.scopedExpressionSources[receiver] = SourceBodyNode{Node: member.Expression}
	b.scopedExpressionSources[callee] = SourceBodyNode{Node: call.Expression}
	// The enclosing await destination is not the literal schema of an all
	// argument. Preserve the argument's own actual checked/source structure.
	previousSlot := b.literalSlot
	b.literalSlot = nil
	defer func() { b.literalSlot = previousSlot }()
	args := make([]*graph.Expression, 0, len(call.Arguments.Nodes))
	for _, node := range call.Arguments.Nodes {
		value, f := b.expression(node)
		if f != nil {
			return nil, f, true
		}
		args = append(args, value)
	}
	return &graph.Expression{Kind: graph.ExpressionPromiseAll, Position: b.position(n), Callee: callee, Arguments: args, Type: graph.Type{Kind: graph.TypePromiseValue, Element: &fulfillment}, Pending: pending("call")}, nil, true
}

func (b *builder) sourcePromiseTuple(value *checker.Type, node *ast.Node) (graph.Type, *fenceError, bool) {
	if b.middleware == nil || !value.IsTupleType() {
		return graph.Type{}, nil, false
	}
	// A homogeneous fixed tuple is also a source array schema. Exact count and
	// source input occurrences remain in the actual array/call graph. This does
	// not supply evaluated element, identity, iterator or native layout proof.
	var element *graph.Type
	for _, arg := range b.checker.GetTypeArguments(value) {
		typ, f := b.graphType(arg, node)
		if f != nil {
			return graph.Type{}, f, true
		}
		if typ.Optional {
			return graph.Type{}, b.fenceDiagnostic(node, "SourcePromiseTuple", "optional tuple element source schema pending"), true
		}
		if element == nil {
			element = &typ
		} else if !sameType(*element, typ) {
			return graph.Type{}, b.fenceDiagnostic(node, "SourcePromiseTuple", "heterogeneous tuple source schema pending"), true
		}
	}
	if element == nil {
		element = &graph.Type{Kind: graph.TypeSourceUnclassified}
	}
	return graph.Type{Kind: graph.TypeArray, Element: element}, nil, true
}
