package mutationsites

import (
	"sort"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/internal/scanner"
	"github.com/microsoft/typescript-go/tsox/mutation"
)

func (b *builder) recordReceiverEffects(node *ast.Node) {
	var receiver *ast.Node
	var operands []*ast.Node
	kind := "index"
	switch node.Kind {
	case ast.KindElementAccessExpression:
		if isWriteTarget(node) {
			return
		}
		access := node.AsElementAccessExpression()
		receiver, operands = access.Expression, []*ast.Node{access.ArgumentExpression}
	case ast.KindCallExpression:
		call := node.AsCallExpression()
		if call.Expression.Kind != ast.KindPropertyAccessExpression {
			return
		}
		receiver, operands = call.Expression.AsPropertyAccessExpression().Expression, call.Arguments.Nodes
		kind = "argument"
	default:
		return
	}
	if !b.checker.IsArrayType(b.checker.GetNonNullableType(b.checker.GetTypeAtLocation(receiver))) {
		return
	}
	statement := containingStatement(node)
	// A helper declaration needs a statement-list position, not a loop header
	// or a single-statement branch. It may capture the original lexical locals.
	if statement == nil || statement.Parent == nil || (statement.Parent.Kind != ast.KindBlock && statement.Parent.Kind != ast.KindSourceFile) {
		return
	}
	switch statement.Kind {
	case ast.KindExpressionStatement, ast.KindVariableStatement, ast.KindReturnStatement:
	default:
		return
	}
	_, targets, ok := b.receiverPath(receiver)
	if !ok || len(targets) == 0 {
		return
	}
	// Resolve donors here so a similarly named variable in a sibling scope can
	// never win. No new binding IDs are allocated by this additive site surface.
	symbols := b.checker.GetSymbolsInScope(node, ast.SymbolFlagsValue)
	sort.Slice(symbols, func(i, j int) bool {
		left, right := symbols[i].ValueDeclaration, symbols[j].ValueDeclaration
		if left == nil {
			return false
		}
		if right == nil {
			return true
		}
		if left.Pos() != right.Pos() {
			return left.Pos() > right.Pos()
		}
		return symbols[i].Name < symbols[j].Name
	})
	for index := range targets {
		for _, symbol := range symbols {
			if !b.runtimeBinding(symbol) || symbol.ValueDeclaration.Kind != ast.KindVariableDeclaration {
				continue
			}
			declaration := symbol.ValueDeclaration
			if declaration.Initializer() == nil || declaration.End() > b.span(statement).Start || symbol.Name == targets[index].Place {
				continue
			}
			if b.typeIdentity(b.checker.GetTypeOfSymbol(symbol)) == targets[index].Type {
				targets[index].Donors = append(targets[index].Donors, symbol.Name)
			}
		}
	}
	for _, operand := range operands {
		identity := b.typeIdentity(b.checker.GetTypeAtLocation(operand))
		if identity.Kind != "number" && identity.Kind != "string" && identity.Kind != "boolean" {
			continue
		}
		b.receiverEffects = append(b.receiverEffects, mutation.ReceiverEffectSite{Kind: kind, Statement: b.span(statement), Operand: b.span(operand), Type: identity, Targets: targets, TopLevel: statement.Parent.Kind == ast.KindSourceFile})
	}
}

// receiverPath accepts local roots, ordinary data properties and literal array
// indices. Calls, getters, computed keys and imported roots are deliberately
// excluded: re-evaluating those in the injected write would add another effect.
func (b *builder) receiverPath(node *ast.Node) (string, []mutation.ReceiverTarget, bool) {
	if node.Kind == ast.KindParenthesizedExpression || node.Kind == ast.KindNonNullExpression {
		return b.receiverPath(node.Expression())
	}
	var place string
	var targets []mutation.ReceiverTarget
	var valueType *checker.Type
	writable := false
	switch node.Kind {
	case ast.KindIdentifier:
		symbol := b.checker.GetSymbolAtLocation(node)
		if !b.runtimeBinding(symbol) || symbol.ValueDeclaration.Kind == ast.KindFunctionDeclaration {
			return "", nil, false
		}
		place = scanner.GetTextOfNode(node)
		valueType = b.checker.GetTypeOfSymbol(symbol)
		writable = symbol.ValueDeclaration.Kind == ast.KindParameter || !ast.IsVarConst(symbol.ValueDeclaration)
	case ast.KindPropertyAccessExpression:
		access := node.AsPropertyAccessExpression()
		parent, owners, ok := b.receiverPath(access.Expression)
		if !ok {
			return "", nil, false
		}
		symbol := b.checker.GetSymbolAtLocation(access.Name())
		if symbol == nil || symbol.Flags&ast.SymbolFlagsProperty == 0 || len(symbol.Declarations) == 0 {
			return "", nil, false
		}
		writable = true
		for _, declaration := range symbol.Declarations {
			if declaration.Kind != ast.KindPropertySignature && declaration.Kind != ast.KindPropertyAssignment && declaration.Kind != ast.KindShorthandPropertyAssignment {
				return "", nil, false
			}
			if ast.GetCombinedModifierFlags(declaration)&ast.ModifierFlagsReadonly != 0 {
				writable = false
			}
		}
		place, targets = parent+"."+scanner.GetTextOfNode(access.Name()), owners
		valueType = b.checker.GetTypeOfSymbol(symbol)
	case ast.KindElementAccessExpression:
		access := node.AsElementAccessExpression()
		if access.ArgumentExpression.Kind != ast.KindNumericLiteral {
			return "", nil, false
		}
		parent, owners, ok := b.receiverPath(access.Expression)
		if !ok {
			return "", nil, false
		}
		arrayType := b.checker.GetNonNullableType(b.checker.GetTypeAtLocation(access.Expression))
		if !b.checker.IsArrayType(arrayType) {
			return "", nil, false
		}
		span := b.span(access.ArgumentExpression)
		place, targets = parent+"["+b.source[span.Start:span.End]+"]", owners
		valueType, writable = b.checker.GetElementTypeOfArrayType(arrayType), true
	default:
		return "", nil, false
	}
	identity := b.typeIdentity(b.checker.GetNonNullableType(valueType))
	if writable && (identity.Kind == "array" || identity.Kind == "object") {
		targets = append(targets, mutation.ReceiverTarget{Place: place, Type: identity, Clearable: includesUndefined(valueType), RootBinding: node.Kind == ast.KindIdentifier})
	}
	if includesNullish(valueType) {
		place += "!"
	}
	return place, targets, true
}

func includesUndefined(value *checker.Type) bool {
	return includesFlags(value, checker.TypeFlagsUndefined)
}
func includesNullish(value *checker.Type) bool {
	return includesFlags(value, checker.TypeFlagsNull|checker.TypeFlagsUndefined)
}
func includesFlags(value *checker.Type, flags checker.TypeFlags) bool {
	if value == nil {
		return false
	}
	if value.Flags()&flags != 0 {
		return true
	}
	if value.Flags()&checker.TypeFlagsUnion != 0 {
		for _, member := range value.Types() {
			if includesFlags(member, flags) {
				return true
			}
		}
	}
	return false
}
