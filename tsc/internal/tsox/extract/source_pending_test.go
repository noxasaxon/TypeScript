package extract

import (
	"path/filepath"
	"testing"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestPendingLexicalRoleSuccessor(t *testing.T) {
	root := sourcefixture.Get(t, "source-flow-output")
	if root == "" {
		t.Fatal("explicit scratch output")
	}
	dir := filepath.Join(root, "lexical")
	cjsBodyWrite(t, filepath.Join(dir, "package.json"), `{"type":"module"}`)
	cjsBodyWrite(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"strict":true,"module":"NodeNext","allowImportingTsExtensions":true,"noEmit":true},"files":["entry.ts"]}`)
	cjsBodyWrite(t, filepath.Join(dir, "entry.ts"), `import {read as local} from './subject.cjs';import type {Shape as ImportedType} from './types.ts';const key=1;const obj={key:2};const short={key};interface Shape{key:number};type Other={key:number};class Box{field=1;method(){return this.field}};const {key:renamed}=obj;export {local as publicName,obj,short,Box,renamed};export type{ImportedType};`)
	cjsBodyWrite(t, filepath.Join(dir, "types.ts"), `export interface Shape{key:number}`)
	cjsBodyWrite(t, filepath.Join(dir, "subject.cjs"), `exports.read=()=>1;`)
	p := cjsDomainSnapshot(t, filepath.Join(dir, "tsconfig.json"), "entry.ts")
	s, e := checked.NewSourceRecoveryScope(p)
	if e != nil {
		t.Fatal(e)
	}
	file := p.Program.Entry
	var nodes []*ast.Node
	var walk func(*ast.Node) bool
	walk = func(n *ast.Node) bool {
		if n == nil {
			return false
		}
		if n.Kind == ast.KindIdentifier {
			nodes = append(nodes, n)
		}
		n.ForEachChild(walk)
		return false
	}
	walk(file.AsNode())
	var local, key uint32
	rejected := 0
	for _, n := range nodes {
		p := n.Parent
		wantReject := false
		switch p.Kind {
		case ast.KindPropertyAssignment, ast.KindPropertySignature, ast.KindPropertyDeclaration, ast.KindMethodDeclaration:
			wantReject = p.Name() == n
		case ast.KindPropertyAccessExpression:
			wantReject = p.Name() == n
		case ast.KindBindingElement:
			wantReject = p.Name() != n
		case ast.KindImportSpecifier:
			wantReject = p.Name() != n || n.Text() == "ImportedType"
		case ast.KindExportSpecifier:
			wantReject = n.Text() == "publicName" || n.Text() == "ImportedType"
		}
		h, e := s.LexicalBinding(n)
		if wantReject {
			if e == nil {
				t.Fatalf("nonlexical %s %s accepted", p.Kind, n.Text())
			}
			rejected++
			continue
		}
		if e != nil {
			continue
		}
		id, e := s.BindingID(h)
		if e != nil {
			t.Fatal(e)
		}
		if n.Text() == "local" {
			if local != 0 && uint32(id) != local {
				t.Fatal("local export/import alias identity split")
			}
			local = uint32(id)
		}
		if n.Text() == "key" && (p.Kind == ast.KindVariableDeclaration || p.Kind == ast.KindShorthandPropertyAssignment) {
			if key != 0 && uint32(id) != key {
				t.Fatal("shorthand not actual lexical value")
			}
			key = uint32(id)
		}
	}
	if rejected < 9 || local == 0 || key == 0 {
		t.Fatal("key contexts omitted", rejected)
	}
	for _, binding := range s.View().Bindings {
		for _, d := range binding.Declarations {
			site := s.View().Declarations[d-1].Site
			if site.Kind == "KindPropertyAssignment" || site.Kind == "KindPropertySignature" || site.Kind == "KindMethodDeclaration" || site.Kind == "KindPropertyDeclaration" {
				t.Fatal("nonlexical declaration in lexical registry", site)
			}
		}
	}
}
