package extract

import "github.com/microsoft/typescript-go/internal/ast"

// jsonLayoutProperty verifies where a storage field comes from. This is not a
// constructor, freshness, alias, or runtime-domain certificate: those obligations
// remain on the actual evaluated expression and its consumers.
func (b *builder) jsonLayoutProperty(declaration *ast.Node) *fenceError {
	if !b.ownsFile(ast.GetSourceFileOfNode(declaration)) {
		return b.fenceWithMessage(declaration, "closed layout fields require owned source declarations")
	}
	name := declaration.Name()
	if name == nil || name.Kind != ast.KindIdentifier {
		return b.fenceWithMessage(declaration, "closed layout requires explicit ordinary data-property names")
	}
	switch declaration.Kind {
	case ast.KindPropertySignature:
		member := declaration.AsPropertySignatureDeclaration()
		if member.Initializer != nil || member.Type == nil {
			return b.fence(declaration)
		}
	case ast.KindPropertyAssignment:
		if name.Text() == "__proto__" {
			return b.fenceWithMessage(declaration, "closed construction requires explicit prototype versus data-property semantics")
		}
		if declaration.Parent == nil || declaration.Parent.Kind != ast.KindObjectLiteralExpression || declaration.AsPropertyAssignment().Initializer == nil {
			return b.fence(declaration)
		}
	case ast.KindShorthandPropertyAssignment:
		if declaration.Parent == nil || declaration.Parent.Kind != ast.KindObjectLiteralExpression || declaration.AsShorthandPropertyAssignment().ObjectAssignmentInitializer != nil {
			return b.fence(declaration)
		}
	default:
		return b.fenceWithMessage(declaration, "closed layout fields require source data-property declarations")
	}
	return nil
}
