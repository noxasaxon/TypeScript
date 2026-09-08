package extract

import (
	"context"
	"fmt"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
)

type AsyncSourceObligation struct {
	Diagnostic graph.Diagnostic
	SourceID   string
	Statement  *ast.Node `json:"-"`
}

type AsyncTemplateBody struct {
	Statements        map[*graph.Statement]SourceBodyNode  `json:"-"`
	Expressions       map[*graph.Expression]SourceBodyNode `json:"-"`
	BindingSources    map[graph.BindingID][]SourceBodyNode
	SourcePath        string
	Captures          []graph.AsyncCapturedCallable
	SourceObligations []AsyncSourceObligation
	Instance          int
	Template          string
	Parameters        []graph.Parameter
	Result            graph.Type
	Shapes            []graph.Shape
	Body              []*graph.Statement
	Stages            []graph.AsyncStage
	Flow              *graph.AsyncFlow
	Calls             []graph.AsyncCallableInvocation
	Obligations       []string
	Diagnostics       []graph.Diagnostic
}
type middlewareBodyContext struct {
	b       *builder
	input   checked.CallableBodyInput
	output  *AsyncTemplateBody
	program *graph.AsyncProgram
	pending []*graph.Statement
}

func ExtractCallableBody(input checked.CallableBodyInput) *AsyncTemplateBody {
	out := &AsyncTemplateBody{Instance: input.Instance, Template: input.Template, Obligations: []string{"startup normal exit and persistent callable environment are not discharged", "each invocation requires evaluated parameter/capture domains and retention/layout proof", "promise job/cleanup/adoption graph has no native lowering"}}
	if input.Program == nil || input.Node == nil {
		out.Obligations = append(out.Obligations, "missing actual source template")
		return out
	}
	p := input.Program
	file := ast.GetSourceFileOfNode(input.Node)
	out.SourcePath = file.FileName()
	if _, ok := p.Files[file]; !ok {
		out.Obligations = append(out.Obligations, "template source not in supplied Program")
		return out
	}
	sourceAsync := func(node *ast.Node) bool {
		if node == nil || node.Body() == nil || node.Modifiers() == nil {
			return false
		}
		for _, modifier := range node.Modifiers().Nodes {
			if modifier.Kind == ast.KindAsyncKeyword {
				return true
			}
		}
		return false
	}
	if !sourceAsync(input.Node) {
		out.Obligations = append(out.Obligations, "template must be an actual async source body, not a Promise signature")
		return out
	}
	for _, target := range input.Targets {
		if target.Node == nil {
			out.Obligations = append(out.Obligations, "missing callable target source")
			return out
		}
		if _, ok := p.Files[ast.GetSourceFileOfNode(target.Node)]; !ok || !sourceAsync(target.Node) {
			out.Obligations = append(out.Obligations, "callable target does not belong to actual async source Program")
			return out
		}
	}
	c, done := p.Compiler.GetTypeChecker(context.Background())
	defer done()
	out.Expressions = map[*graph.Expression]SourceBodyNode{}
	out.Statements = map[*graph.Statement]SourceBodyNode{}
	b := &builder{scopedStatementSources: out.Statements, scopedExpressionSources: out.Expressions, standardEntry: true, jsonValues: true, sourcePath: file.FileName(), file: file, checker: c, bindings: map[*ast.Symbol]graph.BindingID{}, bindingTypes: map[graph.BindingID]graph.Type{}, nextBinding: 1, shapeIDs: map[*ast.Symbol]graph.ShapeID{}, shapeBuilding: map[*ast.Symbol]bool{}, moduleFiles: p.Files, entryFile: p.Entry, asyncThrow: true}
	fail := func(f *fenceError) *AsyncTemplateBody {
		out.Diagnostics = append(out.Diagnostics, f.diagnostic)
		return out
	}
	params, f := b.parameters(input.Node.Parameters())
	if f != nil {
		return fail(f)
	}
	out.Parameters = params
	signatures := c.GetSignaturesOfType(c.GetTypeAtLocation(input.Node), checker.SignatureKindCall)
	if len(signatures) != 1 {
		out.Obligations = append(out.Obligations, "source callable needs one actual signature")
		return out
	}
	promised := c.GetPromisedTypeOfPromise(c.GetReturnTypeOfSignature(signatures[0]))
	if promised == nil {
		out.Obligations = append(out.Obligations, "actual async template fulfillment unavailable")
		return out
	}
	result, f := b.graphType(promised, input.Node)
	if f != nil {
		return fail(f)
	}
	out.Result = result
	b.returnType = &result
	a := &graph.AsyncProgram{Platform: graph.PlatformStandard, Result: result, Position: b.position(input.Node)}
	ctx := &middlewareBodyContext{b: b, input: input, output: out, program: a}
	b.middleware = ctx
	body := input.Node.Body()
	if body == nil || body.Kind != ast.KindBlock {
		out.Obligations = append(out.Obligations, "expression-bodied template requires explicit source return projection")
		return out
	}
	statements, f := b.asyncBody(body.AsBlock().Statements.Nodes, a, nil, 0, nil)
	if f != nil {
		return fail(f)
	}
	out.Body = statements
	out.Stages = a.Stages
	out.Flow = buildAsyncFlowMode(statements, a.Stages, true)
	out.Shapes = b.shapes
	out.BindingSources = b.sourceBindingIdentities()
	if err := out.Flow.ValidateRegions(); err != nil {
		out.Obligations = append(out.Obligations, err.Error())
	}
	return out
}
func middlewareContainsAwait(n *ast.Node) bool {
	if n == nil {
		return false
	}

	if n.Kind == ast.KindAwaitExpression {
		return true
	}
	if n.Kind == ast.KindArrowFunction || n.Kind == ast.KindFunctionExpression || n.Kind == ast.KindFunctionDeclaration {
		return false
	}
	return n.ForEachChild(middlewareContainsAwait)
}
func (m *middlewareBodyContext) capture(value *graph.Expression, pos graph.Position) *graph.Expression {
	b := m.b
	id := b.nextBinding
	b.nextBinding++
	b.bindingTypes[id] = value.Type
	m.pending = append(m.pending, &graph.Statement{Kind: graph.StatementVariable, Binding: id, Name: "evaluated_operand", Type: value.Type, Position: pos, Value: value})
	return &graph.Expression{Kind: graph.ExpressionIdentifier, Binding: id, Type: value.Type, Position: pos}
}
func (m *middlewareBodyContext) expression(n *ast.Node) (*graph.Expression, *fenceError, bool) {
	b := m.b
	if value, f, handled := m.promiseAll(n); handled {
		return value, f, true
	}
	if n.Kind != ast.KindAwaitExpression && n.Kind != ast.KindCallExpression && middlewareContainsAwait(n) {
		return nil, b.fenceDiagnostic(n, "AsyncCallableOrder", "await in this expression needs selected-edge or ordered-place lifting"), true
	}
	if n.Kind == ast.KindIdentifier {
		if target, ok := m.input.Targets[b.sourceSymbol(n)]; ok {
			id, f := b.binding(n)
			if f != nil {
				return nil, f, true
			}
			typ := graph.Type{Kind: graph.TypeAsyncCallable}
			b.bindingTypes[id] = typ
			found := false
			for _, capture := range m.output.Captures {
				if capture.Binding == id {
					found = true
				}
			}
			if !found {
				m.output.Captures = append(m.output.Captures, graph.AsyncCapturedCallable{Binding: id, Cell: target.Cell, Instance: target.Instance, Template: target.Template, Position: b.position(n)})
			}
			return &graph.Expression{Kind: graph.ExpressionAsyncCallable, Binding: id, Type: typ, Position: b.position(n)}, nil, true
		}
	}
	if n.Kind == ast.KindAwaitExpression {
		operand := n.AsAwaitExpression().Expression
		var producer *graph.AsyncProducer
		var fulfillment graph.Type
		var promise *graph.Expression
		if p, t, f, handled := b.standardProducer(operand); handled {
			if f != nil {
				return nil, f, true
			}
			producer = p
			fulfillment = t
		} else {
			value, f := b.expression(operand)
			if f != nil {
				return nil, f, true
			}
			if value.Type.Kind != graph.TypePromiseValue || value.Type.Element == nil {
				return nil, b.fenceDiagnostic(n, "AsyncCallableAwait", "await needs an actual source promise operation"), true
			}
			fulfillment = *value.Type.Element
			promise = m.capture(value, b.position(operand))
		}
		id := b.nextBinding
		b.nextBinding++
		b.bindingTypes[id] = fulfillment
		operation := graph.AsyncAwait{Binding: id, Name: "awaited_value", Type: fulfillment, Position: b.position(n), Producer: producer, Promise: promise}
		m.program.Stages = append(m.program.Stages, graph.AsyncStage{Await: operation})
		m.pending = append(m.pending, &graph.Statement{Kind: graph.StatementAsyncAwait, Binding: id, Name: operation.Name, Type: fulfillment, Position: operation.Position, Producer: producer, Value: promise})
		return &graph.Expression{Kind: graph.ExpressionIdentifier, Binding: id, Type: fulfillment, Position: b.position(n)}, nil, true
	}
	if n.Kind != ast.KindCallExpression {
		return nil, nil, false
	}
	call := n.AsCallExpression()
	target, async := m.input.Targets[b.sourceSymbol(call.Expression)]
	if !async && !middlewareContainsAwait(n) {
		return nil, nil, false
	}
	if call.Expression.Kind != ast.KindIdentifier || call.QuestionDotToken != nil || call.TypeArguments != nil {
		return nil, b.fenceDiagnostic(n, "AsyncCallableCall", "ordered callable target needs an explicit source place contract"), true
	}
	callee, f := b.expression(call.Expression)
	if f != nil {
		return nil, f, true
	}
	var parameterTypes []graph.Type
	var result graph.Type
	if async {
		for _, param := range target.Node.Parameters() {
			typ, f := b.checkedType(param.Name())
			if f != nil {
				return nil, f, true
			}
			parameterTypes = append(parameterTypes, typ)
		}
		sig := b.checker.GetSignaturesOfType(b.checker.GetTypeAtLocation(target.Node), checker.SignatureKindCall)
		if len(sig) != 1 {
			return nil, b.fence(n), true
		}
		promised := b.checker.GetPromisedTypeOfPromise(b.checker.GetReturnTypeOfSignature(sig[0]))
		if promised == nil {
			return nil, b.fence(n), true
		}
		result, f = b.graphType(promised, n)
		if f != nil {
			return nil, f, true
		}
	} else {
		if callee.Type.Kind != graph.TypeFunction || callee.Type.Result == nil {
			return nil, b.fence(n), true
		}
		parameterTypes = callee.Type.Parameters
		result = *callee.Type.Result
	}
	if len(parameterTypes) != len(call.Arguments.Nodes) {
		return nil, b.fenceDiagnostic(n, "AsyncCallableArguments", "defaults and extra arguments need ordered invocation/default facts"), true
	}
	// Every earlier operand is saved before extracting a later operand's await.
	// This is source evaluation order, not a repeated callee read after suspension.
	staged := middlewareContainsAwait(n)
	if staged {
		callee = m.capture(callee, b.position(call.Expression))
	}
	args := make([]*graph.Expression, 0, len(parameterTypes))
	for i, node := range call.Arguments.Nodes {
		value, f := b.expressionForSlot(node, parameterTypes[i])
		if f != nil {
			return nil, f, true
		}
		if !slotAccepts(parameterTypes[i], value) {
			return nil, b.typeFlowFence(node, parameterTypes[i], value.Type), true
		}
		adaptUndefinedToSlot(parameterTypes[i], value)
		if staged {
			value = m.capture(value, b.position(node))
		}
		args = append(args, value)
	}
	typ := result
	kind := graph.ExpressionCall
	if async {
		typ = graph.Type{Kind: graph.TypePromiseValue, Element: &result}
		kind = graph.ExpressionCallAsync
	}
	value := &graph.Expression{Kind: kind, Callee: callee, Arguments: args, Type: typ, Position: b.position(n)}
	if async {
		value.Pending = &graph.SourcePending{Kind: "call", Obligations: []string{"actual evaluated async callee candidate; startup, invocation, effects and promise ownership remain unproved"}}
	}
	if async {
		m.output.Calls = append(m.output.Calls, graph.AsyncCallableInvocation{Instance: target.Instance, Template: target.Template, Expression: value})
	}
	return value, nil, true
}
func (m *middlewareBodyContext) statement(n *ast.Node) ([]*graph.Statement, *fenceError, bool) {
	b := m.b
	if n.Kind != ast.KindVariableStatement && n.Kind != ast.KindReturnStatement && n.Kind != ast.KindExpressionStatement {
		return nil, nil, false
	}
	if len(m.pending) != 0 {
		return nil, b.fenceDiagnostic(n, "AsyncCallableOrder", "pending source operands crossed a statement boundary"), true
	}
	var statements []*graph.Statement
	var f *fenceError
	if n.Kind == ast.KindReturnStatement && n.AsReturnStatement().Expression != nil {
		expression := n.AsReturnStatement().Expression
		value, err := b.expressionForSlot(expression, *b.returnType)
		if err != nil {
			if middlewareContainsAwait(expression) {
				return nil, err, true
			}
			// Preserve the entire original non-suspending return as an explicit
			// obligation. No partial speculative operand graph is executable.
			m.pending = nil
			m.output.SourceObligations = append(m.output.SourceObligations, AsyncSourceObligation{Diagnostic: err.diagnostic, SourceID: m.input.Template + "/return/" + fmt.Sprint(n.Pos()), Statement: n})
			return []*graph.Statement{{Kind: graph.StatementUnresolvedReturn, Position: b.position(n)}}, nil, true
		}
		if value.Type.Kind == graph.TypePromiseValue {
			statements = []*graph.Statement{{Kind: graph.StatementReturnPromise, Position: b.position(n), Value: value, Type: value.Type}}
		} else {
			if !slotAccepts(*b.returnType, value) {
				return nil, b.typeFlowFence(expression, *b.returnType, value.Type), true
			}
			adaptUndefinedToSlot(*b.returnType, value)
			statements = []*graph.Statement{{Kind: graph.StatementReturn, Position: b.position(n), Value: value, Type: *b.returnType}}
		}

	} else {
		statements, f = b.statement(n, false)
	}
	if f != nil {
		return nil, f, true
	}
	prefix := m.pending
	m.pending = nil
	return append(prefix, statements...), nil, true
}
