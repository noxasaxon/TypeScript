package testfixture

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func copyInventory(t *testing.T, source, target string, files map[string]string) {
	t.Helper()
	for name := range files {
		b, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(target, name)
		if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(p, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func TestRequiredInventoryAllRoles(t *testing.T) {
	repo, err := Repository()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"promise", "optional-flow", "optional-consumer", "carrier"} {
		t.Run(key, func(t *testing.T) {
			source, err := sourceRoot(repo, key)
			if err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(Get(t, "project-output"), key)
			copyInventory(t, source, out, fixtureHashes[key])
			if key == "promise" {
				t.Setenv("TSOX_PROMISE_SOURCE", out)
			} else {
				t.Setenv("TSOX_SOURCE_FIXTURES", filepath.Dir(out))
			}
			if _, err = sourceRoot(repo, key); err != nil {
				t.Fatal(err)
			}
			extra := filepath.Join(out, "review-extra.ts")
			if err = os.WriteFile(extra, []byte("throw 1"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = sourceRoot(repo, key); err == nil {
				t.Fatal("extra immutable source accepted")
			}
			if err = os.Remove(extra); err != nil {
				t.Fatal(err)
			}
			if err = os.Mkdir(filepath.Join(out, "unlisted"), 0700); err != nil {
				t.Fatal(err)
			}
			if _, err = sourceRoot(repo, key); err == nil {
				t.Fatal("extra source directory accepted")
			}
			if err = os.Remove(filepath.Join(out, "unlisted")); err != nil {
				t.Fatal(err)
			}
			for name := range fixtureHashes[key] {
				p := filepath.Join(out, name)
				b, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(p); err != nil {
					t.Fatal(err)
				}
				if _, err = sourceRoot(repo, key); err == nil {
					t.Fatal("missing immutable source accepted")
				}
				if err = os.WriteFile(p, append(b, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err = sourceRoot(repo, key); err == nil {
					t.Fatal("changed immutable source accepted")
				}
				if err = os.WriteFile(p, b, 0600); err != nil {
					t.Fatal(err)
				}
				break
			}
			if _, err = sourceRoot(repo, key); err != nil {
				t.Fatal("restored exact inventory rejected", err)
			}
		})
	}
}
func TestPortfolioActualRootAndBoundaries(t *testing.T) {
	repo, err := Repository()
	if err != nil {
		t.Fatal(err)
	}
	source, err := portfolio(repo)
	if err != nil {
		t.Fatal(err)
	}
	consumer := Get(t, "package-output")
	selected := filepath.Join(consumer, "release/portfolio")
	backend := filepath.Join(consumer, "go/difftest/testdata/accepted/backend-modules")
	copyInventory(t, source, selected, fixtureHashes["portfolio-tree"])
	copyInventory(t, source, selected, fixtureHashes["installed-packages"])
	copyInventory(t, filepath.Join(repo, "go/difftest/testdata/accepted/backend-modules"), backend, fixtureHashes["backend-tree"])
	t.Setenv("TSOX_PACKAGE_PORTFOLIO", selected)
	if _, err = portfolio(repo); err != nil {
		t.Fatal(err)
	}
	for _, boundary := range []string{selected, backend} {
		p := filepath.Join(boundary, "review-extra.ts")
		if err = os.WriteFile(p, []byte("export const extra=1"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = portfolio(repo); err == nil {
			t.Fatal("extra multi-root portfolio input accepted", boundary)
		}
		if err = os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	route := filepath.Join(selected, "web-api/route.ts")
	original, err := os.ReadFile(route)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(route, append(original, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = portfolio(repo); err == nil {
		t.Fatal("changed selected canonical tree accepted")
	}
	if err = os.WriteFile(route, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(route); err != nil {
		t.Fatal(err)
	}
	if _, err = portfolio(repo); err == nil {
		t.Fatal("missing selected canonical source accepted")
	}
	if err = os.WriteFile(route, original, 0600); err != nil {
		t.Fatal(err)
	}
	// Exact review witness: an unchanged canonical sibling must not attest the
	// changed alternate selected tree, even with genuine package installation.
	alternate := filepath.Join(consumer, "release/alternate")
	copyInventory(t, source, alternate, fixtureHashes["portfolio-tree"])
	copyInventory(t, source, alternate, fixtureHashes["installed-packages"])
	if err = os.WriteFile(filepath.Join(alternate, "web-api/route.ts"), append(original, []byte("\n// changed selected bytes\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TSOX_PACKAGE_PORTFOLIO", alternate)
	if _, err = portfolio(repo); err == nil {
		t.Fatal("unchanged sibling attested changed selected portfolio")
	}
}
func TestConcurrentWorkspaceIdentity(t *testing.T) {
	var wg sync.WaitGroup
	paths := make(chan string, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); paths <- Get(t, "project-output") }()
	}
	wg.Wait()
	close(paths)
	first := ""
	for p := range paths {
		if first == "" {
			first = p
		}
		if p != first {
			t.Fatal("same test lost workspace identity")
		}
	}
	var mu sync.Mutex
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		t.Run("parallel", func(t *testing.T) {
			t.Parallel()
			p := Get(t, "project-output")
			mu.Lock()
			defer mu.Unlock()
			if seen[p] {
				t.Fatal("parallel tests share workspace")
			}
			seen[p] = true
		})
	}
}
