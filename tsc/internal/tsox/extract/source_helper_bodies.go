package extract

import (
	"context"
	"fmt"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
)

type SourceBodyNode struct {
	ID       string
	Position graph.Position
	Node     *ast.Node `json:"-"`
}
type SourceLayoutTransfer struct {
	Source      SourceBodyNode
	Destination graph.Type
	Value       *graph.Expression
	Obligation  string
}
type SourceHelperBodies struct {
	Statements      map[*graph.Statement]SourceBodyNode `json:"-"`
	BindingSources  map[graph.BindingID][]SourceBodyNode
	LayoutTransfers []SourceLayoutTransfer
	ModuleBindings  map[graph.BindingID]SourceBodyNode

	Program           *graph.Program
	Functions         map[graph.BindingID]SourceBodyNode
	Expressions       map[*graph.Expression]SourceBodyNode `json:"-"`
	ModuleOperations  []SourceBodyNode
	DeferredCallables []SourceBodyNode
	Obligations       []string
	Diagnostics       []graph.Diagnostic
}

func sourceHelperBodies(p *checked.Program) *SourceHelperBodies {
	out := &SourceHelperBodies{Statements: map[*graph.Statement]SourceBodyNode{}, ModuleBindings: map[graph.BindingID]SourceBodyNode{}, Functions: map[graph.BindingID]SourceBodyNode{}, Expressions: map[*graph.Expression]SourceBodyNode{}, Obligations: []string{"ordered module initialization normal exit and persistent cells unresolved", "each invocation requires evaluated input, capture, result and retention domains", "storage shapes and source constructors are not alias/freshness proofs", "async declarations and callable factories retain separate instance/body obligations"}}
	c, done := p.Compiler.GetTypeChecker(context.Background())
	defer done()
	b := &builder{scopedStatementSources: out.Statements, standardEntry: true, jsonValues: true, file: p.Entry, sourcePath: p.Entry.FileName(), checker: c, moduleFiles: p.Files, entryFile: p.Entry, bindings: map[*ast.Symbol]graph.BindingID{}, bindingTypes: map[graph.BindingID]graph.Type{}, nextBinding: 1, shapeIDs: map[*ast.Symbol]graph.ShapeID{}, shapeBuilding: map[*ast.Symbol]bool{}, asyncThrow: true}
	b.sourceBodies = out
	var statements []*graph.Statement
	for _, file := range p.RuntimeFiles {
		b.file = file
		for _, node := range file.Statements.Nodes {
			source := SourceBodyNode{ID: fmt.Sprintf("%s@%d", file.FileName(), node.Pos()), Position: b.position(node), Node: node}
			if node.Kind != ast.KindFunctionDeclaration {
				if node.Kind == ast.KindVariableStatement {
					for _, decl := range node.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes {
						if decl.Name() != nil && decl.Name().Kind == ast.KindIdentifier {
							id, f := b.binding(decl.Name())
							if f == nil {
								out.ModuleBindings[id] = b.sourceBodyNode(decl)
							}
						}
					}
				}
				out.ModuleOperations = append(out.ModuleOperations, source)
				continue
			}
			signatures := c.GetSignaturesOfType(c.GetTypeAtLocation(node.Name()), checker.SignatureKindCall)
			if hasModifier(node.Modifiers(), ast.KindAsyncKeyword) || len(signatures) != 1 || len(c.GetSignaturesOfType(c.GetReturnTypeOfSignature(signatures[0]), checker.SignatureKindCall)) != 0 {
				out.DeferredCallables = append(out.DeferredCallables, source)
				continue
			}
			fn, f := b.functionDeclaration(node)
			if f != nil {
				out.Diagnostics = append(out.Diagnostics, f.diagnostic)
				continue
			}
			out.Functions[fn.Binding] = source
			statements = append(statements, fn)
		}
	}
	out.BindingSources = b.sourceBindingIdentities()
	out.Program = &graph.Program{SourcePath: p.Entry.FileName(), Shapes: b.shapes, Statements: statements}
	return out
}

func (b *builder) sourceBodyNode(node *ast.Node) SourceBodyNode {
	file := ast.GetSourceFileOfNode(node)
	return SourceBodyNode{ID: fmt.Sprintf("%s@%d", file.FileName(), node.Pos()), Position: b.position(node), Node: node}
}

// sourceLayoutTransfer records a storage-compatibility candidate. It does not
// alter the actual RHS type or convert/copy its value. Native carrier, property
// order, initialized domains, optional presence, and alias obligations remain.
func (b *builder) sourceLayoutTransfer(node *ast.Node, target graph.Type, value *graph.Expression) bool {
	if b.sourceBodies == nil || target.Kind != graph.TypeObject || value.Type.Kind != graph.TypeObject || (value.Type.Optional && !target.Optional) {
		return false
	}
	a, c := target, value.Type
	a.Optional = false
	c.Optional = false
	if !b.sourceLayoutEqual(a, c) {
		return false
	}
	b.sourceBodies.LayoutTransfers = append(b.sourceBodies.LayoutTransfers, SourceLayoutTransfer{Source: b.sourceBodyNode(node), Destination: target, Value: value, Obligation: "native carrier, actual domain, property order, optional presence and alias-preserving transfer remain unresolved"})
	return true
}
func (b *builder) sourceLayoutEqual(a, c graph.Type) bool {
	if a.Kind != c.Kind || a.Optional != c.Optional {
		return false
	}
	switch a.Kind {
	case graph.TypeString, graph.TypeNumber, graph.TypeBoolean:
		return true
	case graph.TypeArray:
		return a.Element != nil && c.Element != nil && b.sourceLayoutEqual(*a.Element, *c.Element)
	case graph.TypeObject:
		x, ok := b.shapeByID(a.Shape)
		y, other := b.shapeByID(c.Shape)
		if !ok || !other || len(x.Fields) != len(y.Fields) {
			return false
		}
		for i, f := range x.Fields {
			g := y.Fields[i]
			if f.Name != g.Name || !b.sourceLayoutEqual(f.Type, g.Type) || (f.Literal == nil) != (g.Literal == nil) || (f.Literal != nil && *f.Literal != *g.Literal) {
				return false
			}
		}
		return true
	}
	return false
}

func sourceNullableEquality(operator string, left, right graph.Type) bool {
	if operator != "===" && operator != "!==" {
		return false
	}
	if left.Optional || right.Optional {
		return false
	}
	return (left.Kind == graph.TypeNullableString && (right.Kind == graph.TypeString || right.Kind == graph.TypeNullableString)) || (right.Kind == graph.TypeNullableString && left.Kind == graph.TypeString)
}

// Binding IDs are local to a body bundle. Cross-bundle linkage uses the same
// Program's actual declaration nodes, never numeric-ID coincidence or names.
func (b *builder) sourceBindingIdentities() map[graph.BindingID][]SourceBodyNode {
	out := map[graph.BindingID][]SourceBodyNode{}
	for symbol, id := range b.bindings {
		for _, declaration := range symbol.Declarations {
			if b.ownsFile(ast.GetSourceFileOfNode(declaration)) {
				out[id] = append(out[id], b.sourceBodyNode(declaration))
			}
		}
	}
	return out
}
