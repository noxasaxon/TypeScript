package extract

import (
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"testing"
)

func TestSourceSyntaxAllLookupGuards(t *testing.T) {
	scope, registry := scopedWeb(t)
	h := registry.Handles()[0]
	body, e := h.ResolveSourceBody()
	if e != nil {
		t.Fatal(e)
	}
	source, e := registry.BodySource(h)
	if e != nil {
		t.Fatal(e)
	}
	var fn, literal, object, operator, variable, importClause, modelMember *ast.Node
	for file := range scope.ActualProgram().Files {
		var visit func(*ast.Node) bool
		visit = func(n *ast.Node) bool {
			if n == nil {
				return false
			}
			if n.Kind == ast.KindFunctionDeclaration && n.Name() != nil && n.Name().Text() == "authenticate" {
				fn = n
			}
			if n.Kind == ast.KindStringLiteral {
				literal = n
			}
			if n.Kind == ast.KindObjectLiteralExpression && len(n.AsObjectLiteralExpression().Properties.Nodes) > 1 {
				object = n
			}
			if n.Kind == ast.KindBinaryExpression {
				operator = n.AsBinaryExpression().OperatorToken
			}
			if n.Kind == ast.KindVariableDeclarationList {
				variable = n
			}
			if n.Kind == ast.KindImportSpecifier {
				importClause = n
			}
			if file.FileName()[len(file.FileName())-8:] == "model.ts" && n.Kind == ast.KindPropertySignature {
				modelMember = n
			}
			n.ForEachChild(visit)
			return false
		}
		visit(file.AsNode())
	}
	if fn == nil || literal == nil || object == nil || operator == nil || variable == nil || importClause == nil || modelMember == nil {
		t.Fatal("missing actual source controls")
	}
	originalName := fn.Name()
	nameHandle, e := scope.SourceHandle(originalName)
	if e != nil {
		t.Fatal(e)
	}
	lexical, e := scope.LexicalBinding(originalName)
	if e != nil {
		t.Fatal(e)
	}
	check := func(t *testing.T) {
		t.Helper()
		if scope.ValidateSourceCoverage() == nil {
			t.Error("changed syntax coverage accepted")
		}
		if _, e := h.ResolveSourceBody(); e == nil {
			t.Error("changed syntax body accepted")
		}
		if _, e := scope.SourceSite(source); e == nil {
			t.Error("changed syntax source site accepted")
		}
		if _, e := scope.SourceHandle(fn); e == nil {
			t.Error("changed syntax source handle accepted")
		}
		if _, e := scope.ActualTemplate(fn); e == nil {
			t.Error("changed syntax template accepted")
		}
		if _, e := scope.ActualSite(originalName); e == nil {
			t.Error("changed syntax actual site accepted")
		}
		if _, e := scope.SourceLexical(nameHandle); e == nil {
			t.Error("changed syntax lexical handle accepted")
		}
		if _, e := scope.LexicalBinding(originalName); e == nil {
			t.Error("changed syntax lexical binding accepted")
		}
		if _, e := scope.BindingID(lexical); e == nil {
			t.Error("changed syntax lexical ID accepted")
		}
	}
	controls := []struct {
		name   string
		mutate func() func()
	}{
		{"source-entry-owner", func() func() {
			p := scope.ActualProgram()
			old := p.Entry
			for file := range p.Files {
				if file != old {
					p.Entry = file
					break
				}
			}
			return func() { p.Entry = old }
		}},
		{"runtime-source-order", func() func() {
			p := scope.ActualProgram()
			p.RuntimeFiles[0], p.RuntimeFiles[1] = p.RuntimeFiles[1], p.RuntimeFiles[0]
			return func() { p.RuntimeFiles[0], p.RuntimeFiles[1] = p.RuntimeFiles[1], p.RuntimeFiles[0] }
		}},
		{"operator", func() func() {
			old := operator.Kind
			operator.Kind = ast.KindMinusToken
			if old == ast.KindMinusToken {
				operator.Kind = ast.KindPlusToken
			}
			return func() { operator.Kind = old }
		}},
		{"literal", func() func() {
			d := literal.LiteralLikeData()
			old := d.Text
			d.Text = "changed"
			return func() { d.Text = old }
		}},
		{"declaration-flags", func() func() {
			old := variable.Flags
			variable.Flags ^= ast.NodeFlagsConst
			return func() { variable.Flags = old }
		}},
		{"import-type-only", func() func() {
			d := importClause.AsImportSpecifier()
			old := d.IsTypeOnly
			d.IsTypeOnly = !old
			return func() { d.IsTypeOnly = old }
		}},
		{"property-order", func() func() {
			list := object.AsObjectLiteralExpression().Properties
			list.Nodes[0], list.Nodes[1] = list.Nodes[1], list.Nodes[0]
			return func() { list.Nodes[0], list.Nodes[1] = list.Nodes[1], list.Nodes[0] }
		}},
		{"parent", func() func() {
			old := originalName.Parent
			originalName.Parent = literal
			return func() { originalName.Parent = old }
		}},
		{"source-span", func() func() {
			old := originalName.Loc
			originalName.Loc = core.NewTextRange(originalName.Pos()+1, originalName.End())
			return func() { originalName.Loc = old }
		}},
		{"owned-schema-member", func() func() {
			old := modelMember.Name().AsIdentifier().Text
			modelMember.Name().AsIdentifier().Text = "changedSchema"
			return func() { modelMember.Name().AsIdentifier().Text = old }
		}},
		{"modifier-kind", func() func() {
			m := fn.Modifiers().Nodes[0]
			old := m.Kind
			m.Kind = ast.KindDeclareKeyword
			return func() { m.Kind = old }
		}},
		{"same-text-foreign-list", func() func() {
			d := fn.FunctionLikeData()
			old := d.Parameters
			clone := *old
			d.Parameters = &clone
			return func() { d.Parameters = old }
		}},
		{"cycle", func() func() {
			old := fn.Body().AsBlock().Statements.Nodes[0]
			fn.Body().AsBlock().Statements.Nodes[0] = fn
			return func() { fn.Body().AsBlock().Statements.Nodes[0] = old }
		}},
	}
	for _, c := range controls {
		t.Run(c.name, func(t *testing.T) { restore := c.mutate(); defer restore(); check(t) })
		if _, e := h.ResolveSourceBody(); e != nil {
			t.Fatal("restored original rejected", c.name, e)
		}
	}
	if len(body.Registry.Obligations) == 0 {
		t.Fatal("lost pending obligations")
	}
}
