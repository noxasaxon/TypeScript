package extract

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/tsox/graph"
)

func (b *builder) asyncBody(nodes []*ast.Node, a *graph.AsyncProgram, hostSymbol *ast.Symbol, hostBinding graph.BindingID, helpers map[*ast.Symbol]*graph.AsyncHelper) ([]*graph.Statement, *fenceError) {
	var extractBody func([]*ast.Node) ([]*graph.Statement, *fenceError)
	extractBody = func(nodes []*ast.Node) ([]*graph.Statement, *fenceError) {
		var before []*graph.Statement
		for _, n := range nodes {
			if n.Kind == ast.KindExpressionStatement && n.AsExpressionStatement().Expression.Kind == ast.KindAwaitExpression {
				awaited := n.AsExpressionStatement().Expression.AsAwaitExpression().Expression
				if producer, fulfilled, f, handled := b.standardProducer(awaited); handled {
					if f != nil {
						return nil, f
					}
					binding := b.nextBinding
					b.nextBinding++
					b.bindingTypes[binding] = fulfilled
					operation := graph.AsyncAwait{Position: b.position(n), Binding: binding, Name: "ignored_await", Type: fulfilled, Producer: producer}
					a.Stages = append(a.Stages, graph.AsyncStage{Await: operation})
					before = append(before, &graph.Statement{Kind: graph.StatementAsyncAwait, Position: b.position(n), Binding: binding, Name: operation.Name, Type: fulfilled, Producer: producer})
					continue
				}
			}
			if n.Kind == ast.KindTryStatement {
				tr := n.AsTryStatement()
				region := &graph.AsyncProtectedRegion{Position: b.position(n)}
				var f *fenceError
				region.Try, f = extractBody(tr.TryBlock.AsBlock().Statements.Nodes)
				if f != nil {
					return nil, f
				}
				if tr.CatchClause != nil {
					clause := tr.CatchClause.AsCatchClause()
					region.HasCatch = true
					if clause.VariableDeclaration != nil {
						region.CatchBinding, f = b.errorCatchBinding(clause.VariableDeclaration)
						if f != nil {
							return nil, f
						}
					}
					region.Catch, f = extractBody(clause.Block.AsBlock().Statements.Nodes)
					if f != nil {
						return nil, f
					}
				}
				if tr.FinallyBlock != nil {
					region.HasFinally = true
					region.Finally, f = extractBody(tr.FinallyBlock.AsBlock().Statements.Nodes)
					if f != nil {
						return nil, f
					}
				}
				before = append(before, &graph.Statement{Kind: graph.StatementAsyncProtected, Position: b.position(n), Protected: region})
				continue
			}
			if n.Kind == ast.KindForStatement || n.Kind == ast.KindWhileStatement {
				loop, f := b.loopStatement(n, func(node *ast.Node) ([]*graph.Statement, *fenceError) {
					if node.Kind == ast.KindBlock {
						return extractBody(node.AsBlock().Statements.Nodes)
					}
					return extractBody([]*ast.Node{node})
				})
				if f != nil {
					return nil, f
				}
				before = append(before, loop...)
				continue
			}
			if n.Kind == ast.KindIfStatement {
				branch := n.AsIfStatement()
				condition, f := b.expression(branch.Expression)
				if f != nil {
					return nil, f
				}
				arm := func(node *ast.Node) ([]*graph.Statement, *fenceError) {
					if node == nil {
						return nil, nil
					}
					if node.Kind == ast.KindBlock {
						return extractBody(node.AsBlock().Statements.Nodes)
					}
					return extractBody([]*ast.Node{node})
				}
				left, f := arm(branch.ThenStatement)
				if f != nil {
					return nil, f
				}
				right, f := arm(branch.ElseStatement)
				if f != nil {
					return nil, f
				}
				before = append(before, &graph.Statement{Kind: graph.StatementIf, Position: b.position(n), Condition: condition, Then: left, Else: right})
				continue
			}
			if n.Kind == ast.KindVariableStatement {
				ds := n.AsVariableStatement().DeclarationList.AsVariableDeclarationList()
				if len(ds.Declarations.Nodes) == 1 {
					d := ds.Declarations.Nodes[0].AsVariableDeclaration()
					if d.Initializer != nil && d.Initializer.Kind == ast.KindAwaitExpression {
						if d.Name().Kind != ast.KindIdentifier || ds.Flags&ast.NodeFlagsConst == 0 {
							return nil, b.fenceDiagnostic(n, "AsyncAwait", "await must initialize a const identifier")
						}
						callNode := d.Initializer.AsAwaitExpression().Expression
						if callNode.Kind != ast.KindCallExpression {
							return nil, b.fenceDiagnostic(callNode, "AsyncHost", "await requires the selected host operation")
						}
						if producer, fulfilled, f, handled := b.standardProducer(callNode); handled {
							if f != nil {
								return nil, f
							}
							binding, f := b.binding(d.Name())
							if f != nil {
								return nil, f
							}
							b.bindingTypes[binding] = fulfilled
							operation := graph.AsyncAwait{Position: b.position(d.Initializer), Binding: binding, Name: d.Name().Text(), Type: fulfilled, Producer: producer}
							a.Stages = append(a.Stages, graph.AsyncStage{Await: operation})
							before = append(before, &graph.Statement{Kind: graph.StatementAsyncAwait, Binding: binding, Name: operation.Name, Position: operation.Position, Type: fulfilled, Producer: producer})
							continue
						}
						call := callNode.AsCallExpression()
						if call.Expression.Kind != ast.KindIdentifier || call.QuestionDotToken != nil || call.TypeArguments != nil {
							return nil, b.fenceDiagnostic(callNode, "AsyncCall", "await requires a direct checker-resolved host or async helper call")
						}
						var arg *graph.Expression
						var helper graph.BindingID
						fulfilled := graph.Type{Kind: graph.TypeString}
						if b.sourceSymbol(call.Expression) == hostSymbol {
							if len(call.Arguments.Nodes) != 1 {
								return nil, b.fenceDiagnostic(callNode, "AsyncHost", "host requires one string argument")
							}
							var f *fenceError
							arg, f = b.expression(call.Arguments.Nodes[0])
							if f != nil {
								return nil, f
							}
							if arg.Type.Kind != graph.TypeString || arg.Type.Optional {
								return nil, b.fence(call.Arguments.Nodes[0])
							}
						} else if target := helpers[b.sourceSymbol(call.Expression)]; target != nil {
							helper = target.Binding
							fulfilled = target.Program.Result
							var f *fenceError
							arg, f = b.asyncHelperArguments(callNode, target)
							if f != nil {
								return nil, f
							}
						} else {
							return nil, b.fenceDiagnostic(callNode, "AsyncHost", "await requires the selected host or a direct async helper declaration")
						}
						binding, f := b.binding(d.Name())
						if f != nil {
							return nil, f
						}
						b.bindingTypes[binding] = fulfilled
						operation := graph.AsyncAwait{Position: b.position(d.Initializer), Host: hostBinding, Helper: helper, Binding: binding, Name: d.Name().Text(), Type: fulfilled, Argument: arg}
						a.Stages = append(a.Stages, graph.AsyncStage{Await: operation})
						before = append(before, &graph.Statement{Kind: graph.StatementAsyncAwait, Binding: binding, Name: operation.Name, Position: operation.Position, Type: fulfilled, Value: arg})
						continue
					}
				}
			}
			ss, f := b.statement(n, false)
			if f != nil {
				return nil, f
			}
			before = append(before, ss...)
		}

		return before, nil
	}
	return extractBody(nodes)
}

func (b *builder) asyncHelperArguments(node *ast.Node, helper *graph.AsyncHelper) (*graph.Expression, *fenceError) {
	call := node.AsCallExpression()
	if len(call.Arguments.Nodes) > len(helper.Parameters) {
		return nil, b.fenceDiagnostic(node, "AsyncCall", "async helper arity is unsupported")
	}
	args := make([]*graph.Expression, len(helper.Parameters))
	types := make([]graph.Type, len(args))
	for i, param := range helper.Parameters {
		typ := param.Type
		typ.Optional = param.BoundaryOptional
		types[i] = typ
		if i >= len(call.Arguments.Nodes) {
			if param.Default == nil {
				return nil, b.fenceDiagnostic(node, "AsyncCall", "async helper requires its scalar arguments")
			}
			args[i] = &graph.Expression{Kind: graph.ExpressionUndefined, Type: typ, Position: b.position(node)}
		} else {
			arg, f := b.expression(call.Arguments.Nodes[i])
			if f != nil {
				return nil, f
			}
			if !slotAccepts(typ, arg) {
				return nil, b.typeFlowFence(call.Arguments.Nodes[i], typ, arg.Type)
			}
			adaptUndefinedToSlot(typ, arg)
			args[i] = arg
		}
	}
	result := helper.Program.Result
	return &graph.Expression{Kind: graph.ExpressionCall, Position: b.position(node), Type: result, Callee: &graph.Expression{Kind: graph.ExpressionIdentifier, Binding: helper.Binding, Type: graph.FunctionType(types, result), Position: b.position(call.Expression)}, Arguments: args}, nil
}

// Reject cycles before recursive owned continuation types are constructed.
func (b *builder) asyncAcyclic(a *graph.AsyncProgram) *fenceError {
	helpers := map[graph.BindingID]*graph.AsyncHelper{}
	for _, h := range a.Helpers {
		helpers[h.Binding] = h
	}
	active, done := map[graph.BindingID]bool{}, map[graph.BindingID]bool{}
	var visit func(*graph.AsyncProgram) *fenceError
	visit = func(p *graph.AsyncProgram) *fenceError {
		for _, stage := range p.Stages {
			id := stage.Await.Helper
			if id == 0 {
				continue
			}
			if active[id] {
				pos := stage.Await.Position
				return &fenceError{diagnostic: graph.Diagnostic{SourcePath: pos.SourcePath, Position: pos, Construct: "AsyncRecursion", Message: "recursive async helper calls require an unbounded continuation and are unsupported"}}
			}
			if done[id] {
				continue
			}
			active[id] = true
			if f := visit(helpers[id].Program); f != nil {
				return f
			}
			delete(active, id)
			done[id] = true
		}
		return nil
	}
	if f := visit(a); f != nil {
		return f
	}
	for _, helper := range a.Helpers {
		active[helper.Binding] = true
		if f := visit(helper.Program); f != nil {
			return f
		}
		delete(active, helper.Binding)
	}
	return nil
}
