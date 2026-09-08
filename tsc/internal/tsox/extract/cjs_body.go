package extract

import (
	"context"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
)

type CJSWrapperCell struct {
	Module, Parameter   string
	NeedsRuntimeBinding bool
}
type CJSWrapperGraphUse struct {
	Source  SourceBodyNode
	Binding graph.BindingID
}
type CJSOrdinaryBody struct {
	Input                *checked.CJSCallableBodyInput
	StartupPlan          *checked.ModuleExportPlan
	ExportState          *checked.CJSExportState
	Source               *SourceHelperBodies
	Captures             map[graph.BindingID]checked.CJSBodyCell
	WrapperCells         map[graph.BindingID]CJSWrapperCell
	WrapperUses          []CJSWrapperGraphUse
	Syntax               []SourceBodyNode
	Operations           []CJSBodyOperation
	CheckedTypes         map[string]string
	NeedsInvocationProof bool
}

func sourceCJSOrdinaryBody(input *checked.CJSCallableBodyInput) *CJSOrdinaryBody {
	out := &CJSOrdinaryBody{Input: input, StartupPlan: input.State.Plan, ExportState: input.State, Captures: map[graph.BindingID]checked.CJSBodyCell{}, CheckedTypes: map[string]string{}, NeedsInvocationProof: true}
	p := input.Program
	c, done := p.Compiler.GetTypeChecker(context.Background())
	defer done()
	out.WrapperCells = map[graph.BindingID]CJSWrapperCell{}
	source := &SourceHelperBodies{ModuleBindings: map[graph.BindingID]SourceBodyNode{}, Functions: map[graph.BindingID]SourceBodyNode{}, Expressions: map[*graph.Expression]SourceBodyNode{}, Obligations: []string{"CJS source instance, runtime wrapper/environment cells and ordered startup remain required", "argument/result/receiver/domain, alias and callable-effect proof required before native admission"}}
	out.Source = source
	file := input.State.Plan.File
	b := &builder{standardEntry: true, jsonValues: true, file: file, sourcePath: file.FileName(), checker: c, moduleFiles: p.Files, entryFile: p.Entry, bindings: map[*ast.Symbol]graph.BindingID{}, bindingTypes: map[graph.BindingID]graph.Type{}, nextBinding: 1, shapeIDs: map[*ast.Symbol]graph.ShapeID{}, shapeBuilding: map[*ast.Symbol]bool{}, asyncThrow: true}
	b.sourceBodies = source
	node := input.Callable.Template.Node
	// Preserve complete actual syntax even when native checked extraction stops.
	var visit func(*ast.Node) bool
	visit = func(n *ast.Node) bool {
		out.Syntax = append(out.Syntax, b.sourceBodyNode(n))
		if operation := b.cjsBodyOperation(n); operation != nil {
			out.Operations = append(out.Operations, *operation)
		}
		return n.ForEachChild(visit)
	}
	visit(node)
	for _, cell := range input.Cells {
		id := b.nextBinding
		b.nextBinding++
		b.bindings[cell.Symbol] = id
		out.Captures[id] = cell
	}
	wrapperIDs := map[string]graph.BindingID{}
	for _, use := range input.Wrappers {
		id, ok := wrapperIDs[use.Parameter]
		if !ok {
			id = b.nextBinding
			b.nextBinding++
			wrapperIDs[use.Parameter] = id
			out.WrapperCells[id] = CJSWrapperCell{input.State.Plan.Module, use.Parameter, true}
		}
		out.WrapperUses = append(out.WrapperUses, CJSWrapperGraphUse{b.sourceBodyNode(use.Node), id})
	}
	for _, operation := range input.State.Plan.Initialization {
		if operation.Phase != "instantiate-function" {
			source.ModuleOperations = append(source.ModuleOperations, b.sourceBodyNode(operation.Node))
		}
	}
	if node.Parameters() != nil {
		for _, parameter := range node.Parameters() {
			if parameter.Name() != nil {
				out.CheckedTypes[parameter.Name().Text()] = c.TypeToString(c.GetTypeAtLocation(parameter.Name()))
			}
		}
	}
	var statements []*graph.Statement
	if len(input.Obligations) > 0 {
		source.Diagnostics = append(source.Diagnostics, b.fenceWithMessage(node, "CJS wrapper identity/order obligation precedes body admission").diagnostic)
	} else if len(input.Wrappers) > 0 {
		source.Diagnostics = append(source.Diagnostics, b.fenceWithMessage(input.Wrappers[0].Node, "CJS wrapper cell needs typed runtime storage before ordinary native body admission").diagnostic)
	} else if node.Kind != ast.KindFunctionDeclaration {
		source.Diagnostics = append(source.Diagnostics, b.fenceWithMessage(node, "synchronous callable expression requires actual invocation/this/environment adapter").diagnostic)
	} else {
		fn, f := b.functionDeclaration(node)
		if f != nil {
			source.Diagnostics = append(source.Diagnostics, f.diagnostic)
		} else {
			source.Functions[fn.Binding] = b.sourceBodyNode(node)
			statements = append(statements, fn)
		}
	}
	source.BindingSources = b.sourceBindingIdentities()
	source.Program = &graph.Program{SourcePath: file.FileName(), Shapes: b.shapes, Statements: statements}
	return out
}
