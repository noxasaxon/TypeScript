package checked

import (
	"fmt"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// lexicalContext excludes member/export-name syntax before symbol lookup. A
// computed property's expression and shorthand value are still lexical uses.
func lexicalContext(n *ast.Node) bool {
	if n == nil || n.Kind != ast.KindIdentifier {
		return false
	}
	p := n.Parent
	if p == nil {
		return false
	}
	for a := p; a != nil; a = a.Parent {
		if a.Kind == ast.KindImportDeclaration || a.Kind == ast.KindSourceFile {
			break
		}
		if a.IsTypeOnly() {
			return false
		}
	}
	switch p.Kind {
	case ast.KindPropertyAccessExpression:
		return p.AsPropertyAccessExpression().Name() != n
	case ast.KindPropertyAssignment, ast.KindPropertySignature, ast.KindPropertyDeclaration, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindGetAccessor, ast.KindSetAccessor:
		return p.Name() != n
	case ast.KindBindingElement:
		return p.Name() == n
	case ast.KindImportSpecifier:
		return p.Name() == n && !p.AsImportSpecifier().IsTypeOnly
	case ast.KindExportSpecifier:
		return p.PropertyNameOrName() == n && p.Parent.Parent.ModuleSpecifier() == nil && !p.AsExportSpecifier().IsTypeOnly
	case ast.KindTypeParameter, ast.KindTypeAliasDeclaration, ast.KindInterfaceDeclaration:
		return p.Name() != n
	}
	return true
}
func lexicalSymbol(n *ast.Node, c *checker.Checker) *ast.Symbol {
	if !lexicalContext(n) {
		return nil
	}
	if n.Parent.Kind == ast.KindShorthandPropertyAssignment {
		return c.GetShorthandAssignmentValueSymbol(n.Parent)
	}
	if n.Parent.Kind == ast.KindExportSpecifier {
		return c.GetExportSpecifierLocalTargetSymbol(n.Parent)
	}
	symbol := c.GetSymbolAtLocation(n)
	if symbol != nil && symbol.Flags&(ast.SymbolFlagsValue|ast.SymbolFlagsAlias) == 0 {
		return nil
	}
	return symbol
}
func (s *SourceRecoveryScope) LexicalBinding(n *ast.Node) (ScopedBinding, error) { return s.Binding(n) }
func (s *SourceRecoveryScope) ActualProgram() *Program                           { return s.snapshot.Program }
func (s *SourceRecoveryScope) ActualTemplate(n *ast.Node) (graph.SourceTemplateID, error) {
	if e := s.ValidateSourceCoverage(); e != nil {
		return 0, e
	}
	if !s.nodes[n] || s.templates[n] == 0 {
		return 0, fmt.Errorf("template from this actual scope required")
	}
	return s.templates[n], nil
}
func (s *SourceRecoveryScope) ActualSite(n *ast.Node) (graph.SourceSite, error) {
	if e := s.ValidateSourceCoverage(); e != nil {
		return graph.SourceSite{}, e
	}
	if !s.nodes[n] {
		return graph.SourceSite{}, fmt.Errorf("source node from this actual scope required")
	}
	return s.site(n), nil
}
