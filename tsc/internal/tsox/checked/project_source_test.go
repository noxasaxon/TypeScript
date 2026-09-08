package checked

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/internal/ast"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestProjectSourceSameCapturedProgram(t *testing.T) {
	root := sourcefixture.Get(t, "promise")
	if root == "" {
		t.Fatal("TSOX_PROMISE_SOURCE required")
	}
	p, ds := ReadProjectWithOptions(filepath.Join(root, "tsconfig.json"), "handler.ts", ProjectOptions{DependencyTypesInferJS})
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	s, err := p.SourceSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if s.Program.Compiler != p.dependency.Program || s.ConfiguredRoots != 2 {
		t.Fatal("new Program or missing configured root")
	}
	if len(s.RuntimeModules) != len(p.dependency.Manifest.Modules) {
		t.Fatal("runtime closure changed")
	}
	for i, m := range s.RuntimeModules {
		old := p.dependency.Manifest.Modules[i]
		if m.ID != old.ID || m.Format != old.Format || len(m.Dependencies) != len(old.Dependencies) {
			t.Fatal("runtime order/identity changed")
		}
		for j, edge := range m.Dependencies {
			if edge != old.Dependencies[j] {
				t.Fatal("runtime edge changed")
			}
		}
	}
	scope, err := NewSourceRecoveryScope(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(scope.View().Obligations) == 0 {
		t.Fatal("source registration discharged startup")
	}
	original := p.Entry
	p.Entry = "changed"
	if _, err := p.SourceSnapshot(); err == nil {
		t.Fatal("changed public entry accepted")
	}
	p.Entry = original
	if _, err := p.SourceSnapshot(); err != nil {
		t.Fatal(err)
	}
	if _, err := (*Project)(nil).SourceSnapshot(); err == nil {
		t.Fatal("nil accepted")
	}
	if _, err := (&Project{}).SourceSnapshot(); err == nil {
		t.Fatal("counterfeit snapshot accepted")
	}
}

func TestProjectSourceWholeConfiguredPortfolio(t *testing.T) {
	root := sourcefixture.Get(t, "portfolio")
	if root == "" {
		t.Fatal("TSOX_PACKAGE_PORTFOLIO required")
	}
	p, ds := ReadProjectWithOptions(filepath.Join(root, "tsconfig.json"), "web-api/route.ts", ProjectOptions{DependencyTypesInferJS})
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	s, err := p.SourceSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if s.ConfiguredRoots != 10 || s.Program.Compiler != p.dependency.Program {
		t.Fatal("configured source inventory or Program replaced")
	}
	found := false
	for file, name := range s.Program.Files {
		if strings.HasSuffix(name, "/web-api/model.ts") {
			found = true
			for _, runtime := range s.Program.RuntimeFiles {
				if runtime == file {
					t.Fatal("schema treated as executed module")
				}
			}
		}
	}
	if !found {
		t.Fatal("owned schema omitted")
	}
}

// The public Project already exposes its checked Program through Check. A source
// seal taken later at registration would bless this intervening syntax mutation.
func TestProjectSourceRejectsMutationBeforeRegistration(t *testing.T) {
	root := sourcefixture.Get(t, "promise")
	if root == "" {
		t.Fatal("source required")
	}
	p, ds := ReadProjectWithOptions(filepath.Join(root, "tsconfig.json"), "handler.ts", ProjectOptions{DependencyTypesInferJS})
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	original, ds := p.Check()
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	var declaration *ast.Node
	for _, s := range original.Entry.Statements.Nodes {
		if s.Kind == ast.KindFunctionDeclaration {
			declaration = s
			break
		}
	}
	if declaration == nil {
		t.Fatal("actual handler declaration absent")
	}
	flags := declaration.Flags
	declaration.Flags ^= ast.NodeFlagsAmbient
	if _, err := p.SourceSnapshot(); err == nil {
		t.Error("AST mutation before source registration was blessed")
	}
	declaration.Flags = flags
	if _, err := p.SourceSnapshot(); err != nil {
		t.Fatal("restored original capture rejected", err)
	}
}

func TestProjectSourceCallableObservationsRemainUnresolved(t *testing.T) {
	root, out := sourcefixture.Get(t, "promise"), sourcefixture.Get(t, "project-output")
	if root == "" || out == "" {
		t.Fatal("explicit source/output roots required")
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	for name, extra := range map[string]string{
		"rebind": `child = async function(path:string):Promise<string>{return path;};`,
		"escape": `const alias = child;`,
	} {
		t.Run(name, func(t *testing.T) {
			dir, err := os.MkdirTemp(out, "callable-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			for _, file := range []string{"handler.ts", "child.ts", "tsconfig.json", "package.json"} {
				bytes, err := os.ReadFile(filepath.Join(root, file))
				if err != nil {
					t.Fatal(err)
				}
				if file == "child.ts" {
					bytes = append(bytes, []byte("\n"+extra+"\n")...)
				}
				if err := os.WriteFile(filepath.Join(dir, file), bytes, 0600); err != nil {
					t.Fatal(err)
				}
			}
			p, ds := ReadProjectWithOptions(filepath.Join(dir, "tsconfig.json"), "handler.ts", ProjectOptions{DependencyTypesInferJS})
			if name == "rebind" {
				if len(ds) == 0 || !strings.Contains(ds[0].String(), "Cannot assign to 'child' because it is a function") {
					t.Fatal("original checker rebind control changed", ds)
				}
				t.Log("source rebind rejected by checker before source facts", ds[0].String())
				return
			}
			if len(ds) != 0 {
				t.Fatal(ds)
			}
			snapshot, err := p.SourceSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			scope, err := NewSourceRecoveryScope(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			ops, err := scope.OriginalStartup()
			if err != nil {
				t.Fatal(err)
			}
			pending := 0
			for _, op := range ops {
				if op.Kind == StartupSourceUnresolved {
					pending++
				}
			}
			if pending != 1 {
				t.Fatalf("evaluated initializer omitted: %d", pending)
			}
			var call *ast.Node
			var visit func(*ast.Node) bool
			visit = func(n *ast.Node) bool {
				if n.Kind == ast.KindCallExpression && n.AsCallExpression().Expression.Kind == ast.KindIdentifier && n.AsCallExpression().Expression.Text() == "child" {
					call = n
				}
				n.ForEachChild(visit)
				return false
			}
			visit(snapshot.Program.Entry.AsNode())
			if call == nil {
				t.Fatal("actual call absent")
			}
			h, err := scope.SourceHandle(call)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := scope.OriginalCallableTarget(h); err == nil {
				t.Fatal("alias/rebound function granted direct target facts")
			}
		})
	}
}
