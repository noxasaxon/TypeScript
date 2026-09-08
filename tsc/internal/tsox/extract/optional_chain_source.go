package extract

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/tsox/graph"
)

func optionalSourceLink(node *ast.Node) *graph.OptionalLink {
	var receiver *ast.Node
	check := false
	switch node.Kind {
	case ast.KindPropertyAccessExpression:
		data := node.AsPropertyAccessExpression()
		receiver = data.Expression
		check = data.QuestionDotToken != nil
	case ast.KindElementAccessExpression:
		data := node.AsElementAccessExpression()
		receiver = data.Expression
		check = data.QuestionDotToken != nil
	default:
		return nil
	}
	return &graph.OptionalLink{CheckReceiver: check, ContinuesReceiverChain: assertsOptionalChainLink(receiver)}
}
