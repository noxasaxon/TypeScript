// Package testfixture supplies required frozen inputs and isolated build output
// for integrated source-authority tests. It issues no compiler evidence.
package testfixture

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var fixtureHashes = func() map[string]map[string]string {
	var m map[string]map[string]string
	if err := json.Unmarshal([]byte(manifestJSON), &m); err != nil {
		panic(err)
	}
	return m
}()
var workspaces sync.Map // key is the actual testing.TB, never a reusable test name.

type workspace struct {
	mu          sync.Mutex
	directories map[string]string
}

func Repository() (string, error) {
	p, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(p, "go.work")); err == nil {
			if _, err := os.Stat(filepath.Join(p, "CONTEXT.md")); err == nil {
				return p, nil
			}
		}
		next := filepath.Dir(p)
		if next == p {
			return "", fmt.Errorf("integrated source fixtures require the outer repository")
		}
		p = next
	}
}
func validate(root string, files map[string]string) error {
	for name, want := range files {
		if filepath.IsAbs(name) || strings.HasPrefix(filepath.Clean(name), "..") {
			return fmt.Errorf("invalid fixture path %s", name)
		}
		p := filepath.Join(root, filepath.FromSlash(name))
		info, err := os.Lstat(p)
		if err != nil {
			return fmt.Errorf("required source fixture %s: %w", p, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("required source fixture is not regular: %s", p)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
			return fmt.Errorf("required source fixture changed: %s (%s)", p, got)
		}
	}
	return nil
}
func portfolio(repo string) (string, error) {
	p := os.Getenv("TSOX_PACKAGE_PORTFOLIO")
	if p == "" {
		p = filepath.Join(repo, "build/portfolio/consumer/release/portfolio")
	}
	// A selected root must be the actual release/portfolio tree. A sibling
	// cannot attest this root's source bytes.
	p, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if filepath.Base(p) != "portfolio" || filepath.Base(filepath.Dir(p)) != "release" {
		return "", fmt.Errorf("selected portfolio must have canonical release/portfolio layout: %s", p)
	}
	consumer := filepath.Clean(filepath.Join(p, "../.."))
	for _, root := range []string{repo, consumer} {
		if err := validateTree(filepath.Join(root, "release/portfolio"), fixtureHashes["portfolio-tree"], "node_modules"); err != nil {
			return "", err
		}
		if err := validateTree(filepath.Join(root, "go/difftest/testdata/accepted/backend-modules"), fixtureHashes["backend-tree"], ""); err != nil {
			return "", err
		}
	}
	if err := validateClosure(p, "node_modules", fixtureHashes["installed-packages"]); err != nil {
		return "", fmt.Errorf("required installed packages: run mise run native:package-test-fixture: %w", err)
	}
	return p, nil
}
func sourceRoot(repo, key string) (string, error) {
	if key == "promise" {
		p := os.Getenv("TSOX_PROMISE_SOURCE")
		if p == "" {
			p = filepath.Join(repo, "release/checkpoint/fixtures/promise-file-all")
		}
		return p, validateTree(p, fixtureHashes[key], "")
	}
	root := os.Getenv("TSOX_SOURCE_FIXTURES")
	if root == "" {
		root = filepath.Join(repo, "release/checkpoint/fixtures/source-authority")
	}
	p := filepath.Join(root, key)
	return p, validateTree(p, fixtureHashes[key], "")
}

// Get returns immutable prepared source for portfolio/promise; every other key
// gets one private per-test workspace. Mixed input/output keys copy verified
// seeds before any test can mutate them, removing cross-package test ordering.
func Get(t testing.TB, key string) string {
	t.Helper()
	repo, err := Repository()
	if err != nil {
		t.Fatal(err)
	}
	if key == "portfolio" {
		p, err := portfolio(repo)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if key == "promise" {
		p, err := sourceRoot(repo, key)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	switch key {
	case "optional-flow", "optional-consumer", "carrier", "package-output", "project-output", "middleware-output", "cjs-body-output", "cjs-domain-output", "snapshot-output", "source-flow-output", "registry-output":
	default:
		t.Fatalf("unknown source fixture role %q", key)
	}
	fresh := &workspace{directories: map[string]string{}}
	value, loaded := workspaces.LoadOrStore(t, fresh)
	w := value.(*workspace)
	if !loaded {
		t.Cleanup(func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			for _, p := range w.directories {
				if err := os.RemoveAll(p); err != nil {
					t.Error(err)
				}
			}
			workspaces.Delete(t)
		})
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if p := w.directories[key]; p != "" {
		return p
	}
	parent := filepath.Join(repo, "build/source-authority-tests")
	if err = buildDirectory(repo, parent); err != nil {
		t.Fatal(err)
	}
	out, err := os.MkdirTemp(parent, key+"-")
	if err != nil {
		t.Fatal(err)
	}
	w.directories[key] = out
	if files, ok := fixtureHashes[key]; ok {
		source, err := sourceRoot(repo, key)
		if err != nil {
			t.Fatal(err)
		}
		for name := range files {
			p := filepath.Join(out, filepath.FromSlash(name))
			if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(p, b, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return out
}

func validateClosure(root, subtree string, files map[string]string) error {
	if err := validate(root, files); err != nil {
		return err
	}
	count := 0
	err := filepath.WalkDir(filepath.Join(root, subtree), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		if _, ok := files[filepath.ToSlash(relative)]; !ok {
			return fmt.Errorf("unexpected installed package input: %s", p)
		}
		count++
		return nil
	})
	if err != nil {
		return err
	}
	if count != len(files) {
		return fmt.Errorf("installed package closure incomplete")
	}
	return nil
}
func buildDirectory(repo, target string) error {
	relative, err := filepath.Rel(repo, target)
	if err != nil {
		return err
	}
	if relative != "build" && !strings.HasPrefix(relative, "build"+string(filepath.Separator)) {
		return fmt.Errorf("output is outside build")
	}
	current := repo
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if err = os.Mkdir(current, 0700); err != nil && !os.IsExist(err) {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("output directory is not a real build directory: %s", current)
		}
	}
	return nil
}

// validateTree attests the complete intended subtree, including directories.
// The sole portfolio exclusion is its separately checked installed package tree.
func validateTree(root string, files map[string]string, excluded string) error {
	if err := validate(root, files); err != nil {
		return err
	}
	directories := map[string]bool{".": true}
	for name := range files {
		for dir := filepath.Dir(filepath.FromSlash(name)); dir != "."; dir = filepath.Dir(dir) {
			directories[filepath.ToSlash(dir)] = true
		}
	}
	count := 0
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink in required source inventory: %s", p)
		}
		if d.IsDir() {
			if excluded != "" && relative == excluded {
				return filepath.SkipDir
			}
			if !directories[relative] {
				return fmt.Errorf("extra source directory: %s", p)
			}
			return nil
		}
		if _, ok := files[relative]; !ok {
			return fmt.Errorf("extra required source inventory file: %s", p)
		}
		count++
		return nil
	})
	if err != nil {
		return err
	}
	if count != len(files) {
		return fmt.Errorf("required source inventory incomplete: %s", root)
	}
	return nil
}
