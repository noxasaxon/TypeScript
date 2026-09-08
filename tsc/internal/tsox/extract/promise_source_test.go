package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestPromiseSourceFirstFamily(t *testing.T) {
	root := sourcefixture.Get(t, "promise")
	if root == "" {
		t.Fatal("TSOX_PROMISE_SOURCE required")
	}
	snapshot, err := checked.ReadCJSBodySourceSnapshot(filepath.Join(root, "tsconfig.json"), "handler.ts", checked.CJSExportBoundary{})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := checked.NewSourceRecoveryScope(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	bodies, err := RegisterScopedTypedBodies(scope, "handle")
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies.Handles()) != 2 {
		t.Fatalf("want complete root and actual helper, got %d", len(bodies.Handles()))
	}
	for _, handle := range bodies.Handles() {
		body, err := handle.ResolveSourceBody()
		if err != nil {
			t.Fatal(err)
		}
		if body.Async == nil || body.Registry.Scope != scope.View().Scope {
			t.Fatal("actual same-scope async body required")
		}
		if len(body.Obligations) == 0 {
			t.Fatal("source recovery cannot discharge startup/instance/promise proof")
		}
	}
}

func TestPromiseSourceBuiltinRoles(t *testing.T) {
	root := sourcefixture.Get(t, "promise")
	if root == "" {
		t.Fatal("TSOX_PROMISE_SOURCE required")
	}
	snapshot, err := checked.ReadCJSBodySourceSnapshot(filepath.Join(root, "tsconfig.json"), "handler.ts", checked.CJSExportBoundary{})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := checked.NewSourceRecoveryScope(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	view := scope.View()
	builtin := 0
	for _, module := range view.Modules {
		if module.Format != "builtin" {
			continue
		}
		builtin++
		if module.File != 0 || len(module.Startup) != 0 || len(module.Templates) != 0 || module.StartupStatus != "external-contract-pending" {
			t.Fatal("builtin acquired source body or startup proof", module)
		}
		linked := false
		for _, imp := range view.Imports {
			if imp.Target == module.ID {
				linked = true
				if imp.Local == 0 || len(imp.CandidateBodies) != 0 || len(imp.TypeDeclarations) == 0 {
					t.Fatal("builtin source import identity/declaration separation lost", imp)
				}
			}
		}
		if !linked {
			t.Fatal("builtin import absent")
		}
	}
	if builtin != 1 {
		t.Fatalf("want one actual bodyless module, got %d", builtin)
	}
}

func TestPromiseSourceAliasedAndShadowedFile(t *testing.T) {
	root := sourcefixture.Get(t, "promise")
	if root == "" {
		t.Fatal("TSOX_PROMISE_SOURCE required")
	}
	out := sourcefixture.Get(t, "project-output")
	for _, tc := range []struct {
		name     string
		replace  func(string) string
		producer int
		calls    int
	}{
		{"import-alias", func(s string) string {
			return strings.ReplaceAll(strings.Replace(s, "{ readFile }", "{ readFile as actualRead }", 1), "await readFile(", "await actualRead(")
		}, 1, 2},
		{"own-shadow", func(s string) string {
			return strings.Replace(s, `import { readFile } from "node:fs/promises";`, `async function readFile(path: string, encoding: string): Promise<string> { return path; }`, 1)
		}, 0, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, err := os.MkdirTemp(out, tc.name+"-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(dir) })
			for _, name := range []string{"package.json", "tsconfig.json", "handler.ts", "child.ts"} {
				data, err := os.ReadFile(filepath.Join(root, name))
				if err != nil {
					t.Fatal(err)
				}
				if name == "child.ts" {
					data = []byte(tc.replace(string(data)))
				}
				if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := checked.ReadCJSBodySourceSnapshot(filepath.Join(dir, "tsconfig.json"), "handler.ts", checked.CJSExportBoundary{})
			if err != nil {
				t.Fatal(err)
			}
			scope, err := checked.NewSourceRecoveryScope(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			bodies, err := RegisterScopedTypedBodies(scope, "handle")
			if err != nil {
				t.Fatal(err)
			}
			producers, calls := 0, 0
			for _, h := range bodies.Handles() {
				body, err := h.ResolveSourceBody()
				if err != nil {
					t.Fatal(err)
				}
				for _, stage := range body.Async.Stages {
					if stage.Await.Producer != nil {
						producers++
					}
				}
				for _, x := range body.GraphExpressions {
					if x.Kind == graph.ExpressionCallAsync {
						calls++
						if x.Pending == nil || x.Pending.CandidateSetComplete {
							t.Fatal("candidate became executable callee proof")
						}
					}
				}
			}
			if producers != tc.producer || calls != tc.calls {
				t.Fatalf("producer/callee identity confused: %d/%d", producers, calls)
			}
		})
	}
}
