package checked

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func middlewareWrite(t *testing.T, name string, value any) {
	t.Helper()
	root := sourcefixture.Get(t, "middleware-output")
	if root == "" {
		t.Fatal("explicit output required")
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestMiddlewareSourceActual(t *testing.T) {
	portfolio := sourcefixture.Get(t, "portfolio")
	if portfolio == "" {
		t.Fatal("actual frozen project required")
	}
	p, ds := ReadProjectWithOptions(filepath.Join(portfolio, "tsconfig.json"), "web-api/route.ts", ProjectOptions{DependencyTypesInferJS})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	checked, ds := p.Check()
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if _, _, boundary := AsyncEntry(checked, "handle"); boundary == nil || !strings.Contains(boundary.Message, "function declaration") {
		t.Fatal("existing computed entry boundary changed", boundary)
	}
	plan, err := discoverCallableEntry(checked, "handle")
	if err != nil {
		t.Fatal(err)
	}
	if plan.StartupCertified {
		t.Fatal("source discovery certified startup")
	}
	entry := plan.Values[plan.EntryValue-1]
	if entry.Kind != "callable-instance" {
		t.Fatal(entry)
	}
	selected := plan.Instances[entry.Instance-1]
	if selected.Creation.Position.SourcePath != filepath.Join(portfolio, "web-api/auth.ts") || selected.Creation.Position.Line != 17 {
		t.Fatal(selected)
	}
	// Trace actual captured next cells, with two wrappers before the actual route.
	for _, line := range []int{17, 11} {
		if len(selected.Returns) != 1 || selected.Returns[0].Kind != "return-promise-adoption" || len(selected.Returns[0].EvaluatedOperands) != 3 {
			t.Fatal("actual return-next lost callee/arguments/adoption", selected)
		}

		if selected.Creation.Position.Line != line {
			t.Fatal("wrapper chain changed", selected)
		}
		var next int
		for _, capture := range selected.Captures {
			v := plan.Values[capture.Value-1]
			if v.Kind == "callable-instance" {
				candidate := plan.Instances[v.Instance-1]
				if strings.HasSuffix(candidate.Creation.Position.SourcePath, "/auth.ts") && candidate.Creation.Position.Line == 11 || strings.HasSuffix(candidate.Creation.Position.SourcePath, "/route.ts") && candidate.Creation.Position.Line == 5 {
					next = v.Instance
				}
			}
		}
		if next == 0 {
			t.Fatal("actual next capture missing", selected)
		}
		selected = plan.Instances[next-1]
	}
	if selected.Creation.Position.SourcePath != filepath.Join(portfolio, "web-api/route.ts") || selected.Creation.Position.Line != 5 {
		t.Fatal("leaf identity changed", selected)
	}
	hashes := map[string]string{}
	for _, file := range checked.RuntimeFiles {
		sum := sha256.Sum256([]byte(file.Text()))
		hashes[file.FileName()] = fmtHash(sum)
	}
	middlewareWrite(t, "actual-source-hashes.json", hashes)
	middlewareWrite(t, "actual-plan.json", plan)
}
func fmtHash(v [32]byte) string {
	const digits = "0123456789abcdef"
	s := make([]byte, 64)
	for i, x := range v {
		s[2*i] = digits[x>>4]
		s[2*i+1] = digits[x&15]
	}
	return string(s)
}
func TestMiddlewareSourceInstanceIdentity(t *testing.T) {
	source := `async function a(request:Request):Promise<Response>{return new Response("a");}async function b(request:Request):Promise<Response>{return new Response("b");}function wrap(next:(request:Request)=>Promise<Response>){return async(request:Request)=>{return next(request);};}const left=wrap(a);const right=wrap(b);export const handle=left;`
	p, ds := New("entry.ts", map[string]string{"entry.ts": source})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	plan, err := discoverCallableEntry(p, "handle")
	if err != nil {
		t.Fatal(err)
	}
	instances := map[string][]callableInstance{}
	for _, i := range plan.Instances {
		instances[i.Template] = append(instances[i.Template], i)
	}
	found := false
	for _, group := range instances {
		if len(group) == 2 {
			found = true
			if group[0].ID == group[1].ID {
				t.Fatal("factory instances collapsed")
			}
			// One template captures two distinct invocation-local next binding cells.
			var x, y callableCell
			for _, c := range group[0].Captures {
				x = c
			}
			for _, c := range group[1].Captures {
				y = c
			}
			if x.ID == 0 || y.ID == 0 || x.ID == y.ID || x.Value == y.Value {
				t.Fatal("lexical environments collapsed", x, y)
			}
		}
	}
	if !found {
		t.Fatal("same source template not exercised twice")
	}
	middlewareWrite(t, "instance-plan.json", plan)
}

func TestMiddlewareSourceDeclarationEntry(t *testing.T) {
	source := `export async function handle(request:Request):Promise<Response>{return new Response("ok");}`
	p, ds := New("entry.ts", map[string]string{"entry.ts": source})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	selected, name, boundary := AsyncEntry(p, "handle")
	if boundary != nil || name != "handle" || selected.Entry != p.Entry {
		t.Fatal("declaration entry path changed", name, boundary)
	}
	plan, err := discoverCallableEntry(p, "handle")
	if err != nil {
		t.Fatal(err)
	}
	value := plan.Values[plan.EntryValue-1]
	if value.Kind != "callable-instance" || len(plan.Instances[value.Instance-1].Captures) != 0 {
		t.Fatal("declaration not zero-capture instance")
	}
}
func TestMiddlewareSourceUnresolvedStartup(t *testing.T) {
	source := `function factory(){console.log("startup-effect");return async(request:Request)=>{return new Response("ok");};}export const handle=factory();`
	p, ds := New("entry.ts", map[string]string{"entry.ts": source})
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	plan, err := discoverCallableEntry(p, "handle")
	if err != nil {
		t.Fatal(err)
	}
	if plan.StartupCertified || len(plan.Pending) == 0 {
		t.Fatal("unknown factory effect was declared pure")
	}
	found := false
	for _, op := range plan.Operations {
		if op.Kind == "retained-source-obligation" {
			found = true
		}
	}
	if !found {
		t.Fatal("factory source effect dropped")
	}
	middlewareWrite(t, "startup-obligation-plan.json", plan)
}
