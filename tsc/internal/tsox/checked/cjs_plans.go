package checked

import (
	"crypto/sha256"
	"fmt"
	"slices"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/internal/scanner"
)

// SourceNodeID identifies original syntax in the immutable, captured Program.
// It is not a checker type, function-effect proof, or serialized AST substitute.
type SourceNodeID struct {
	Module, SourceSHA256, Kind string
	Start, End                 int
}

func sourceNodeID(file *ast.SourceFile, node *ast.Node) SourceNodeID {
	return SourceNodeID{file.FileName(), fmt.Sprintf("%x", sha256.Sum256([]byte(file.Text()))), fmt.Sprint(node.Kind), node.Pos(), node.End()}
}

type SourceOperation struct {
	Phase  string
	Source SourceNodeID
	Node   *ast.Node `json:"-"`
}
type FunctionSource struct {
	Declaration, Body SourceNodeID
	Name              string
	Node              *ast.Node   `json:"-"`
	Symbol            *ast.Symbol `json:"-"`
}

// A wrapper reference identifies the parameter binding, not its present value.
// module/exports may be rebound; module.exports is never an unconditional alias
// for the loader's persistent ModuleRecord.exports cell.
type CJSWriteSite struct {
	Source             SourceNodeID
	Parameter          string
	Properties         []string
	DynamicProperty    bool
	Value              SourceNodeID
	FunctionBodies     []SourceNodeID
	EnclosingFunctions []SourceNodeID
	Node               *ast.Node `json:"-"`
}
type ModuleExportPlan struct {
	Strict                       bool
	Module, Format, SourceSHA256 string
	File                         *ast.SourceFile `json:"-"`
	Initialization               []SourceOperation
	HoistedFunctions             []FunctionSource
	Functions                    []FunctionSource
	WrapperWrites                []CJSWriteSite
	// True until the original operations are lowered and analyzed. Syntax sites
	// and hoisted bodies never justify skipping initializer calls/effects.
	NeedsInitializationProof bool
}

type RuntimeImportBinding struct {
	Importer, Module, Format, Requested, Form, Capture string
	Local                                              SourceNodeID
	CheckerLocal                                       *ast.Symbol `json:"-"`
	TypeDeclarations                                   []SourceNodeID
	// Candidates are source bodies mentioned by matching wrapper writes. They
	// are not a final export/callee certificate; detachment, replacement, calls,
	// descriptors and later/deferred writes still need the initialization plan.
	CandidateBodies       []SourceNodeID
	NeedsExportStateProof bool
}
type RuntimeSourcePlans struct {
	Modules map[string]*ModuleExportPlan
	Imports []RuntimeImportBinding
}

func sourceFunction(file *ast.SourceFile, n *ast.Node, c *checker.Checker) (FunctionSource, bool) {
	if !ast.IsFunctionLike(n) {
		return FunctionSource{}, false
	}
	body := n.Body()
	if body == nil {
		return FunctionSource{}, false
	}
	out := FunctionSource{Declaration: sourceNodeID(file, n), Body: sourceNodeID(file, body), Node: n}
	if name := n.Name(); name != nil && name.Kind == ast.KindIdentifier {
		out.Name = name.Text()
		out.Symbol = c.GetSymbolAtLocation(name)
	}
	return out, true
}
func cjsWrapperParameter(file *ast.SourceFile, n *ast.Node, c *checker.Checker) string {
	if n == nil || n.Kind != ast.KindIdentifier || n.Text() != "exports" && n.Text() != "module" {
		return ""
	}
	symbol := c.GetSymbolAtLocation(n)
	// The Node-classified CJS wrapper provides this lexical binding even when
	// TypeScript does not synthesize a symbol (for example alias-only exports).
	// This helper is used only under an actual commonjs runtime module plan.
	// Resolved source parameters/locals continue to take precedence below.
	if symbol == nil {
		return n.Text()
	}
	if symbol.Name != n.Text() || len(symbol.Declarations) == 0 {
		return ""
	}
	// The upstream JS binder synthesizes these declarations at this exact
	// SourceFile. A lexical parameter/variable with the spelling is not enough.
	for _, declaration := range symbol.Declarations {
		if declaration != file.AsNode() {
			return ""
		}
	}
	return n.Text()
}
func cjsWriteTarget(file *ast.SourceFile, n *ast.Node, c *checker.Checker) (string, []string, bool) {
	if n == nil {
		return "", nil, false
	}
	if parameter := cjsWrapperParameter(file, n, c); parameter != "" {
		return parameter, nil, false
	}
	switch n.Kind {
	case ast.KindPropertyAccessExpression:
		p := n.AsPropertyAccessExpression()
		parameter, path, dynamic := cjsWriteTarget(file, p.Expression, c)
		if parameter != "" {
			return parameter, append(path, p.Name().Text()), dynamic
		}
	case ast.KindElementAccessExpression:
		p := n.AsElementAccessExpression()
		parameter, path, dynamic := cjsWriteTarget(file, p.Expression, c)
		if parameter != "" {
			if p.ArgumentExpression.Kind == ast.KindStringLiteral {
				return parameter, append(path, p.ArgumentExpression.Text()), dynamic
			}
			return parameter, append(path, ""), true
		}
	}
	return "", nil, false
}

func planRuntimeModule(file *ast.SourceFile, format string, c *checker.Checker) (*ModuleExportPlan, error) {
	if file == nil || file.IsDeclarationFile {
		return nil, fmt.Errorf("runtime module plan requires actual implementation source")
	}
	out := &ModuleExportPlan{Module: file.FileName(), Format: format, SourceSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(file.Text()))), File: file, NeedsInitializationProof: true}
	out.Strict = format == "module"
	for _, n := range file.Statements.Nodes {
		if !ast.IsPrologueDirective(n) {
			break
		}
		literal := n.AsExpressionStatement().Expression
		text := file.Text()[scanner.GetTokenPosOfNode(literal, file, false):literal.End()]
		if text == `"use strict"` || text == `'use strict'` {
			out.Strict = true
		}
	}
	for _, n := range file.Statements.Nodes {
		phase := "evaluate"
		switch n.Kind {
		case ast.KindFunctionDeclaration:
			phase = "instantiate-function"
		case ast.KindImportDeclaration, ast.KindExportDeclaration:
			phase = "module-link"
		case ast.KindInterfaceDeclaration, ast.KindTypeAliasDeclaration:
			phase = "erased-type"
		}
		out.Initialization = append(out.Initialization, SourceOperation{Phase: phase, Source: sourceNodeID(file, n), Node: n})
		if n.Kind == ast.KindFunctionDeclaration {
			if f, ok := sourceFunction(file, n, c); ok {
				out.HoistedFunctions = append(out.HoistedFunctions, f)
			}
		}
	}
	var bodies func(*ast.Node) bool
	bodies = func(n *ast.Node) bool {
		if f, ok := sourceFunction(file, n, c); ok {
			out.Functions = append(out.Functions, f)
		}
		return n.ForEachChild(bodies)
	}
	file.AsNode().ForEachChild(bodies)
	if format != "commonjs" {
		return out, nil
	}
	var visit func(*ast.Node, []SourceNodeID)
	visit = func(n *ast.Node, inside []SourceNodeID) {
		if f, ok := sourceFunction(file, n, c); ok {
			inside = append(slices.Clone(inside), f.Declaration)
		}
		if n.Kind == ast.KindBinaryExpression && n.AsBinaryExpression().OperatorToken.Kind == ast.KindEqualsToken {
			expression := n.AsBinaryExpression()
			parameter, path, dynamic := cjsWriteTarget(file, expression.Left, c)
			if parameter != "" {
				site := CJSWriteSite{Source: sourceNodeID(file, n), Parameter: parameter, Properties: path, DynamicProperty: dynamic, Value: sourceNodeID(file, expression.Right), EnclosingFunctions: slices.Clone(inside), Node: n}
				// Link the actual implementation declaration(s), not the import's d.ts
				// target. Multiple declarations remain visible, including duplicate hoists.
				if expression.Right.Kind == ast.KindIdentifier {
					symbol := c.GetSymbolAtLocation(expression.Right)
					for _, function := range out.Functions {
						if symbol != nil && function.Symbol == symbol {
							site.FunctionBodies = append(site.FunctionBodies, function.Body)
						}
					}
				} else if f, ok := sourceFunction(file, expression.Right, c); ok {
					site.FunctionBodies = append(site.FunctionBodies, f.Body)
				}
				out.WrapperWrites = append(out.WrapperWrites, site)
			}
		}
		n.ForEachChild(func(child *ast.Node) bool { visit(child, inside); return false })
	}
	file.AsNode().ForEachChild(func(n *ast.Node) bool { visit(n, nil); return false })
	return out, nil
}

// The caller keeps the same Program/checker lease throughout planning and graph
// extraction. All node/symbol references point to original implementation or
// import syntax in that Program; exported signatures are recorded separately.
func planRuntimeSources(project *dependencyProject, c *checker.Checker) (*RuntimeSourcePlans, error) {
	out := &RuntimeSourcePlans{Modules: map[string]*ModuleExportPlan{}}
	for _, module := range project.Manifest.Modules {
		file := project.Program.GetSourceFile(module.ID)
		plan, err := planRuntimeModule(file, module.Format, c)
		if err != nil {
			return nil, err
		}
		out.Modules[module.ID] = plan
	}
	for _, module := range project.Manifest.Modules {
		file := project.Program.GetSourceFile(module.ID)
		resolutions := map[string]RuntimeResolution{}
		for _, edge := range module.Dependencies {
			resolutions[edge.Edge.Specifier] = edge
		}
		for _, statement := range file.Statements.Nodes {
			if statement.Kind != ast.KindImportDeclaration {
				continue
			}
			declaration := statement.AsImportDeclaration()
			if declaration.ImportClause == nil {
				continue
			}
			clause := declaration.ImportClause.AsImportClause()
			if clause.PhaseModifier == ast.KindTypeKeyword {
				continue
			}
			resolution, ok := resolutions[declaration.ModuleSpecifier.Text()]
			if !ok {
				return nil, fmt.Errorf("runtime import lost captured resolution")
			}
			add := func(name *ast.Node, requested, form string) error {
				symbol := c.GetSymbolAtLocation(name)
				if symbol == nil {
					return fmt.Errorf("import local has no checker identity")
				}
				binding := RuntimeImportBinding{Importer: file.FileName(), Module: resolution.Module, Format: resolution.Format, Requested: requested, Form: form, Local: sourceNodeID(file, name), CheckerLocal: symbol, NeedsExportStateProof: true}
				target := symbol
				if symbol.Flags&ast.SymbolFlagsAlias != 0 {
					target = c.GetAliasedSymbol(symbol)
				}
				if target != nil {
					for _, decl := range target.Declarations {
						source := ast.GetSourceFileOfNode(decl)
						if source != nil {
							binding.TypeDeclarations = append(binding.TypeDeclarations, sourceNodeID(source, decl))
						}
					}
				}
				binding.Capture = "esm-live-binding"
				if form == "namespace" {
					binding.Capture = "module-namespace"
				} else if resolution.Format == "commonjs" {
					binding.Capture = "cjs-named-snapshot"
					if requested == "default" || requested == "module.exports" {
						binding.Capture = "cjs-final-module-exports-value"
					}
					if plan := out.Modules[resolution.Module]; plan != nil {
						for _, site := range plan.WrapperWrites {
							matches := site.Parameter == "exports" && len(site.Properties) == 1 && site.Properties[0] == requested
							matches = matches || binding.Capture == "cjs-final-module-exports-value" && site.Parameter == "module" && slices.Equal(site.Properties, []string{"exports"})
							if matches && !site.DynamicProperty {
								binding.CandidateBodies = append(binding.CandidateBodies, site.FunctionBodies...)
							}
						}
					}
				}
				out.Imports = append(out.Imports, binding)
				return nil
			}
			if clause.Name() != nil {
				if err := add(clause.Name(), "default", "default"); err != nil {
					return nil, err
				}
			}
			if clause.NamedBindings == nil {
				continue
			}
			if clause.NamedBindings.Kind == ast.KindNamespaceImport {
				if err := add(clause.NamedBindings.Name(), "*", "namespace"); err != nil {
					return nil, err
				}
				continue
			}
			for _, node := range clause.NamedBindings.AsNamedImports().Elements.Nodes {
				specifier := node.AsImportSpecifier()
				if specifier.IsTypeOnly {
					continue
				}
				requested := specifier.Name().Text()
				if specifier.PropertyName != nil {
					requested = specifier.PropertyName.Text()
				}
				if err := add(specifier.Name(), requested, "named"); err != nil {
					return nil, err
				}
			}
		}
	}
	return out, nil
}
