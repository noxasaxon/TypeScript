package checked

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/scanner"
)

// All identities here describe distinct evaluated source instances within this
// plan. ProgramInstance supplies runtime ownership; a source site is not unique.
type CJSStartupCell struct {
	ID        int
	Binding   SourceNodeID
	Symbol    *ast.Symbol `json:"-"`
	Value     CJSStateValue
	Immutable bool
}
type CJSStartupEnvironment struct {
	ID, Parent int
	Strict     bool
	Creation   SourceNodeID
	Cells      []int
	parent     *CJSStartupEnvironment
	bindings   map[*ast.Symbol]*CJSStartupCell
}
type CJSStartupCallable struct {
	ID                                  int
	Template                            FunctionSource
	Environment, Object                 int
	Constructible                       bool
	Strict                              bool
	NeedsNativeCreation, NeedsBodyProof bool
}
type CJSStartupInvocation struct {
	Source                         SourceNodeID
	Callee                         CJSStateValue
	Environment                    int
	Body                           []SourceNodeID
	Executed                       []SourceNodeID
	Result                         CJSStateValue
	Complete, NeedsNativeExecution bool
}

func (b *cjsStateBuilder) environment(parent *CJSStartupEnvironment, n *ast.Node) *CJSStartupEnvironment {
	env := &CJSStartupEnvironment{ID: len(b.out.Environments), Parent: -1, Creation: sourceNodeID(b.out.Plan.File, n), parent: parent, bindings: map[*ast.Symbol]*CJSStartupCell{}}
	if parent != nil {
		env.Parent = parent.ID
		env.Strict = parent.Strict
	} else {
		env.Strict = b.out.Plan.Strict
	}
	b.out.Environments = append(b.out.Environments, env)
	return env
}
func (b *cjsStateBuilder) lookup(symbol *ast.Symbol) *CJSStartupCell {
	if symbol == nil {
		return nil
	}
	for env := b.env; env != nil; env = env.parent {
		if cell := env.bindings[symbol]; cell != nil {
			return cell
		}
	}
	return nil
}
func (b *cjsStateBuilder) bind(symbol *ast.Symbol, value CJSStateValue, immutable bool, n *ast.Node) {
	if symbol == nil {
		b.fail(n, "binding has no actual source symbol")
		return
	}
	cell := b.env.bindings[symbol]
	if cell == nil {
		cell = &CJSStartupCell{ID: len(b.out.Cells), Binding: sourceNodeID(b.out.Plan.File, n), Symbol: symbol}
		b.out.Cells = append(b.out.Cells, cell)
		b.env.bindings[symbol] = cell
		b.env.Cells = append(b.env.Cells, cell.ID)
	}
	cell.Value = value
	cell.Immutable = immutable
	kind := "initialize-binding-cell"
	if b.instantiating {
		kind = "instantiate-binding-cell"
	}
	b.out.Effects = append(b.out.Effects, CJSStartupEffect{Source: sourceNodeID(b.out.Plan.File, n), Kind: kind, Environment: b.env.ID, Cell: &cell.ID, Value: value, NeedsNativeExecution: true})
}
func cjsSynchronous(n *ast.Node) bool {
	if n.Modifiers() != nil {
		for _, m := range n.Modifiers().Nodes {
			if m.Kind == ast.KindAsyncKeyword {
				return false
			}
		}
	}
	if n.Kind == ast.KindFunctionDeclaration && n.AsFunctionDeclaration().AsteriskToken != nil {
		return false
	}
	if n.Kind == ast.KindFunctionExpression && n.AsFunctionExpression().AsteriskToken != nil {
		return false
	}
	return true
}
func (b *cjsStateBuilder) callable(n *ast.Node) CJSStateValue {
	template, ok := sourceFunction(b.out.Plan.File, n, b.checker)
	if !ok {
		return b.fail(n, "callable requires actual implementation body")
	}
	id := len(b.out.Instances)
	value := CJSStateValue{Kind: "function", Instance: id, Function: &template.Body}
	object := b.object(n)
	constructible := n.Kind != ast.KindArrowFunction && cjsSynchronous(n)
	b.out.Instances = append(b.out.Instances, CJSStartupCallable{ID: id, Template: template, Environment: b.env.ID, Object: object.Object, Constructible: constructible, NeedsNativeCreation: true, NeedsBodyProof: true, Strict: b.env.Strict || b.strictBody(n.Body())})
	b.out.Objects[object.Object].Prototype = "FunctionPrototype"
	if constructible {
		prototype := b.object(n)
		b.out.Objects[prototype.Object].Prototype = "ObjectPrototype"
		b.out.Objects[prototype.Object].Own["constructor"] = value
		b.out.Objects[prototype.Object].Descriptors["constructor"] = CJSDataAttributes{true, false, true}
		b.out.Objects[object.Object].Own["prototype"] = prototype
		b.out.Objects[object.Object].Descriptors["prototype"] = CJSDataAttributes{true, false, false}
	}
	return value
}
func (b *cjsStateBuilder) functionObject(value CJSStateValue, n *ast.Node) CJSStateValue {
	if value.Instance < 0 || value.Instance >= len(b.out.Instances) {
		return b.fail(n, "function has no evaluated instance")
	}
	return CJSStateValue{Kind: "object", Object: b.out.Instances[value.Instance].Object}
}
func (b *cjsStateBuilder) instantiate(statements []*ast.Node) {
	prior := b.instantiating
	b.instantiating = true
	defer func() { b.instantiating = prior }()
	for _, n := range statements {
		if n.Kind == ast.KindFunctionDeclaration {
			f, ok := sourceFunction(b.out.Plan.File, n, b.checker)
			if !ok {
				b.fail(n, "function declaration lacks body")
				return
			}
			b.bind(f.Symbol, b.function(n), false, n.Name())
		}
	}
	// Unsupported nested scopes/control flow are never treated as successful
	// startup. This bounded lane instantiates direct declarations only.
	for _, n := range statements {
		if n.Kind != ast.KindVariableStatement {
			continue
		}
		list := n.AsVariableStatement().DeclarationList.AsVariableDeclarationList()
		if list.Flags&ast.NodeFlagsBlockScoped != 0 {
			continue
		}
		for _, item := range list.Declarations.Nodes {
			d := item.AsVariableDeclaration()
			if d.Name().Kind != ast.KindIdentifier {
				b.fail(item, "binding pattern needs actual ordered instantiation")
				return
			}
			symbol := b.checker.GetSymbolAtLocation(d.Name())
			if b.env.bindings[symbol] == nil {
				b.bind(symbol, CJSStateValue{Kind: "undefined"}, false, d.Name())
			}
		}
	}
}

// Source-completion transfer only. Every original statement is retained in the
// plan/invocation body. Unsupported operations leave normal exit unproved.
func (b *cjsStateBuilder) startupStatement(n *ast.Node) (CJSStateValue, bool) {
	switch n.Kind {
	case ast.KindEmptyStatement: // Retained in source plan; no evaluation effect.
	case ast.KindFunctionDeclaration: // Instantiated separately.
	case ast.KindExpressionStatement:
		b.value(n.AsExpressionStatement().Expression)
	case ast.KindReturnStatement:
		return b.value(n.AsReturnStatement().Expression), true
	case ast.KindVariableStatement:
		list := n.AsVariableStatement().DeclarationList.AsVariableDeclarationList()
		for _, item := range list.Declarations.Nodes {
			d := item.AsVariableDeclaration()
			if d.Name().Kind != ast.KindIdentifier {
				b.fail(item, "binding pattern needs actual ordered lowering")
				break
			}
			if d.Name().Text() == "module" || d.Name().Text() == "exports" {
				b.fail(item, "wrapper/local collision needs explicit binding instantiation")
				break
			}
			symbol := b.checker.GetSymbolAtLocation(d.Name())
			if d.Initializer != nil {
				value := b.value(d.Initializer)
				b.bind(symbol, value, list.Flags&ast.NodeFlagsConst != 0, d.Name())
			} else if b.env.bindings[symbol] == nil {
				b.bind(symbol, CJSStateValue{Kind: "undefined"}, list.Flags&ast.NodeFlagsConst != 0, d.Name())
			}
		}
	default:
		b.fail(n, "statement requires shared startup control-flow proof")
	}
	return CJSStateValue{Kind: "undefined"}, false
}
func (b *cjsStateBuilder) startupCall(n *ast.Node) CJSStateValue {
	call := n.AsCallExpression()
	if property := call.Expression; property.Kind == ast.KindPropertyAccessExpression && property.AsPropertyAccessExpression().Name().Text() == "defineProperty" {
		return b.definePropertyCall(n)
	}
	if value, handled := b.objectCreateNull(n); handled {
		return value
	}
	callee := b.value(call.Expression)
	if !b.out.Complete {
		return CJSStateValue{Kind: "unknown"}
	}
	if callee.Kind != "function" || callee.Instance >= len(b.out.Instances) {
		return b.fail(n, "call needs an actual evaluated callable instance")
	}
	instance := b.out.Instances[callee.Instance]
	template := instance.Template.Node
	if (call.Arguments != nil && len(call.Arguments.Nodes) != 0) || len(template.Parameters()) != 0 || !cjsSynchronous(template) {
		return b.fail(n, "startup invocation needs argument/default/async or generator completion proof")
	}
	if b.active[template] {
		return b.fail(n, "recursive startup needs a finite invocation/completion proof")
	}
	b.active[template] = true
	defer delete(b.active, template)
	prior := b.env
	b.env = b.environment(b.out.Environments[instance.Environment], n)
	b.env.Strict = instance.Strict
	defer func() { b.env = prior }()
	invocation := CJSStartupInvocation{Source: sourceNodeID(b.out.Plan.File, n), Callee: callee, Environment: b.env.ID, NeedsNativeExecution: true}
	index := len(b.out.Invocations)
	b.out.Invocations = append(b.out.Invocations, invocation)
	body := template.Body()
	result := CJSStateValue{Kind: "undefined"}
	if body.Kind != ast.KindBlock {
		invocation.Body = []SourceNodeID{sourceNodeID(b.out.Plan.File, body)}
		invocation.Executed = invocation.Body
		result = b.value(body)
	} else {
		statements := body.AsBlock().Statements.Nodes
		for _, statement := range statements {
			invocation.Body = append(invocation.Body, sourceNodeID(b.out.Plan.File, statement))
		}
		b.instantiate(statements)
		for _, statement := range statements {
			if !b.out.Complete {
				break
			}
			invocation.Executed = append(invocation.Executed, sourceNodeID(b.out.Plan.File, statement))
			value, returned := b.startupStatement(statement)
			if returned {
				result = value
				break
			}
		}
	}
	invocation.Result = result
	invocation.Complete = b.out.Complete
	b.out.Invocations[index] = invocation
	return result
}
func (b *cjsStateBuilder) intrinsicCapture(n *ast.Node) (CJSStateValue, bool) {
	property := n.AsPropertyAccessExpression()
	if property.Name().Text() != "toString" || property.Expression.Kind != ast.KindPropertyAccessExpression {
		return CJSStateValue{}, false
	}
	prototype := property.Expression.AsPropertyAccessExpression()
	if prototype.Name().Text() != "prototype" || prototype.Expression.Kind != ast.KindIdentifier || prototype.Expression.Text() != "Object" {
		return CJSStateValue{}, false
	}
	if !b.out.Boundary.IntrinsicObjectPrototypeToString || !cjsBundledSymbol(b.checker.GetSymbolAtLocation(prototype.Expression)) || !cjsBundledSymbol(b.checker.GetSymbolAtLocation(prototype.Name())) || !cjsBundledSymbol(b.checker.GetSymbolAtLocation(property.Name())) {
		return b.fail(n, "Object.prototype.toString capture needs exact intrinsic/realm proof"), true
	}
	site := sourceNodeID(b.out.Plan.File, n)
	value := CJSStateValue{Kind: "intrinsic", Intrinsic: "Object.prototype.toString", Literal: &site}
	b.out.Effects = append(b.out.Effects, CJSStartupEffect{Source: site, Kind: "capture-intrinsic-data-property", Environment: b.env.ID, Receiver: sourceNodeID(b.out.Plan.File, property.Expression), Target: value, NeedsNativeExecution: true})
	return value, true
}
func (b *cjsStateBuilder) objectCreateNull(n *ast.Node) (CJSStateValue, bool) {
	call := n.AsCallExpression()
	if call.Expression.Kind != ast.KindPropertyAccessExpression {
		return CJSStateValue{}, false
	}
	property := call.Expression.AsPropertyAccessExpression()
	if property.Name().Text() != "create" || property.Expression.Kind != ast.KindIdentifier || property.Expression.Text() != "Object" {
		return CJSStateValue{}, false
	}
	if !b.out.Boundary.IntrinsicObjectCreate || !cjsBundledSymbol(b.checker.GetSymbolAtLocation(property.Expression)) || !cjsBundledSymbol(b.checker.GetSymbolAtLocation(property.Name())) {
		return b.fail(n, "Object.create requires exact intrinsic/realm proof"), true
	}
	if call.Arguments == nil || len(call.Arguments.Nodes) != 1 {
		return b.fail(n, "Object.create descriptor/prototype arguments need actual semantics"), true
	}
	argument := call.Arguments.Nodes[0]
	prototype := b.value(argument)
	if prototype.Kind != "literal" || prototype.Literal == nil || prototype.Literal.Kind != "KindNullKeyword" {
		return b.fail(n, "Object.create startup supports only actual null prototype"), true
	}
	value := b.object(n)
	b.out.Objects[value.Object].Prototype = "null"
	b.out.Effects = append(b.out.Effects, CJSStartupEffect{Source: sourceNodeID(b.out.Plan.File, n), Kind: "create-null-prototype-object", Environment: b.env.ID, Callee: sourceNodeID(b.out.Plan.File, call.Expression), Receiver: sourceNodeID(b.out.Plan.File, property.Expression), Arguments: []SourceNodeID{sourceNodeID(b.out.Plan.File, argument)}, Target: value, NeedsNativeExecution: true})
	return value, true
}

func (b *cjsStateBuilder) strictBody(body *ast.Node) bool {
	if body == nil || body.Kind != ast.KindBlock {
		return false
	}
	for _, statement := range body.AsBlock().Statements.Nodes {
		if !ast.IsPrologueDirective(statement) {
			break
		}
		value := statement.AsExpressionStatement().Expression
		text := b.out.Plan.File.Text()[scanner.GetTokenPosOfNode(value, b.out.Plan.File, false):value.End()]
		if text == `"use strict"` || text == "'use strict'" {
			return true
		}
	}
	return false
}

// Environment cells preserve original checker-symbol identity. These are
// conservative lexical cells, not a minimal capture set or a NativeData proof.
func (s *CJSExportState) CallableEnvironmentCells(name string) ([]*CJSStartupCell, bool) {
	ref, ok := s.CallableReference(name)
	if !ok {
		return nil, false
	}
	var cells []*CJSStartupCell
	for env := s.Environments[ref.Environment]; env != nil; env = env.parent {
		for _, id := range env.Cells {
			cells = append(cells, s.Cells[id])
		}
	}
	return cells, true
}
