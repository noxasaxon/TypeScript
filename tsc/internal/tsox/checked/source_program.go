package checked

import (
	"context"
	"fmt"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// StartupSourceKind classifies an original operation, not its native discharge.
// Function instantiation still needs invocation/identity/effect and carrier proof.
type StartupSourceKind uint8

const (
	StartupSourceUnresolved StartupSourceKind = iota
	StartupSourceImport
	StartupSourceFunction
	StartupSourceTypeErasure
	StartupSourceEmpty
	StartupSourceExternal
)

// Ordinal retains lexical source order. Function operations belong to module
// instantiation; they are not executed at the ordinal among body evaluations.
type StartupSourceOperation struct {
	RuntimeIdentity string
	Position        graph.Position
	Module          graph.SourceModuleID
	Ordinal         int
	Kind            StartupSourceKind
	Site            graph.SourceSite
	Template        graph.SourceTemplateID
	Binding         graph.SourceLexicalID
}

// OriginalStartup reads actual immutable source owners, never Registry.Kind or
// startup status strings. Results are detached and confer no independent proof.
func (s *SourceRecoveryScope) OriginalStartup() ([]StartupSourceOperation, error) {
	if err := s.ValidateSourceCoverage(); err != nil {
		return nil, err
	}
	var out []StartupSourceOperation
	for _, record := range s.view.Modules {
		if record.Format == "builtin" {
			out = append(out, StartupSourceOperation{Module: record.ID, RuntimeIdentity: record.Identity, Kind: StartupSourceExternal})
			continue
		}
		plan := s.snapshot.Plans.Modules[record.Identity]
		if plan == nil || plan.File == nil || s.snapshot.Program.Compiler.GetSourceFile(record.Identity) != plan.File {
			return nil, fmt.Errorf("original startup module source required")
		}
		for i, node := range plan.File.Statements.Nodes {
			op := StartupSourceOperation{Module: record.ID, RuntimeIdentity: record.Identity, Ordinal: i, Site: s.site(node)}
			op.Position = diagnostic(plan.File, record.Identity, node, "ModuleStartup", "").Position
			op.Position.SourcePath = record.Identity
			// CJS wrapper instantiation and export state remain a different contract.
			if plan.Format == "module-typescript" || plan.Format == "module" {
				switch node.Kind {
				case ast.KindImportDeclaration:
					op.Kind = StartupSourceImport
					clause := node.AsImportDeclaration().ImportClause
					if clause != nil && clause.AsImportClause().PhaseModifier == ast.KindTypeKeyword {
						op.Kind = StartupSourceTypeErasure
					}
				case ast.KindFunctionDeclaration:
					// Bodyless declarations do not create callable runtime values.
					if node.Body() == nil {
						op.Kind = StartupSourceTypeErasure
					} else {
						op.Kind = StartupSourceFunction
						op.Template = s.templates[node]
						if op.Template == 0 || node.Name() == nil {
							return nil, fmt.Errorf("original named function template required")
						}
						h, err := s.SourceHandle(node)
						if err != nil {
							return nil, err
						}
						op.Binding, err = s.SourceLexical(h)
						if err != nil {
							return nil, err
						}
					}
				case ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
					op.Kind = StartupSourceTypeErasure
				case ast.KindEmptyStatement:
					op.Kind = StartupSourceEmpty
				}
			}
			out = append(out, op)
		}
	}
	return out, nil
}

// SourceCallableTarget is a source declaration/linkage observation. The caller
// still needs evaluated argument, effects, function-object and invocation proof.
type SourceCallableTarget struct {
	Calls    []graph.SourceSite
	Template graph.SourceTemplateID
	Binding  graph.SourceLexicalID
	Site     graph.SourceSite
}

func (s *SourceRecoveryScope) OriginalCallableTarget(source ScopedSourceHandle) (SourceCallableTarget, error) {
	if _, err := s.SourceSite(source); err != nil {
		return SourceCallableTarget{}, err
	}
	call := source.node
	if call.Kind != ast.KindCallExpression || call.AsCallExpression().Expression.Kind != ast.KindIdentifier {
		return SourceCallableTarget{}, fmt.Errorf("original direct lexical call required")
	}
	c, done := s.snapshot.Program.Compiler.GetTypeChecker(context.Background())
	defer done()
	resolve := func(n *ast.Node) *ast.Symbol {
		symbol := lexicalSymbol(n, c)
		if symbol != nil && symbol.Flags&ast.SymbolFlagsAlias != 0 {
			symbol = c.GetAliasedSymbol(symbol)
		}
		return symbol
	}
	return s.originalCallableTarget(c, resolve(call.AsCallExpression().Expression))
}

func (s *SourceRecoveryScope) originalCallableTarget(c *checker.Checker, symbol *ast.Symbol) (SourceCallableTarget, error) {
	resolve := func(n *ast.Node) *ast.Symbol {
		symbol := lexicalSymbol(n, c)
		if symbol != nil && symbol.Flags&ast.SymbolFlagsAlias != 0 {
			symbol = c.GetAliasedSymbol(symbol)
		}
		return symbol
	}
	if symbol == nil || len(symbol.Declarations) != 1 {
		return SourceCallableTarget{}, fmt.Errorf("one actual function declaration required")
	}
	decl := symbol.Declarations[0]
	file := ast.GetSourceFileOfNode(decl)
	if file == nil || file.IsDeclarationFile || decl.Kind != ast.KindFunctionDeclaration || decl.Parent != file.AsNode() || decl.Body() == nil || decl.Name() == nil {
		return SourceCallableTarget{}, fmt.Errorf("selected runtime function body required")
	}
	plan := s.snapshot.Plans.Modules[file.FileName()]
	if plan == nil || plan.File != file || (plan.Format != "module" && plan.Format != "module-typescript") {
		return SourceCallableTarget{}, fmt.Errorf("actual ESM runtime function required")
	}
	// This first source query accepts only declaration/linkage and direct calls.
	// An alias assignment, property observation, return, capture-as-value or
	// write leaves the function-object/target obligation unresolved. A readonly
	// .d.ts signature or a metadata target is never enough.
	unresolved := false
	var calls []graph.SourceSite
	var visit func(*ast.Node) bool
	visit = func(n *ast.Node) bool {
		if n.Kind == ast.KindIdentifier && resolve(n) == symbol {
			p := n.Parent
			allowed := n == decl.Name() || p.Kind == ast.KindCallExpression && p.AsCallExpression().Expression == n
			if p.Kind == ast.KindCallExpression && p.AsCallExpression().Expression == n {
				calls = append(calls, s.site(p))
			}
			if p.Kind == ast.KindImportSpecifier && p.Name() == n {
				allowed = true
			}
			if p.Kind == ast.KindImportClause && p.Name() == n {
				allowed = true
			}
			if !allowed {
				unresolved = true
			}
		}
		n.ForEachChild(visit)
		return false
	}
	for _, file := range s.snapshot.Program.RuntimeFiles {
		visit(file.AsNode())
	}
	if unresolved {
		return SourceCallableTarget{}, fmt.Errorf("function binding has value/alias/write observations requiring proof")
	}
	template := s.templates[decl]
	if template == 0 {
		return SourceCallableTarget{}, fmt.Errorf("registered original template required")
	}
	h, err := s.SourceHandle(decl)
	if err != nil {
		return SourceCallableTarget{}, err
	}
	binding, err := s.SourceLexical(h)
	if err != nil {
		return SourceCallableTarget{}, err
	}
	return SourceCallableTarget{Calls: calls, Template: template, Binding: binding, Site: s.site(decl)}, nil
}

// OriginalEntry resolves the actual selected export in the captured entry
// module. It supplies no Request or invocation value premise.
func (s *SourceRecoveryScope) OriginalEntry(name string) (SourceCallableTarget, error) {
	if err := s.ValidateSourceCoverage(); err != nil {
		return SourceCallableTarget{}, err
	}
	c, done := s.snapshot.Program.Compiler.GetTypeChecker(context.Background())
	defer done()
	module := s.snapshot.Program.Entry.AsNode().Symbol()
	if module == nil {
		return SourceCallableTarget{}, fmt.Errorf("actual entry module required")
	}
	for _, exported := range c.GetExportsOfModule(module) {
		if ast.SymbolName(exported) != name {
			continue
		}
		symbol := exported
		if symbol.Flags&ast.SymbolFlagsAlias != 0 {
			symbol = c.GetAliasedSymbol(symbol)
		}
		return s.originalCallableTarget(c, symbol)
	}
	return SourceCallableTarget{}, fmt.Errorf("actual selected entry export absent")
}
