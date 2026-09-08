package extract

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
)

func (b *builder) standardType(t *checker.Type) graph.TypeKind {
	if !b.standardEntry || t == nil {
		return ""
	}
	for _, name := range []string{"Request", "Response"} {
		g := b.checker.GetGlobalSymbol(name, ast.SymbolFlagsType, nil)
		if g != nil && t.Symbol() == g && b.platformLibrarySymbol(g) {
			if name == "Request" {
				return graph.TypeRequest
			}
			return graph.TypeResponse
		}
	}
	return ""
}
func (b *builder) standardProducer(n *ast.Node) (*graph.AsyncProducer, graph.Type, *fenceError, bool) {
	if !b.standardEntry || n.Kind != ast.KindCallExpression {
		return nil, graph.Type{}, nil, false
	}
	c := n.AsCallExpression()
	p := &graph.AsyncProducer{Position: b.position(n)}
	if c.QuestionDotToken != nil || c.TypeArguments != nil {
		return nil, graph.Type{}, nil, false
	}
	if c.Expression.Kind == ast.KindPropertyAccessExpression {
		member := c.Expression.AsPropertyAccessExpression()
		kind := b.standardType(b.checker.GetTypeAtLocation(member.Expression))
		if kind != graph.TypeRequest && kind != graph.TypeResponse {
			return nil, graph.Type{}, nil, false
		}
		name := member.Name().Text()
		if name != "json" && name != "text" {
			return nil, graph.Type{}, nil, false
		}
		globalName := "Request"
		if kind == graph.TypeResponse {
			globalName = "Response"
		}
		g := b.checker.GetGlobalSymbol(globalName, ast.SymbolFlagsType, nil)
		expected := b.checker.GetPropertyOfType(b.checker.GetDeclaredTypeOfSymbol(g), name)
		if expected == nil || b.checker.GetSymbolAtLocation(member.Name()) != expected {
			return nil, graph.Type{}, b.fenceWithMessage(n, "body operation lacks intrinsic member identity"), true
		}
		if !b.platformLibrarySymbol(expected) {
			return nil, graph.Type{}, b.fenceWithMessage(n, "body member modified by non-intrinsic declaration"), true
		}
		if member.QuestionDotToken != nil {
			return nil, graph.Type{}, b.fenceWithMessage(n, "optional body invocation needs receiver contract"), true
		}
		r, f := b.expression(member.Expression)
		if f != nil {
			return nil, graph.Type{}, f, true
		}
		p.Receiver = r
		if name == "json" {
			p.Kind = graph.ProducerBodyJSON
			p.Contract = graph.ProducerBodyJSONCollectedNode24
		} else {
			p.Kind = graph.ProducerBodyText
			p.Contract = graph.ProducerBodyTextCollectedNode24
		}
	} else if c.Expression.Kind == ast.KindIdentifier {
		symbol := b.sourceSymbol(c.Expression)
		if symbol == nil || symbol.Name != "readFile" {
			return nil, graph.Type{}, nil, false
		}
		intrinsic := len(symbol.Declarations) > 0
		for _, d := range symbol.Declarations {
			intrinsic = intrinsic && ast.GetSourceFileOfNode(d).FileName() == checked.StandardNodeDeclarationPath
		}
		if !intrinsic {
			return nil, graph.Type{}, nil, false
		}
		p.Kind = graph.ProducerFileUTF8
		p.Contract = graph.ProducerFileUTF8Node24
	} else {
		return nil, graph.Type{}, nil, false
	}
	for _, arg := range c.Arguments.Nodes {
		if arg.Kind == ast.KindSpreadElement {
			return nil, graph.Type{}, b.fence(arg), true
		}
		v, f := b.expression(arg)
		if f != nil {
			return nil, graph.Type{}, f, true
		}
		p.Arguments = append(p.Arguments, v)
	}
	result := graph.Type{Kind: graph.TypeString}
	if p.Kind == graph.ProducerBodyJSON {
		result.Kind = graph.TypeUnknown
	}
	if p.Kind == graph.ProducerFileUTF8 {
		if len(p.Arguments) != 2 || p.Arguments[0].Type.Kind != graph.TypeString || p.Arguments[0].Type.Optional || p.Arguments[1].Kind != graph.ExpressionString || p.Arguments[1].String != "utf8" {
			return nil, graph.Type{}, b.fenceWithMessage(n, "file producer requires the contracted string/utf8 overload"), true
		}
	}
	return p, result, nil, true
}
