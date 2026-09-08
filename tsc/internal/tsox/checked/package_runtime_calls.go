package checked

import (
	"context"
	"fmt"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/compiler"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// projectDiagnosticError preserves the source boundary through capture and replay.
// The public API returns Diagnostic directly; Error remains useful to internal callers.
type projectDiagnosticError struct {
	Diagnostic graph.Diagnostic
	Cause      error
}

func (e *projectDiagnosticError) Error() string {
	return e.Diagnostic.Construct + ": " + e.Diagnostic.String()
}
func (e *projectDiagnosticError) Unwrap() error { return e.Cause }

// Static imports close before this pass. Only the final Program, including actual
// runtime JavaScript roots, can distinguish a local binding from Node's loader.
// A source binding is not a proof of callable behavior: ordinary extraction and
// effect checks still apply to its body, arguments and resolved captures.
func validateSourceRuntimeCalls(program *compiler.Program, modules []packageModule) error {
	c, done := program.GetTypeChecker(context.Background())
	defer done()
	formats := make(map[*ast.SourceFile]string, len(modules))
	for _, module := range modules {
		if file := program.GetSourceFile(module.ID); file != nil {
			formats[file] = module.Format
		}
	}
	for _, module := range modules {
		file := program.GetSourceFile(module.ID)
		if file == nil {
			return fmt.Errorf("runtime source absent from final Program: %s", module.ID)
		}
		var rejected *ast.Node
		var visit func(*ast.Node) bool
		visit = func(n *ast.Node) bool {
			if n == nil {
				return false
			}
			if n.Kind == ast.KindImportEqualsDeclaration {
				rejected = n
				return true
			}
			if n.Kind == ast.KindCallExpression {
				callee := n.AsCallExpression().Expression
				for callee.Kind == ast.KindParenthesizedExpression {
					callee = callee.AsParenthesizedExpression().Expression
				}
				if callee.Kind == ast.KindImportKeyword {
					rejected = n
					return true
				}
				if callee.Kind == ast.KindIdentifier && callee.Text() == "require" {
					symbol := c.GetSymbolAtLocation(callee)
					if symbol != nil && symbol.Flags&ast.SymbolFlagsAlias != 0 {
						symbol = c.GetAliasedSymbol(symbol)
					}
					if !sourceRequireBinding(symbol, formats) {
						rejected = n
						return true
					}
				}
			}
			return n.ForEachChild(visit)
		}
		file.AsNode().ForEachChild(visit)
		if rejected != nil {
			return &projectDiagnosticError{Diagnostic: *diagnostic(file, file.FileName(), rejected, "SourceRuntimeDependency", "require/dynamic-import identity and timing are not represented")}
		}
	}
	return nil
}

// Declaration-file values and synthetic wrapper symbols are not executable local
// bindings. Classification supplies no callable, argument or effect proof.
func sourceRequireBinding(symbol *ast.Symbol, formats map[*ast.SourceFile]string) bool {
	if !hasRuntimeValue(symbol) {
		return false
	}
	// CJS var instantiation reuses the implicit wrapper parameter. Even an
	// initializer does not prove its value at every call (it may execute later
	// or on only one branch). Keep that loader seam positioned until value/order
	// proof exists. A direct hoisted source function replaces the parameter
	// before execution, and an uninitialized var does not replace that function.
	if symbol.Name == "require" {
		wrapperVar, initialized, hoisted := false, false, false
		for _, declaration := range symbol.Declarations {
			file := ast.GetSourceFileOfNode(declaration)
			if file == nil || formats[file] != "commonjs" {
				continue
			}
			if variable := cjsWrapperVarDeclaration(declaration); variable != nil {
				wrapperVar = true
				initialized = initialized || variable.AsVariableDeclaration().Initializer != nil
			}
			if declaration.Kind == ast.KindFunctionDeclaration && declaration.Parent == file.AsNode() && declaration.AsFunctionDeclaration().Body != nil {
				hoisted = true
			}
		}
		if wrapperVar && (!hoisted || initialized) {
			return false
		}
	}
	for _, declaration := range symbol.Declarations {
		file := ast.GetSourceFileOfNode(declaration)
		if file == nil || file.IsDeclarationFile {
			continue
		}
		ambient := false
		for node := declaration; node != nil; node = node.Parent {
			if ast.HasAmbientModifier(node) {
				ambient = true
				break
			}
		}
		if ambient {
			continue
		}
		switch declaration.Kind {
		case ast.KindFunctionDeclaration:
			if declaration.AsFunctionDeclaration().Body != nil {
				return true
			}
		case ast.KindVariableDeclaration, ast.KindParameter, ast.KindBindingElement, ast.KindFunctionExpression:
			return true
		}
	}
	return false
}

// A var nested in a block/loop still belongs to the CJS wrapper function. A
// nested function or class static block instead creates a distinct var scope.
// Catch parameters and lexical declarations do not redeclare the wrapper.
func cjsWrapperVarDeclaration(declaration *ast.Node) *ast.Node {
	variable := declaration
	for variable != nil && (variable.Kind == ast.KindBindingElement || variable.Kind == ast.KindObjectBindingPattern || variable.Kind == ast.KindArrayBindingPattern) {
		variable = variable.Parent
	}
	if variable == nil || variable.Kind != ast.KindVariableDeclaration {
		return nil
	}
	list := variable.Parent
	if list == nil || list.Kind != ast.KindVariableDeclarationList || list.Flags&ast.NodeFlagsBlockScoped != 0 {
		return nil
	}
	for scope := list.Parent; scope != nil; scope = scope.Parent {
		if ast.IsFunctionLike(scope) || scope.Kind == ast.KindClassStaticBlockDeclaration {
			return nil
		}
		if scope.Kind == ast.KindSourceFile {
			return variable
		}
	}
	return nil
}
