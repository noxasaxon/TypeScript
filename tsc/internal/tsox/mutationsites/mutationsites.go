// Package mutationsites extracts checked, pointer-free source coordinates for
// the offline mutation prospector.
package mutationsites

import (
	"context"
	"strings"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/bundled"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/internal/compiler"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/scanner"
	"github.com/microsoft/typescript-go/internal/tsoptions"
	"github.com/microsoft/typescript-go/internal/tspath"
	"github.com/microsoft/typescript-go/internal/vfs/vfstest"
	"github.com/microsoft/typescript-go/tsox/graph"
	"github.com/microsoft/typescript-go/tsox/mutation"
)

const virtualMainPath = "/src/main.ts"

// Extract checks source and returns all call-interaction mutation sites.
func Extract(sourcePath string, source string) mutation.Result {
	if sourcePath == "" {
		sourcePath = "main.ts"
	}
	fs := bundled.WrapFS(vfstest.FromMap(map[string]string{virtualMainPath: source}, true))
	host := compiler.NewCompilerHost("/src", fs, bundled.LibPath(), nil, nil)
	config := tsoptions.NewParsedCommandLine(&core.CompilerOptions{
		Strict: core.TSTrue, ModuleDetection: core.ModuleDetectionKindForce,
	}, []string{virtualMainPath}, tspath.ComparePathsOptions{
		UseCaseSensitiveFileNames: true, CurrentDirectory: "/src",
	})
	program := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host, SingleThreaded: core.TSTrue})
	program.BindSourceFiles()
	file := program.GetSourceFile(virtualMainPath)
	if file == nil {
		return mutation.Result{Diagnostics: []graph.Diagnostic{{SourcePath: sourcePath, Position: graph.Position{Line: 1, Column: 1}, Construct: "SourceFile", Message: "TypeScript program did not load the input source file"}}}
	}
	ctx := context.Background()
	if diagnostics := program.GetSyntacticDiagnostics(ctx, file); len(diagnostics) != 0 {
		return diagnostic(sourcePath, file, diagnostics[0])
	}
	if diagnostics := program.GetSemanticDiagnostics(ctx, file); len(diagnostics) != 0 {
		return diagnostic(sourcePath, file, diagnostics[0])
	}
	typeChecker, done := program.GetTypeChecker(ctx)
	defer done()

	b := builder{source: source, file: file, checker: typeChecker, ids: make(map[*ast.Symbol]uint32), bindings: make(map[*ast.Symbol]*mutation.Binding)}
	b.visit(file.AsNode())
	result := mutation.Result{Calls: b.calls, Identifiers: b.identifiers}
	for _, symbol := range b.bindingOrder {
		result.Bindings = append(result.Bindings, *b.bindings[symbol])
	}
	return result
}

func diagnostic(sourcePath string, file *ast.SourceFile, diagnostic *ast.Diagnostic) mutation.Result {
	position := graph.Position{Line: 1, Column: 1}
	if diagnostic.File() != nil && diagnostic.Pos() >= 0 {
		line, column := scanner.GetECMALineAndUTF16CharacterOfPosition(file, diagnostic.Pos())
		position = graph.Position{Line: line + 1, Column: int(column) + 1}
	}
	return mutation.Result{Diagnostics: []graph.Diagnostic{{SourcePath: sourcePath, Position: position, Construct: "TypeScriptDiagnostic", Message: "TypeScript diagnostic: " + diagnostic.String()}}}
}

type builder struct {
	source       string
	file         *ast.SourceFile
	checker      *checker.Checker
	ids          map[*ast.Symbol]uint32
	nextID       uint32
	bindings     map[*ast.Symbol]*mutation.Binding
	bindingOrder []*ast.Symbol
	calls        []mutation.CallSite
	identifiers  []string
}

func (b *builder) visit(node *ast.Node) bool {
	if ast.IsIdentifier(node) {
		b.identifiers = append(b.identifiers, scanner.GetTextOfNode(node))
		b.recordIdentifier(node)
	}
	if ast.IsVariableDeclaration(node) {
		b.recordDeclaration(node)
	}
	if node.Kind == ast.KindCallExpression {
		b.recordCall(node)
	}
	node.ForEachChild(b.visit)
	return false
}

func (b *builder) recordIdentifier(node *ast.Node) {
	symbol := b.checker.GetSymbolAtLocation(node)
	if symbol == nil || symbol.ValueDeclaration == nil || ast.GetSourceFileOfNode(symbol.ValueDeclaration) != b.file {
		return
	}
	binding := b.ensureBinding(symbol, node)
	if isDeclarationName(node) {
		return
	}
	binding.Uses = append(binding.Uses, b.span(node))
}

func (b *builder) recordDeclaration(node *ast.Node) {
	declaration := node.AsVariableDeclaration()
	name := declaration.Name()
	if name == nil || !ast.IsIdentifier(name) {
		return
	}
	symbol := b.checker.GetSymbolAtLocation(name)
	if symbol == nil {
		return
	}
	binding := b.ensureBinding(symbol, name)
	statement := containingStatement(node)
	if statement != nil {
		binding.Declaration = b.span(statement)
	}
	if declaration.Initializer != nil {
		binding.Initializer = b.span(declaration.Initializer)
	}
	binding.Constant = ast.IsVarConst(node)
}

func (b *builder) recordCall(node *ast.Node) {
	call := node.AsCallExpression()
	statement := containingStatement(node)
	if statement == nil {
		return
	}
	site := mutation.CallSite{Span: b.span(node), Statement: b.span(statement)}
	for index, argumentNode := range call.Arguments.Nodes {
		argument := mutation.Argument{Span: b.span(argumentNode), Type: b.typeIdentity(b.checker.GetTypeAtLocation(argumentNode))}
		if ast.IsIdentifier(argumentNode) {
			if symbol := b.checker.GetSymbolAtLocation(argumentNode); symbol != nil {
				argument.Binding = b.id(symbol)
			}
		}
		site.Arguments = append(site.Arguments, argument)
		parameterType := b.checker.GetContextualTypeForArgumentAtIndex(node, index)
		site.ParameterTypes = append(site.ParameterTypes, b.typeIdentity(parameterType))
	}
	b.calls = append(b.calls, site)
}

func (b *builder) ensureBinding(symbol *ast.Symbol, node *ast.Node) *mutation.Binding {
	if binding := b.bindings[symbol]; binding != nil {
		return binding
	}
	binding := &mutation.Binding{ID: b.id(symbol), Name: scanner.GetTextOfNode(node), Type: b.typeIdentity(b.checker.GetTypeOfSymbol(symbol))}
	b.bindings[symbol] = binding
	b.bindingOrder = append(b.bindingOrder, symbol)
	return binding
}

func (b *builder) id(symbol *ast.Symbol) uint32 {
	if id := b.ids[symbol]; id != 0 {
		return id
	}
	b.nextID++
	b.ids[symbol] = b.nextID
	return b.nextID
}

func (b *builder) typeIdentity(value *checker.Type) mutation.TypeIdentity {
	if value == nil {
		return mutation.TypeIdentity{}
	}
	flags := value.Flags()
	kind := b.checker.TypeToString(value)
	switch {
	case flags&checker.TypeFlagsNumberLike != 0:
		kind = "number"
	case flags&checker.TypeFlagsStringLike != 0:
		kind = "string"
	case flags&checker.TypeFlagsBooleanLike != 0:
		kind = "boolean"
	case flags&checker.TypeFlagsVoidLike != 0:
		kind = "void"
	case flags&checker.TypeFlagsObject != 0:
		kind = "object"
	}
	identity := mutation.TypeIdentity{Kind: kind}
	if kind == "object" {
		name := b.checker.TypeToString(value)
		if name != "object" && !strings.HasPrefix(name, "{") && !strings.HasPrefix(name, "(") {
			identity.Named = name
		}
	}
	return identity
}

func (b *builder) span(node *ast.Node) mutation.Span {
	return mutation.Span{Start: scanner.GetTokenPosOfNode(node, b.file, false), End: node.End()}
}

func containingStatement(node *ast.Node) *ast.Node {
	for current := node; current != nil; current = current.Parent {
		if ast.IsStatement(current) && current.Kind != ast.KindBlock {
			return current
		}
	}
	return nil
}

func isDeclarationName(node *ast.Node) bool {
	parent := node.Parent
	if parent == nil {
		return false
	}
	switch parent.Kind {
	case ast.KindVariableDeclaration, ast.KindParameter, ast.KindFunctionDeclaration:
		return parent.Name() == node
	}
	return false
}
