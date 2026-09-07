package extract

import (
	"context"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// ExtractAsyncFiles selects an explicit ambient host declaration by symbol.
// No implementation is replaced and ordinary graph extraction stays unchanged.
func ExtractAsyncFiles(entry string, sources map[string]string, entryName, hostName string) graph.AsyncResult {
	p, diagnostics := checked.New(entry, sources)
	if len(diagnostics) != 0 {
		return graph.AsyncResult{Diagnostics: diagnostics}
	}
	return ExtractAsyncChecked(entry, p, entryName, hostName)
}

// ExtractAsyncChecked preserves the configured project's checker and identities.
func ExtractAsyncChecked(entry string, p *checked.Program, entryName, hostName string) graph.AsyncResult {
	c, done := p.Compiler.GetTypeChecker(context.Background())
	defer done()
	b := &builder{sourcePath: entry, file: p.Entry, checker: c, bindings: make(map[*ast.Symbol]graph.BindingID), bindingTypes: make(map[graph.BindingID]graph.Type), nextBinding: 1, shapeIDs: make(map[*ast.Symbol]graph.ShapeID), shapeBuilding: make(map[*ast.Symbol]bool), moduleFiles: p.Files, entryFile: p.Entry, asyncThrow: true}
	fail := func(f *fenceError) graph.AsyncResult {
		return graph.AsyncResult{Diagnostics: []graph.Diagnostic{f.diagnostic}}
	}
	var handler, host *ast.Node
	for _, file := range p.RuntimeFiles {
		b.file = file
		if f := b.checkJSONCalls(file.AsNode()); f != nil {
			return fail(f)
		}
		for _, n := range file.Statements.Nodes {
			if n.Kind != ast.KindFunctionDeclaration || n.Name() == nil {
				continue
			}
			if n.Name().Text() == entryName && file == p.Entry {
				if handler != nil {
					return fail(b.fence(n))
				}
				handler = n
			}
			if n.Name().Text() == hostName {
				if host != nil {
					return fail(b.fence(n))
				}
				host = n
			}
		}
	}
	if handler == nil || host == nil || handler == host {
		return fail(b.fenceDiagnostic(p.Entry.AsNode(), "AsyncSignature", "async entry and host must name distinct function declarations"))
	}
	b.file = ast.GetSourceFileOfNode(host)
	hd := host.AsFunctionDeclaration()
	if hd.Body != nil || !hasModifier(hd.Modifiers(), ast.KindDeclareKeyword) || hd.TypeParameters != nil || hd.AsteriskToken != nil || len(hd.Parameters.Nodes) != 1 {
		return fail(b.fenceDiagnostic(host, "AsyncHost", "host must be an ambient read(string): Promise<string> declaration"))
	}
	hp, f := b.parameters(hd.Parameters.Nodes)
	if f != nil {
		return fail(f)
	}
	hr, f := b.asyncPromiseResult(hd.Type)
	if f != nil {
		return fail(f)
	}
	if hp[0].Default != nil || hp[0].BoundaryOptional || hp[0].Type.Kind != graph.TypeString || hr.Kind != graph.TypeString || hr.Optional {
		return fail(b.fenceDiagnostic(host, "AsyncHost", "host requires one string argument and Promise<string> result"))
	}
	hostBinding, f := b.binding(hd.Name())
	if f != nil {
		return fail(f)
	}

	helpers := map[*ast.Symbol]*graph.AsyncHelper{}
	var helperNodes []*ast.Node
	var asyncHelpers []*graph.AsyncHelper
	for _, file := range p.RuntimeFiles {
		b.file = file
		for _, n := range file.Statements.Nodes {
			if n == handler || n == host || n.Kind != ast.KindFunctionDeclaration || !hasModifier(n.Modifiers(), ast.KindAsyncKeyword) {
				continue
			}
			fn := n.AsFunctionDeclaration()
			if fn.Name() == nil || fn.Body == nil || fn.AsteriskToken != nil || fn.TypeParameters != nil {
				return fail(b.fenceDiagnostic(n, "AsyncHelper", "async helpers require named non-generic function declarations"))
			}
			params, f := b.parameters(fn.Parameters.Nodes)
			if f != nil {
				return fail(f)
			}
			for _, param := range params {
				if param.Type.Optional || (param.Type.Kind != graph.TypeString && param.Type.Kind != graph.TypeNumber && param.Type.Kind != graph.TypeBoolean) || (param.BoundaryOptional && param.Default == nil) {
					return fail(b.fenceDiagnostic(n, "AsyncHelper", "async helper parameters require scalars, optionally with scalar defaults"))
				}
			}
			result, f := b.asyncPromiseResult(fn.Type)
			if f != nil {
				return fail(f)
			}
			if result.Kind != graph.TypeString || result.Optional {
				return fail(b.fenceDiagnostic(n, "AsyncHelper", "async helper fulfillment must be a required string"))
			}
			binding, f := b.binding(fn.Name())
			if f != nil {
				return fail(f)
			}
			helper := &graph.AsyncHelper{Binding: binding, Name: fn.Name().Text(), Parameters: params, Program: &graph.AsyncProgram{Position: b.position(n), Result: result}}
			helpers[b.sourceSymbol(fn.Name())] = helper
			helperNodes = append(helperNodes, n)
			asyncHelpers = append(asyncHelpers, helper)
		}
	}
	var module []*graph.Statement
	for _, file := range p.RuntimeFiles {
		b.file = file
		for _, n := range file.Statements.Nodes {
			if n == handler || n == host || (n.Kind == ast.KindFunctionDeclaration && n.Name() != nil && helpers[b.sourceSymbol(n.Name())] != nil) {
				continue
			}
			ss, f := b.statement(n, true)
			if f != nil {
				return fail(f)
			}
			module = append(module, ss...)
		}
	}
	b.file = p.Entry
	fd := handler.AsFunctionDeclaration()
	if !hasModifier(fd.Modifiers(), ast.KindAsyncKeyword) || fd.Body == nil || fd.AsteriskToken != nil || fd.TypeParameters != nil {
		return fail(b.fenceDiagnostic(handler, "AsyncSignature", "entry must be an async function with a body"))
	}
	for _, modifier := range fd.Modifiers().Nodes {
		if modifier.Kind != ast.KindAsyncKeyword && modifier.Kind != ast.KindExportKeyword {
			return fail(b.fence(modifier))
		}
	}
	params, f := b.parameters(fd.Parameters.Nodes)
	if f != nil {
		return fail(f)
	}
	if len(params) != 1 || params[0].Type.Kind != graph.TypeObject || params[0].Type.Optional || params[0].BoundaryOptional || params[0].Default != nil {
		return fail(b.fenceDiagnostic(handler, "AsyncSignature", "async entry requires one required flat record input"))
	}
	result, f := b.asyncPromiseResult(fd.Type)
	if f != nil {
		return fail(f)
	}
	if result.Kind != graph.TypeObject || result.Optional {
		return fail(b.fenceDiagnostic(handler, "AsyncSignature", "async entry requires Promise of a flat response record"))
	}
	b.returnType = &result
	a := &graph.AsyncProgram{Position: b.position(handler), Input: params[0], Result: result}
	body := fd.Body.AsBlock().Statements.Nodes
	if len(body) == 1 && body[0].Kind == ast.KindTryStatement {
		tr := body[0].AsTryStatement()
		body = tr.TryBlock.AsBlock().Statements.Nodes
		if tr.CatchClause != nil {
			ca := tr.CatchClause.AsCatchClause()
			if ca.VariableDeclaration != nil {
				return fail(b.fenceDiagnostic(ca.VariableDeclaration, "AsyncCatch", "catch bindings are outside the string-rejection host boundary; use catch without binding"))
			}
			a.HasCatch = true
			a.Catch, f = b.statements(ca.Block.AsBlock().Statements.Nodes, false)
			if f != nil {
				return fail(f)
			}
		}
		if tr.FinallyBlock != nil {
			a.Finally, f = b.statements(tr.FinallyBlock.AsBlock().Statements.Nodes, false)
			if f != nil {
				return fail(f)
			}
		}
	}

	structured, f := b.asyncBody(body, a, b.sourceSymbol(hd.Name()), hostBinding, helpers)
	if f != nil {
		return fail(f)
	}
	if len(a.Stages) == 0 {
		return fail(b.fenceDiagnostic(handler, "AsyncAwait", "async entry requires at least one direct const initialization awaiting the selected host"))
	}
	if len(asyncHelpers) > 0 || asyncConditionalAwait(structured) {
		a.Flow = buildAsyncFlow(structured, a.Stages)
		pruneAsyncFlow(a)
	} else {
		var before []*graph.Statement
		stage := 0
		for _, s := range structured {
			if s.Kind == graph.StatementAsyncAwait {
				a.Stages[stage].Before = before
				before = nil
				stage++
			} else {
				before = append(before, s)
			}
		}
		a.After = before
	}

	for i, helper := range asyncHelpers {
		n := helperNodes[i]
		b.file = ast.GetSourceFileOfNode(n)
		b.returnType = &helper.Program.Result
		body := n.AsFunctionDeclaration().Body.AsBlock().Statements.Nodes
		// Cleanup belongs to each helper promise, separately from its caller.
		if len(body) == 1 && body[0].Kind == ast.KindTryStatement {
			tr := body[0].AsTryStatement()
			body = tr.TryBlock.AsBlock().Statements.Nodes
			if tr.CatchClause != nil {
				ca := tr.CatchClause.AsCatchClause()
				if ca.VariableDeclaration != nil {
					return fail(b.fenceDiagnostic(ca.VariableDeclaration, "AsyncCatch", "async helper catch bindings are unsupported"))
				}
				helper.Program.HasCatch = true
				helper.Program.Catch, f = b.statements(ca.Block.AsBlock().Statements.Nodes, false)
				if f != nil {
					return fail(f)
				}
			}
			if tr.FinallyBlock != nil {
				helper.Program.Finally, f = b.statements(tr.FinallyBlock.AsBlock().Statements.Nodes, false)
				if f != nil {
					return fail(f)
				}
			}
		}
		structured, f := b.asyncBody(body, helper.Program, b.sourceSymbol(hd.Name()), hostBinding, helpers)
		if f != nil {
			return fail(f)
		}
		helper.Program.Flow = buildAsyncFlow(structured, helper.Program.Stages)
		pruneAsyncFlow(helper.Program)
	}
	a.Module = &graph.Program{SourcePath: entry, Shapes: b.shapes, Statements: module}
	a.Helpers = asyncHelpers
	for _, helper := range asyncHelpers {
		helper.Program.Module = a.Module
	}
	if f := b.asyncAcyclic(a); f != nil {
		return fail(f)
	}
	return graph.AsyncResult{Program: a}
}

func (b *builder) asyncPromiseResult(node *ast.Node) (graph.Type, *fenceError) {
	if node == nil {
		return graph.Type{}, b.fenceDiagnostic(b.file.AsNode(), "AsyncSignature", "explicit Promise result annotation is required")
	}
	if node.Kind != ast.KindTypeReference {
		return graph.Type{}, b.fenceDiagnostic(node, "AsyncSignature", "result must use the intrinsic Promise type")
	}
	ref := node.AsTypeReferenceNode()
	symbol := b.sourceSymbol(ref.TypeName)
	if ref.TypeName.Kind != ast.KindIdentifier || ref.TypeName.Text() != "Promise" || symbol == nil || ref.TypeArguments == nil || len(ref.TypeArguments.Nodes) != 1 {
		return graph.Type{}, b.fenceDiagnostic(node, "AsyncSignature", "result must use the intrinsic Promise type")
	}
	for _, d := range symbol.Declarations {
		if b.ownsFile(ast.GetSourceFileOfNode(d)) {
			return graph.Type{}, b.fenceDiagnostic(node, "AsyncSignature", "user-defined Promise types are unsupported")
		}
	}
	promised := b.checker.GetPromisedTypeOfPromise(b.checker.GetTypeAtLocation(node))
	if promised == nil {
		return graph.Type{}, b.fence(node)
	}
	return b.graphType(promised, node)
}
