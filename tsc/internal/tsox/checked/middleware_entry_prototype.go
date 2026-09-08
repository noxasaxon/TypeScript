package checked

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/tsox/graph"
)

type callableSource struct {
	ID       string
	Position graph.Position
	Kind     string
}
type callableTemplate struct {
	Source     callableSource
	Async      bool
	Parameters []string
	Returns    []callableReturn
	node       *ast.Node
}
type callableReturn struct {
	Source callableSource
	Kind   string
	node   *ast.Node
}
type callableCell struct {
	ID      int
	Binding string
	Value   int
}
type callableReturnEdge struct {
	Source            callableSource
	Kind              string
	TargetInstance    int
	EvaluatedOperands []callableSource
}

type callableInstance struct {
	ID       int
	Template string
	Creation callableSource
	Captures []callableCell
	Returns  []callableReturnEdge
}
type callableValue struct {
	ID       int
	Kind     string
	Instance int
}
type callableOperation struct {
	ID     int
	Kind   string
	Source callableSource
	Inputs []int
	Output int
}
type callablePending struct {
	Source callableSource
	Reason string
}
type callableEntryPlan struct {
	Modules          []string
	ModuleStatements []callableSource
	Templates        []*callableTemplate
	Instances        []callableInstance
	Values           []callableValue
	Operations       []callableOperation
	EntryExport      callableSource
	EntryValue       int
	Pending          []callablePending
	// Deliberately not supplied by source identity discovery.
	StartupCertified bool
	moduleCells      map[*ast.Symbol]callableCell
}
type callableDiscovery struct {
	p            *Program
	c            *checker.Checker
	plan         *callableEntryPlan
	templates    map[*ast.Node]*callableTemplate
	functions    map[*ast.Symbol]*ast.Node
	module       map[*ast.Symbol]callableCell
	nextCell     int
	active       map[*ast.Node]bool
	environments map[int]map[*ast.Symbol]callableCell
}

func (d *callableDiscovery) source(n *ast.Node) callableSource {
	f := ast.GetSourceFileOfNode(n)
	pos := diagnostic(f, f.FileName(), n, "", "").Position
	pos.SourcePath = f.FileName()
	return callableSource{ID: fmt.Sprintf("%s@%d", f.FileName(), n.Pos()), Position: pos, Kind: n.Kind.String()}
}
func (d *callableDiscovery) symbol(n *ast.Node) *ast.Symbol {
	s := d.c.GetSymbolAtLocation(n)
	if s != nil && s.Flags&ast.SymbolFlagsAlias != 0 {
		s = d.c.GetAliasedSymbol(s)
	}
	return s
}
func (d *callableDiscovery) template(n *ast.Node) *callableTemplate {
	if prior := d.templates[n]; prior != nil {
		return prior
	}
	t := &callableTemplate{Source: d.source(n), node: n}
	if n.Modifiers() != nil {
		for _, m := range n.Modifiers().Nodes {
			if m.Kind == ast.KindAsyncKeyword {
				t.Async = true
			}
		}
	}
	for _, p := range n.Parameters() {
		t.Parameters = append(t.Parameters, d.source(p).ID)
	}
	var visit func(*ast.Node) bool
	visit = func(x *ast.Node) bool {
		if x == nil {
			return false
		}
		if x != n && (x.Kind == ast.KindArrowFunction || x.Kind == ast.KindFunctionExpression || x.Kind == ast.KindFunctionDeclaration) {
			d.template(x)
			return false
		}
		if x.Kind == ast.KindReturnStatement {
			value := x.AsReturnStatement().Expression
			kind := "return-value"
			if value != nil && value.Kind == ast.KindAwaitExpression {
				kind = "return-await"
			} else if value != nil && value.Kind == ast.KindCallExpression {
				kind = "return-call-pending-result"
			}
			t.Returns = append(t.Returns, callableReturn{Source: d.source(x), Kind: kind, node: x})
		}
		return x.ForEachChild(visit)
	}
	d.templates[n] = t
	d.plan.Templates = append(d.plan.Templates, t)
	if n.Body() != nil {
		visit(n.Body())
	}
	return t
}
func (d *callableDiscovery) value(kind string, instance int) int {
	id := len(d.plan.Values) + 1
	d.plan.Values = append(d.plan.Values, callableValue{id, kind, instance})
	return id
}
func (d *callableDiscovery) operation(kind string, n *ast.Node, inputs []int, output int) {
	d.plan.Operations = append(d.plan.Operations, callableOperation{len(d.plan.Operations) + 1, kind, d.source(n), inputs, output})
}
func (d *callableDiscovery) unknown(n *ast.Node, reason string) int {
	d.plan.Pending = append(d.plan.Pending, callablePending{d.source(n), reason})
	v := d.value("unresolved", 0)
	d.operation("retained-source-obligation", n, nil, v)
	return v
}
func (d *callableDiscovery) cell(n *ast.Node, value int) callableCell {
	d.nextCell++
	return callableCell{d.nextCell, d.source(n).ID, value}
}
func (d *callableDiscovery) create(n, creation *ast.Node, env map[*ast.Symbol]callableCell) int {
	t := d.template(n)
	instance := callableInstance{ID: len(d.plan.Instances) + 1, Template: t.Source.ID, Creation: d.source(creation)}
	captured := map[int]callableCell{}
	var visit func(*ast.Node) bool
	visit = func(x *ast.Node) bool {
		if x == nil {
			return false
		}
		if x.Kind == ast.KindIdentifier && !ast.IsDeclarationName(x) {
			if cell, ok := env[d.symbol(x)]; ok {
				captured[cell.ID] = cell
			}
		}
		return x.ForEachChild(visit)
	}
	if n.Body() != nil {
		visit(n.Body())
	}
	for _, id := range slices.Sorted(maps.Keys(captured)) {
		instance.Captures = append(instance.Captures, captured[id])
	}
	for _, ret := range t.Returns {
		value := ret.node.AsReturnStatement().Expression
		awaited := value != nil && value.Kind == ast.KindAwaitExpression
		if awaited {
			value = value.AsAwaitExpression().Expression
		}
		if value == nil || value.Kind != ast.KindCallExpression {
			continue
		}
		call := value.AsCallExpression()
		if call.Expression.Kind != ast.KindIdentifier {
			continue
		}
		cell, ok := env[d.symbol(call.Expression)]
		if !ok || cell.Value == 0 {
			continue
		}
		callee := d.plan.Values[cell.Value-1]
		if callee.Kind != "callable-instance" {
			continue
		}
		target := d.plan.Instances[callee.Instance-1]
		async := false
		for _, candidate := range d.plan.Templates {
			if candidate.Source.ID == target.Template {
				async = candidate.Async
			}
		}
		if !async {
			continue
		}
		kind := "return-promise-adoption"
		if awaited {
			kind = "return-await-promise"
		}
		edge := callableReturnEdge{Source: ret.Source, Kind: kind, TargetInstance: target.ID, EvaluatedOperands: []callableSource{d.source(call.Expression)}}
		for _, arg := range call.Arguments.Nodes {
			edge.EvaluatedOperands = append(edge.EvaluatedOperands, d.source(arg))
		}
		instance.Returns = append(instance.Returns, edge)
	}
	d.plan.Instances = append(d.plan.Instances, instance)
	d.environments[instance.ID] = maps.Clone(env)
	v := d.value("callable-instance", instance.ID)
	d.operation("create-callable", creation, nil, v)
	return v
}
func (d *callableDiscovery) eval(n *ast.Node, env map[*ast.Symbol]callableCell) int {
	if n == nil {
		return 0
	}
	switch n.Kind {
	case ast.KindParenthesizedExpression:
		return d.eval(n.AsParenthesizedExpression().Expression, env)
	case ast.KindIdentifier:
		if cell, ok := env[d.symbol(n)]; ok {
			d.operation("read-binding-cell", n, []int{cell.Value}, cell.Value)
			return cell.Value
		}
		return d.unknown(n, "callable source binding is not initialized in this environment")
	case ast.KindArrowFunction, ast.KindFunctionExpression:
		return d.create(n, n, env)
	case ast.KindCallExpression:
		call := n.AsCallExpression()
		callee := d.eval(call.Expression, env)
		inputs := []int{callee}
		for _, arg := range call.Arguments.Nodes {
			inputs = append(inputs, d.eval(arg, env))
		}
		d.operation("invoke-eager", n, inputs, 0)
		if callee == 0 || d.plan.Values[callee-1].Kind != "callable-instance" {
			return d.unknown(n, "callee instance unresolved")
		}
		instance := d.plan.Instances[d.plan.Values[callee-1].Instance-1]
		var template *callableTemplate
		for _, t := range d.plan.Templates {
			if t.Source.ID == instance.Template {
				template = t
			}
		}
		if template == nil || template.Async {
			return d.unknown(n, "async invocation returns a fresh promise; adoption requires async semantic integration")
		}
		if d.active[template.node] {
			return d.unknown(n, "recursive factory instance needs an inductive environment contract")
		}
		fn := template.node
		// Captures retain their creation environment, never the caller's locals.
		local := maps.Clone(d.module)
		for symbol, cell := range d.environments[instance.ID] {
			local[symbol] = cell
		}
		if len(fn.Parameters()) != len(inputs)-1 {
			return d.unknown(n, "defaults/ignored arity require evaluated binding operations")
		}
		for i, p := range fn.Parameters() {
			if p.Name() == nil || p.Name().Kind != ast.KindIdentifier || p.Initializer() != nil {
				return d.unknown(p, "factory parameter requires binding/default contract")
			}
			local[d.symbol(p.Name())] = d.cell(p, inputs[i+1])
		}
		d.active[fn] = true
		result := d.factoryBody(fn.Body(), local)
		delete(d.active, fn)
		d.operation("complete-factory", n, inputs, result)
		return result
	default:
		return d.unknown(n, "native initializer expression retained for constructor/effect/startup proof")
	}
}
func (d *callableDiscovery) factoryBody(body *ast.Node, env map[*ast.Symbol]callableCell) int {
	if body.Kind != ast.KindBlock {
		return d.eval(body, env)
	}
	for _, n := range body.AsBlock().Statements.Nodes {
		switch n.Kind {
		case ast.KindReturnStatement:
			v := d.eval(n.AsReturnStatement().Expression, env)
			d.operation("return-factory-value", n, []int{v}, v)
			return v
		case ast.KindVariableStatement:
			for _, decl := range n.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
				v := d.eval(decl.Initializer(), env)
				env[d.symbol(decl.Name())] = d.cell(decl, v)
				d.operation("define-factory-binding", decl, []int{v}, v)
			}
		default:
			d.unknown(n, "factory control/effect retained; candidate cannot certify normal completion")
		}
	}
	return 0
}
func discoverCallableEntry(p *Program, name string) (*callableEntryPlan, error) {
	c, done := p.Compiler.GetTypeChecker(context.Background())
	defer done()
	d := &callableDiscovery{p: p, c: c, plan: &callableEntryPlan{}, templates: map[*ast.Node]*callableTemplate{}, functions: map[*ast.Symbol]*ast.Node{}, module: map[*ast.Symbol]callableCell{}, active: map[*ast.Node]bool{}, environments: map[int]map[*ast.Symbol]callableCell{}}
	var entry *ast.Symbol
	for _, s := range c.GetExportsOfModule(p.Entry.AsNode().Symbol()) {
		if ast.SymbolName(s) == name {
			entry = s
			if entry.Flags&ast.SymbolFlagsAlias != 0 {
				entry = c.GetAliasedSymbol(entry)
			}
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("public export does not resolve")
	}
	for _, file := range p.RuntimeFiles {
		d.plan.Modules = append(d.plan.Modules, file.FileName())
		for _, n := range file.Statements.Nodes {
			d.plan.ModuleStatements = append(d.plan.ModuleStatements, d.source(n))
			if n.Kind == ast.KindFunctionDeclaration && n.Body() != nil {
				d.template(n)
				v := d.create(n, n, nil)
				s := d.symbol(n.Name())
				d.functions[s] = n
				d.module[s] = d.cell(n, v)
			}
		}
	}
	for _, file := range p.RuntimeFiles {
		for _, n := range file.Statements.Nodes {
			switch n.Kind {
			case ast.KindFunctionDeclaration, ast.KindImportDeclaration, ast.KindExportDeclaration, ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
				continue
			case ast.KindVariableStatement:
				for _, decl := range n.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
					v := d.eval(decl.Initializer(), d.module)
					s := d.symbol(decl.Name())
					d.module[s] = d.cell(decl, v)
					d.operation("define-module-binding", decl, []int{v}, v)
					if s == entry {
						d.plan.EntryExport = d.source(decl)
						d.plan.EntryValue = v
					}
				}
			default:
				d.unknown(n, "module startup operation retained; normal exit/effects are not certified")
			}
		}
	}
	if cell, ok := d.module[entry]; ok && d.plan.EntryValue == 0 {
		d.plan.EntryValue = cell.Value
		d.plan.EntryExport = d.source(entry.Declarations[0])
	}
	if d.plan.EntryValue == 0 {
		return nil, fmt.Errorf("entry value unavailable")
	}
	d.plan.moduleCells = maps.Clone(d.module)
	return d.plan, nil
}
