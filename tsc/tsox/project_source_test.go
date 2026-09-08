package tsox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/tsox/graph"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestProjectSourceCapturedPublicBodies(t *testing.T) {
	root, out := sourcefixture.Get(t, "promise"), sourcefixture.Get(t, "project-output")
	if root == "" || out == "" {
		t.Fatal("explicit source and output roots required")
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(out, "snapshot-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	for _, name := range []string{"handler.ts", "child.ts", "tsconfig.json", "package.json"} {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, ds := ReadProjectWithOptions(filepath.Join(dir, "tsconfig.json"), "handler.ts", ProjectOptions{DependencyTypes: DependencyTypesInferJS})
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	first, err := p.RegisteredSourceBodies("handle")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 {
		t.Fatal("whole source body closure omitted")
	}
	before, err := first[0].ResolveSourceBody()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"handler.ts", "child.ts", "tsconfig.json", "package.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("invalid changed disk"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	files := p.SourceFiles()
	files[p.Entry] = "changed map"
	manifest := p.ResolutionManifest()
	manifest[0] = '!'
	second, err := p.RegisteredSourceBodies("handle")
	if err != nil {
		t.Fatal("recaptured disk", err)
	}
	after, err := second[0].ResolveSourceBody()
	if err != nil {
		t.Fatal(err)
	}
	if before.Registry.SnapshotFingerprint != after.Registry.SnapshotFingerprint || before.Registry.Scope == after.Registry.Scope {
		t.Fatal("snapshot changed or scopes share authority")
	}
	if !json.Valid(p.ResolutionManifest()) || !strings.Contains(p.SourceFiles()[p.Entry], "Promise.all") {
		t.Fatal("defensive metadata mutated capture")
	}
	// A graph from another registration does not become the first handle's body.
	old := before.Program
	before.Program = after.Program
	if _, err := first[0].ResolveSourceBody(); err == nil {
		t.Fatal("foreign registration graph accepted")
	}
	before.Program = old
	if _, err := first[0].ResolveSourceBody(); err != nil {
		t.Fatal(err)
	}
	entry := p.Entry
	p.Entry = "forged"
	if _, err := p.RegisteredSourceBodies("handle"); err == nil {
		t.Fatal("forged public identity accepted")
	}
	p.Entry = entry
	if _, err := (*Project)(nil).RegisteredSourceBodies("handle"); err == nil {
		t.Fatal("nil source project accepted")
	}
}

func TestProjectSourceRetainsUnrelatedStrictDiagnostic(t *testing.T) {
	out := sourcefixture.Get(t, "project-output")
	if out == "" {
		t.Fatal("explicit output required")
	}
	dir, err := os.MkdirTemp(out, "strict-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	files := map[string]string{
		"package.json":  `{"type":"module"}`,
		"tsconfig.json": `{"compilerOptions":{"strict":true,"module":"NodeNext","allowImportingTsExtensions":true,"noEmit":true},"files":["entry.ts","other.ts"]}`,
		"entry.ts":      `export async function handle():Promise<string>{return "ok"}`,
		"other.ts":      `export const bad:string=3;`,
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	p, ds := ReadProjectWithOptions(filepath.Join(dir, "tsconfig.json"), "entry.ts", ProjectOptions{DependencyTypes: DependencyTypesInferJS})
	if p != nil || len(ds) == 0 || !strings.Contains(ds[0].String(), "other.ts") {
		t.Fatal("unrelated configured checker failure discarded", ds)
	}
}

func TestProjectSourceOriginalStartupAndCalls(t *testing.T) {
	root := sourcefixture.Get(t, "promise")
	if root == "" {
		t.Fatal("source required")
	}
	p, ds := ReadProjectWithOptions(filepath.Join(root, "tsconfig.json"), "handler.ts", ProjectOptions{DependencyTypes: DependencyTypesInferJS})
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	program, err := p.RegisteredSourceProgram("handle")
	if err != nil {
		t.Fatal(err)
	}
	entry, err := program.Entry()
	if err != nil || entry.Template == 0 {
		t.Fatal("actual exported entry absent", err)
	}
	ops, err := program.OriginalStartup()
	if err != nil {
		t.Fatal(err)
	}
	imports, functions, external := 0, 0, 0
	templates := map[graph.SourceTemplateID]bool{}
	for _, op := range ops {
		switch op.Kind {
		case StartupSourceImport:
			imports++
		case StartupSourceFunction:
			functions++
			if op.Binding == 0 || op.Template == 0 {
				t.Fatal("original function identity lost")
			}
			templates[op.Template] = true
		case StartupSourceExternal:
			external++
		default:
			t.Fatalf("unaccounted original startup operation: %+v", op)
		}
	}
	if imports != 2 || functions != 2 || external != 1 {
		t.Fatalf("startup closure changed: %d/%d/%d", imports, functions, external)
	}
	ops[0].Kind = StartupSourceUnresolved
	original, err := program.OriginalStartup()
	if err != nil || original[0].Kind == StartupSourceUnresolved {
		t.Fatal("mutable detached startup became authority", err)
	}
	bodies, err := program.Bodies()
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, h := range bodies {
		b, err := h.ResolveSourceBody()
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range b.GraphExpressions {
			if x.Kind != graph.ExpressionCallAsync {
				continue
			}
			target, err := h.OriginalCallableTarget(x)
			if err != nil {
				t.Fatal(err)
			}
			if !templates[target.Template] || target.Binding == 0 {
				t.Fatal("callee not an original runtime function")
			}
			calls++
			clone := *x
			if _, err := h.OriginalCallableTarget(&clone); err == nil {
				t.Fatal("foreign call expression accepted")
			}
		}
	}
	if calls != 2 {
		t.Fatalf("actual eager call occurrences changed: %d", calls)
	}
	if _, err := (RegisteredSourceProgram{}).OriginalStartup(); err == nil {
		t.Fatal("zero program accepted")
	}
}
