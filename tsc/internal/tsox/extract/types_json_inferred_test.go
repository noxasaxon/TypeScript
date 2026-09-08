package extract

import (
	"context"
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
	"testing"
)

func inferredField(t *testing.T, p *graph.Program, typ graph.Type, name string) graph.Field {
	t.Helper()
	for _, s := range p.Shapes {
		if s.ID == typ.Shape {
			for _, f := range s.Fields {
				if f.Name == name {
					return f
				}
			}
		}
	}
	t.Fatalf("missing %s in %+v", name, typ)
	return graph.Field{}
}

// This helper intentionally extracts only source-backed result storage. It does
// not certify the helper body, evaluated inputs, constructors, or aliases.
func inferredResultLayout(t *testing.T, source string) (*graph.Program, graph.Type) {
	t.Helper()
	p, ds := checked.New("layout.ts", map[string]string{"layout.ts": source})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	c, done := p.Compiler.GetTypeChecker(context.Background())
	defer done()
	b := &builder{jsonValues: true, file: p.Entry, sourcePath: p.Entry.FileName(), checker: c, moduleFiles: p.Files, shapeIDs: map[*ast.Symbol]graph.ShapeID{}, shapeBuilding: map[*ast.Symbol]bool{}}
	var fn *ast.Node
	for _, node := range p.Entry.Statements.Nodes {
		if node.Kind == ast.KindFunctionDeclaration {
			fn = node
		}
	}
	if fn == nil {
		t.Fatal("actual source function missing")
	}
	signatures := c.GetSignaturesOfType(c.GetTypeAtLocation(fn), checker.SignatureKindCall)
	if len(signatures) != 1 {
		t.Fatal("source signature missing")
	}
	typ, f := b.graphType(c.GetReturnTypeOfSignature(signatures[0]), fn)
	if f != nil {
		t.Fatal(f.diagnostic)
	}
	return &graph.Program{Shapes: b.shapes}, typ
}

func TestJSONInferredLayoutActualSource(t *testing.T) {
	p, result := inferredResultLayout(t, `interface Item {title:string; tags:string[]; detail?:{owner:string;note?:string}} function summarize(items:Item[]){return {total:items.length, tasks:items.map(item=>({title:item.title,tags:item.tags,owner:item.detail?.owner,note:item.detail?.note}))};}`)
	tasks := inferredField(t, p, result, "tasks")
	if tasks.Type.Kind != graph.TypeArray || tasks.Type.Element == nil {
		t.Fatal(tasks)
	}
	for _, name := range []string{"owner", "note"} {
		f := inferredField(t, p, *tasks.Type.Element, name)
		if f.Type.Kind != graph.TypeString || !f.Type.Optional {
			t.Fatal(f)
		}
	}
	tags := inferredField(t, p, *tasks.Type.Element, "tags")
	if tags.Type.Kind != graph.TypeArray || tags.Type.Element.Kind != graph.TypeString {
		t.Fatal(tags)
	}
	if inferredField(t, p, result, "total").Type.Optional {
		t.Fatal("required field widened")
	}
	if len(p.Statements) != 0 {
		t.Fatal("storage schema fabricated construction proof")
	}
}

func TestJSONInferredLayoutShorthandAndNested(t *testing.T) {
	p, result := inferredResultLayout(t, `function make(note:string|undefined){const nested={note};const alias=nested;return {nested,alias};}`)
	n := inferredField(t, p, result, "nested")
	a := inferredField(t, p, result, "alias")
	if n.Type.Shape != a.Type.Shape {
		t.Fatal("same actual source type split", n, a)
	}
	if !inferredField(t, p, n.Type, "note").Type.Optional {
		t.Fatal("undefined lost")
	}
	// Shared shape identity describes storage only. It is not object identity or
	// freshness; the source graph must retain the actual identifier edges later.
}

func TestJSONInferredLayoutDistinctInstantiations(t *testing.T) {
	p, result := inferredResultLayout(t, `function pair(){return {first:{value:1},second:{value:"x"}};}`)
	first := inferredField(t, p, result, "first")
	second := inferredField(t, p, result, "second")
	if first.Type.Shape == second.Type.Shape || inferredField(t, p, first.Type, "value").Type.Kind != graph.TypeNumber || inferredField(t, p, second.Type, "value").Type.Kind != graph.TypeString {
		t.Fatal("inferred storage collapsed")
	}
}

func TestJSONInferredLayoutBoundaries(t *testing.T) {
	for name, src := range map[string]string{
		"accessor":  `function make(){return {get value(){return 1}};}`,
		"computed":  `const key="value";function make(){return {[key]:1};}`,
		"prototype": `function make(){return {__proto__:{value:1}};}`,
		"spread":    `function make(){const x={value:1};return {...x};}`,
		"assertion": `function make(raw:unknown){return raw as {value:string};}`,
		"recursive": `interface R {next?:R} function make(x:R){return {x};}`,
	} {
		t.Run(name, func(t *testing.T) {
			r := extractSource("boundary.ts", src, true)
			if r.Program != nil || len(r.Diagnostics) == 0 {
				t.Fatal("unsupported semantics admitted")
			}
		})
	}
}
