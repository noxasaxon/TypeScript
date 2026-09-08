package testfixture

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRequiredFrozenSeeds(t *testing.T) {
	for _, key := range []string{"optional-flow", "optional-consumer", "carrier"} {
		t.Run(key, func(t *testing.T) {
			root := Get(t, key)
			if err := validate(root, fixtureHashes[key]); err != nil {
				t.Fatal(err)
			}
			if err := validate(filepath.Join(root, "missing"), fixtureHashes[key]); err == nil {
				t.Fatal("missing seed accepted")
			}
			for name := range fixtureHashes[key] {
				p := filepath.Join(root, name)
				data, err := os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(p, append(data, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
				if err = validate(root, fixtureHashes[key]); err == nil {
					t.Fatal("changed seed accepted")
				}
				break
			}
		})
	}
}
func TestWorkspaceIdentityAndCleanup(t *testing.T) {
	parent := Get(t, "carrier")
	if Get(t, "carrier") != parent {
		t.Fatal("same test lost output identity")
	}
	var child string
	t.Run("isolated", func(t *testing.T) {
		child = Get(t, "carrier")
		if child == parent {
			t.Fatal("different tests share output")
		}
		if err := validate(child, fixtureHashes["carrier"]); err != nil {
			t.Fatal(err)
		}
	})
	if _, err := os.Stat(child); !os.IsNotExist(err) {
		t.Fatal("finished child workspace not reclaimed", err)
	}
	if _, err := os.Stat(parent); err != nil {
		t.Fatal("child cleanup retired parent", err)
	}
}
func TestRequiredInstalledPortfolio(t *testing.T) {
	repo, err := Repository()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = portfolio(repo); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TSOX_PACKAGE_PORTFOLIO", filepath.Join(Get(t, "package-output"), "missing/release/portfolio"))
	if _, err = portfolio(repo); err == nil {
		t.Fatal("missing installed portfolio accepted")
	}
}

func TestInstalledClosureRejectsAdditionalInputs(t *testing.T) {
	repo, err := Repository()
	if err != nil {
		t.Fatal(err)
	}
	source, err := portfolio(repo)
	if err != nil {
		t.Fatal(err)
	}
	out := Get(t, "package-output")
	for name := range fixtureHashes["installed-packages"] {
		b, err := os.ReadFile(filepath.Join(source, name))
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(out, name)
		if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(p, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = validateClosure(out, "node_modules", fixtureHashes["installed-packages"]); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(out, "node_modules/extra.d.ts"), []byte("declare global {}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = validateClosure(out, "node_modules", fixtureHashes["installed-packages"]); err == nil {
		t.Fatal("extra checker/package input accepted")
	}
}
func TestOutputDirectoryRejectsSymlinkAndEscape(t *testing.T) {
	repo, err := Repository()
	if err != nil {
		t.Fatal(err)
	}
	if err = buildDirectory(repo, filepath.Join(repo, "release")); err == nil {
		t.Fatal("non-build output accepted")
	}
	out := Get(t, "package-output")
	link := filepath.Join(out, "redirect")
	if err = os.Symlink(filepath.Join(repo, "release"), link); err != nil {
		t.Fatal(err)
	}
	if err = buildDirectory(repo, filepath.Join(link, "source-fixture-output")); err == nil {
		t.Fatal("symlink output accepted")
	}
}
