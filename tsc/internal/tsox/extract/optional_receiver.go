package extract

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// Postfix assertions do not close a chain; grouping does. The parser adds
// OptionalChain to assertion nodes only when certain following links request
// it, so inspect the underlying link without crossing any parentheses.
func assertsOptionalChainLink(operand *ast.Node) bool {
	for operand.Kind == ast.KindNonNullExpression {
		operand = operand.AsNonNullExpression().Expression
	}
	return operand.Flags&ast.NodeFlagsOptionalChain != 0
}

// Syntactic ?. tests the actual optional slot even when checker narrowing did
// not account for an intervening helper's mutation. Explicit ! still requests
// its existing trapping read and must not acquire optional-chain semantics.
func restoreOptionalChainReceiver(receiver *graph.Expression, explicit bool) {
	if explicit && receiver.UnwrapOptional && !receiver.NonNullAssertion {
		receiver.Type.Optional = true
		receiver.UnwrapOptional = false
	}
}

// A nonoptional slot can still rely on the checker's narrowing of an optional
// chain, under the existing D0002 checked unwrap. Keep the chain itself optional
// for consumers such as console and JSON that can observe undefined directly.
func (b *builder) narrowOptionalChainForSlot(node *ast.Node, slot graph.Type, value *graph.Expression) {
	if value == nil || !value.OptionalChain || !value.Type.Optional || slot.Optional || !sameInnerType(slot, value.Type) {
		return
	}
	checked, fence := b.checkedType(node)
	if fence == nil && !checked.Optional && sameInnerType(checked, value.Type) {
		value.Type.Optional = false
		value.UnwrapOptional = true
	}
}

func (b *builder) narrowOptionalChainUse(node *ast.Node, value *graph.Expression) {
	slot := value.Type
	slot.Optional = false
	b.narrowOptionalChainForSlot(node, slot, value)
}
