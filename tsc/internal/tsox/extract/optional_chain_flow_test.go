package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestOptionalFlowActualSourceFixtures(t *testing.T) {
	root := sourcefixture.Get(t, "optional-flow")
	if root == "" {
		t.Fatal("explicit output required")
	}
	cases := map[string]string{
		"length":           "box?.values?.length",
		"rebind-key":       "",
		"required":         "box?.zero",
		"property":         "box?.inner?.value",
		"continued":        "box?.inner.value",
		"grouped":          "(box?.inner).value",
		"grouped-optional": "(box?.inner)?.value",
		"index":            "box?.values?.[key()]",
		"grouped-index":    "(box?.values)[key()]",
		"index-followup":   "box?.rows?.[key()]?.value",
		"receiver":         "take(box)?.values?.[key()]",
		"throw-argument":   "take(badArgument())?.values?.[key()]",
		"throw-key":        "box?.values?.[explode()]",
		"zero":             "box?.zero",
		"empty":            "box?.empty",
		"false":            "box?.flag",
	}
	for name, expression := range cases {
		t.Run(name, func(t *testing.T) {
			parameterType := "Box|undefined"
			if name == "required" {
				parameterType = "Box"
			}
			source := `interface Inner {value:number} interface Box {inner?:Inner;values?:number[];rows?:Inner[];zero:number;empty:string;flag:boolean}
function key():number {console.log("key");return 0;}
function explode():number {console.log("key");throw new TypeError("key");}
function take(box:Box|undefined):Box|undefined {console.log("receiver");return box;}
function badArgument():Box|undefined {console.log("arg");throw new TypeError("arg");}
export function run(box:` + parameterType + `) {
 // @ts-ignore: retain the source's unguarded continuation/grouped access.
 return ` + expression + `;}`
			if name == "rebind-key" {
				source = `interface B{values:number[]}let current:B|undefined=undefined;
function key():number{console.log("key");current={values:[22]};return 0;}
export function run(box:B|undefined){current=box;return current?.values[key()];}`
			}
			p, ds := checked.New("case.ts", map[string]string{"case.ts": source})
			if len(ds) > 0 {
				t.Fatal(ds)
			}
			out := sourceHelperBodies(p)
			if len(out.Diagnostics) > 0 {
				t.Fatal(out.Diagnostics)
			}
			dir := filepath.Join(root, "fixtures", name)
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			for file, text := range map[string]string{"case.ts": source, "package.json": `{"type":"module"}`} {
				if err := os.WriteFile(filepath.Join(dir, file), []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			data, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "graph.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			links := 0
			walkGraphExpressions(out.Program.Statements, func(x *graph.Expression) {
				if x.OptionalChain {
					links++
					if x.OptionalLink == nil {
						t.Fatal("source link metadata missing")
					}
				}
			})
			if links == 0 {
				t.Fatal("source chain lost")
			}
		})
	}
}

func TestOptionalFlowSourceGroupingMetadata(t *testing.T) {
	for _, c := range []struct {
		expr             string
		check, continued bool
	}{
		{"box?.inner.value", false, true}, {"box?.inner?.value", true, true}, {"(box?.inner)?.value", true, false},
	} {
		p, ds := checked.New("chain.ts", map[string]string{"chain.ts": `interface I{value:number}interface B{inner:I}export function run(box:B|undefined){return ` + c.expr + `;}`})
		if len(ds) > 0 {
			t.Fatal(ds)
		}
		out := sourceHelperBodies(p)
		if len(out.Diagnostics) > 0 {
			t.Fatal(out.Diagnostics)
		}
		x := out.Program.Statements[0].Body[0].Value
		if x.OptionalLink == nil || x.OptionalLink.CheckReceiver != c.check || x.OptionalLink.ContinuesReceiverChain != c.continued {
			t.Fatal(c.expr, x.OptionalLink)
		}
	}
}

func TestOptionalFlowSourceFencedFixtures(t *testing.T) {
	root := sourcefixture.Get(t, "optional-flow")
	if root == "" {
		t.Fatal("explicit output required")
	}
	for name, expression := range map[string]string{
		"method":          "box?.values?.includes(0)",
		"link-assertion":  "box?.inner!.value",
		"group-assertion": "(box?.inner)!.value",
		"base-assertion":  "box!.inner?.value",
		"key-assertion":   "box?.values?.[index!]",
	} {
		t.Run(name, func(t *testing.T) {
			source := `interface I{value:number}interface B{inner?:I;values?:number[]}export function run(box:B|undefined,index:number|undefined){return ` + expression + `;}`
			p, ds := checked.New("boundary.ts", map[string]string{"boundary.ts": source})
			if len(ds) > 0 {
				t.Fatal(ds)
			}
			out := sourceHelperBodies(p)
			if len(out.Diagnostics) > 0 {
				t.Fatal(out.Diagnostics)
			}
			dir := filepath.Join(root, "fences", name)
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			data, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "graph.json"), data, 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "source.ts"), []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
		})
	}
}
