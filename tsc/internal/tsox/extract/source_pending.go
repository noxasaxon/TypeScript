package extract

import (
	"context"
	"fmt"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
)

type sourceRecoveryContext struct {
	scope *checked.SourceRecoveryScope
	out   *graph.RecoveredSourceBody
	nodes map[*graph.Expression]*ast.Node
}

func sourceUnclassified() graph.Type { return graph.Type{Kind: graph.TypeSourceUnclassified} }

// RecoverScopedSourceBody retains one actual body and the entire scope ledger.
// Other module bodies/startup operations remain pending, never omitted proof.
func RecoverScopedSourceBody(scope *checked.SourceRecoveryScope, node *ast.Node) (*graph.RecoveredSourceBody, []graph.Diagnostic) {
	if scope == nil {
		return nil, []graph.Diagnostic{{Message: "actual source scope required"}}
	}
	template, e := scope.ActualTemplate(node)
	if e != nil {
		return nil, []graph.Diagnostic{{Message: e.Error()}}
	}
	p := scope.ActualProgram()
	file := ast.GetSourceFileOfNode(node)
	c, done := p.Compiler.GetTypeChecker(context.Background())
	defer done()
	out := &graph.RecoveredSourceBody{Template: template, Async: hasModifier(node.Modifiers(), ast.KindAsyncKeyword), Bindings: map[graph.BindingID]graph.SourceLexicalID{}, Expressions: map[*graph.Expression]graph.SourceSite{}, Statements: map[*graph.Statement]graph.SourceSite{}, Obligations: []string{"all selected module startup, export-state, callable body and invocation domains remain unproved"}}
	ctx := &sourceRecoveryContext{scope: scope, out: out, nodes: map[*graph.Expression]*ast.Node{}}
	b := &builder{sourceRecovery: ctx, file: file, entryFile: p.Entry, sourcePath: file.FileName(), checker: c, moduleFiles: p.Files, bindings: map[*ast.Symbol]graph.BindingID{}, bindingTypes: map[graph.BindingID]graph.Type{}, shapeIDs: map[*ast.Symbol]graph.ShapeID{}, shapeBuilding: map[*ast.Symbol]bool{}, nextBinding: 1, asyncThrow: true}
	for _, param := range node.Parameters() {
		if param.Name() == nil || param.Name().Kind != ast.KindIdentifier || param.AsParameterDeclaration().Initializer != nil || param.AsParameterDeclaration().DotDotDotToken != nil {
			return nil, []graph.Diagnostic{b.fenceWithMessage(param, "source parameter defaults/rest require invocation mapping").diagnostic}
		}
		id, f := b.binding(param.Name())
		if f != nil {
			return nil, []graph.Diagnostic{f.diagnostic}
		}
		out.Parameters = append(out.Parameters, id)
	}
	if node.Body() == nil || node.Body().Kind != ast.KindBlock {
		return nil, []graph.Diagnostic{b.fenceWithMessage(node, "source body requires actual block").diagnostic}
	}
	body, f := b.statementBody(node.Body())
	if f != nil {
		return nil, []graph.Diagnostic{f.diagnostic}
	}
	out.Body = body
	temporary := &graph.TypedSourceBody{Function: &graph.Statement{Body: body}}
	for s := range typedBodyStatements(temporary) {
		s.MarkSourceOnly()
	}
	for x := range typedBodyExpressions(temporary) {
		x.MarkSourceOnly()
	}
	out.Registry = scope.View()
	return out, nil
}
func (c *sourceRecoveryContext) recordExpression(n *ast.Node, x *graph.Expression) {
	x.MarkSourceOnly()
	if _, exists := c.out.Expressions[x]; !exists {
		site, e := c.scope.ActualSite(n)
		if e != nil {
			panic(e)
		}
		c.out.Expressions[x] = site
		c.nodes[x] = n
	}
}
func (b *builder) sourceBinding(n *ast.Node) (graph.BindingID, *fenceError) {
	h, e := b.sourceRecovery.scope.LexicalBinding(n)
	if e != nil {
		return 0, b.fenceWithMessage(n, e.Error())
	}
	id, e := b.sourceRecovery.scope.BindingID(h)
	if e != nil {
		return 0, b.fenceWithMessage(n, e.Error())
	}
	binding := graph.BindingID(id)
	b.sourceRecovery.out.Bindings[binding] = id
	b.bindingTypes[binding] = sourceUnclassified()
	return binding, nil
}
func (b *builder) sourcePending(n *ast.Node, kind string) *graph.SourcePending {
	site, e := b.sourceRecovery.scope.ActualSite(n)
	if e != nil {
		panic(e)
	}
	return &graph.SourcePending{Kind: kind, Site: site, Obligations: []string{"actual evaluated domain, intrinsic/callable identity and effects unresolved"}}
}
func (b *builder) sourceStatement(n *ast.Node) ([]*graph.Statement, *fenceError, bool) {
	switch n.Kind {
	case ast.KindExpressionStatement, ast.KindReturnStatement, ast.KindThrowStatement:
		var source *ast.Node
		kind := graph.StatementExpression
		switch n.Kind {
		case ast.KindExpressionStatement:
			source = n.AsExpressionStatement().Expression
		case ast.KindReturnStatement:
			source = n.AsReturnStatement().Expression
			kind = graph.StatementReturn
		case ast.KindThrowStatement:
			source = n.AsThrowStatement().Expression
			kind = graph.StatementThrow
		}
		var x *graph.Expression
		var f *fenceError
		if source != nil {
			x, f = b.expression(source)
			if f != nil {
				return nil, f, true
			}
		}
		s := &graph.Statement{Kind: kind, Position: b.position(n), Value: x, Type: sourceUnclassified()}
		site, _ := b.sourceRecovery.scope.ActualSite(n)
		b.sourceRecovery.out.Statements[s] = site
		return []*graph.Statement{s}, nil, true
	case ast.KindEmptyStatement:
		site, _ := b.sourceRecovery.scope.ActualSite(n)
		b.sourceRecovery.out.NoOps = append(b.sourceRecovery.out.NoOps, site)
		return nil, nil, true
	}
	return nil, nil, false
}
func (b *builder) sourceVariableDeclarations(n *ast.Node) ([]*graph.Statement, *fenceError) {
	list := n.AsVariableDeclarationList()
	flags := n.Flags & ast.NodeFlagsBlockScoped
	if flags != ast.NodeFlagsLet && flags != ast.NodeFlagsConst {
		return nil, b.fenceWithMessage(n, "source var instantiation requires body hoisting contract")
	}
	var out []*graph.Statement
	for _, dn := range list.Declarations.Nodes {
		d := dn.AsVariableDeclaration()
		if d.Name() == nil || d.Name().Kind != ast.KindIdentifier || d.Initializer == nil {
			return nil, b.fenceWithMessage(dn, "source declaration requires simple initialized binding")
		}
		id, f := b.binding(d.Name())
		if f != nil {
			return nil, f
		}
		x, f := b.expression(d.Initializer)
		if f != nil {
			return nil, f
		}
		s := &graph.Statement{Kind: graph.StatementVariable, Position: b.position(dn), Binding: id, Name: d.Name().Text(), Mutable: flags == ast.NodeFlagsLet, Type: sourceUnclassified(), Value: x}
		site, _ := b.sourceRecovery.scope.ActualSite(dn)
		b.sourceRecovery.out.Statements[s] = site
		out = append(out, s)
	}
	return out, nil
}
func (b *builder) sourceExpression(n *ast.Node) (*graph.Expression, *fenceError, bool) {
	mk := func(kind graph.ExpressionKind) *graph.Expression {
		return &graph.Expression{Kind: kind, Position: b.position(n), Type: sourceUnclassified()}
	}
	child := func(n *ast.Node) (*graph.Expression, *fenceError) { return b.expression(n) }
	switch n.Kind {
	case ast.KindNumericLiteral, ast.KindStringLiteral, ast.KindNoSubstitutionTemplateLiteral, ast.KindTrueKeyword, ast.KindFalseKeyword:
		return nil, nil, false // Existing exact literal extraction.
	case ast.KindNullKeyword:
		x := mk(graph.ExpressionNull)
		x.Type = graph.Type{Kind: graph.TypeNull}
		return x, nil, true
	case ast.KindParenthesizedExpression:
		x, f := child(n.AsParenthesizedExpression().Expression)
		return x, f, true
	case ast.KindIdentifier:
		if b.checker.IsUndefinedSymbol(b.checker.GetSymbolAtLocation(n)) {
			return mk(graph.ExpressionUndefined), nil, true
		}
		id, f := b.binding(n)
		if f != nil {
			return nil, f, true
		}
		x := mk(graph.ExpressionIdentifier)
		x.Name = n.Text()
		x.Binding = id
		x.Pending = b.sourcePending(n, "lexical-get-value")
		return x, nil, true
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		x := mk(graph.ExpressionProperty)
		var receiver, key *ast.Node
		if n.Kind == ast.KindPropertyAccessExpression {
			p := n.AsPropertyAccessExpression()
			receiver = p.Expression
			x.Name = p.Name().Text()
		} else {
			p := n.AsElementAccessExpression()
			receiver = p.Expression
			key = p.ArgumentExpression
			x.Kind = graph.ExpressionIndex
		}
		var f *fenceError
		x.Receiver, f = child(receiver)
		if f != nil {
			return nil, f, true
		}
		if key != nil {
			x.Index, f = child(key)
			if f != nil {
				return nil, f, true
			}
		}
		x.Pending = b.sourcePending(n, "property-get")
		if link := optionalSourceLink(n); link != nil && (link.CheckReceiver || link.ContinuesReceiverChain) {
			x.OptionalLink = link
			x.OptionalChain = true
		}
		return x, nil, true
	case ast.KindCallExpression, ast.KindNewExpression:
		var callee *ast.Node
		var args []*ast.Node
		mode := "call"
		if n.Kind == ast.KindCallExpression {
			call := n.AsCallExpression()
			if call.QuestionDotToken != nil {
				return nil, b.fenceWithMessage(n, "optional call requires selected reference continuation"), true
			}
			callee = call.Expression
			args = call.Arguments.Nodes
		} else {
			call := n.AsNewExpression()
			callee = call.Expression
			if call.Arguments != nil {
				args = call.Arguments.Nodes
			}
			mode = "construct"
		}
		x := mk(graph.ExpressionSourceInvoke)
		x.Pending = b.sourcePending(n, mode)
		var f *fenceError
		x.Callee, f = child(callee)
		if f != nil {
			return nil, f, true
		}
		if x.Callee.OptionalChain {
			return nil, b.fenceWithMessage(n, "optional method invocation requires selected reference continuation"), true
		}
		for _, arg := range args {
			if arg.Kind == ast.KindSpreadElement {
				return nil, b.fenceWithMessage(arg, "source spread requires ordered iterator expansion"), true
			}
			a, f := child(arg)
			if f != nil {
				return nil, f, true
			}
			x.Arguments = append(x.Arguments, a)
		}
		if x.Callee.Kind == graph.ExpressionIdentifier {
			for _, imp := range b.sourceRecovery.scope.View().Imports {
				if graph.BindingID(imp.Local) == x.Callee.Binding {
					x.Pending.ImportLocal = imp.Local
					for _, instance := range b.sourceRecovery.scope.View().Instances {
						if instance.Module == imp.Target {
							x.Pending.Candidates = append(x.Pending.Candidates, instance.ID)
						}
					}
					x.Pending.Obligations = append(x.Pending.Obligations, "runtime import capture and selected callable/export cell proof unresolved")
					for _, cell := range b.sourceRecovery.scope.View().Cells {
						if cell.Module == imp.Target {
							x.Pending.CapturedCells = append(x.Pending.CapturedCells, cell.ID)
						}
					}
				}
			}
		}
		return x, nil, true
	case ast.KindConditionalExpression:
		d := n.AsConditionalExpression()
		x := mk(graph.ExpressionConditional)
		var f *fenceError
		x.Operand, f = child(d.Condition)
		if f != nil {
			return nil, f, true
		}
		x.Left, f = child(d.WhenTrue)
		if f != nil {
			return nil, f, true
		}
		x.Right, f = child(d.WhenFalse)
		return x, f, true
	case ast.KindBinaryExpression:
		d := n.AsBinaryExpression()
		op, ok := binaryOperator(d.OperatorToken.Kind)
		if d.OperatorToken.Kind == ast.KindEqualsToken {
			op = "="
			ok = true
		}
		if d.OperatorToken.Kind == ast.KindPlusEqualsToken {
			op = "+="
			ok = true
		}
		if d.OperatorToken.Kind == ast.KindQuestionQuestionToken {
			op = "??"
			ok = true
		}
		if !ok {
			return nil, b.fenceWithMessage(n, "source binary operator pending"), true
		}
		x := mk(graph.ExpressionBinary)
		x.Operator = op
		if op == "??" {
			x.Kind = graph.ExpressionNullish
		}
		if d.OperatorToken.Kind == ast.KindEqualsToken || d.OperatorToken.Kind == ast.KindPlusEqualsToken {
			x.Kind = graph.ExpressionAssignment
			x.Pending = b.sourcePending(n, "assignment")
		} else {
			x.Pending = b.sourcePending(n, "binary-coercion")
		}
		var f *fenceError
		x.Left, f = child(d.Left)
		if f != nil {
			return nil, f, true
		}
		x.Right, f = child(d.Right)
		return x, f, true
	case ast.KindObjectLiteralExpression:
		x := mk(graph.ExpressionObject)
		x.Pending = b.sourcePending(n, "fresh-object-construction")
		for _, pn := range n.AsObjectLiteralExpression().Properties.Nodes {
			var key string
			var vn *ast.Node
			switch pn.Kind {
			case ast.KindPropertyAssignment:
				key = pn.Name().Text()
				if pn.Name().Kind != ast.KindIdentifier && pn.Name().Kind != ast.KindStringLiteral {
					return nil, b.fenceWithMessage(pn, "computed source constructor key pending"), true
				}
				vn = pn.AsPropertyAssignment().Initializer
			case ast.KindShorthandPropertyAssignment:
				key = pn.Name().Text()
				vn = pn.Name()
			default:
				return nil, b.fenceWithMessage(pn, "source constructor member semantics pending"), true
			}
			v, f := child(vn)
			if f != nil {
				return nil, f, true
			}
			x.Properties = append(x.Properties, graph.PropertyValue{Name: key, Value: v})
		}
		return x, nil, true
	case ast.KindArrayLiteralExpression:
		x := mk(graph.ExpressionArray)
		x.Pending = b.sourcePending(n, "array-construction")
		for _, en := range n.AsArrayLiteralExpression().Elements.Nodes {
			v, f := child(en)
			if f != nil {
				return nil, f, true
			}
			x.Expressions = append(x.Expressions, v)
		}
		return x, nil, true
	}
	return nil, b.fenceWithMessage(n, fmt.Sprintf("source-only operation %s requires explicit ordered recovery", n.Kind)), true
}
