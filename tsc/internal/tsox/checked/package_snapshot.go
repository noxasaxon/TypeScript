package checked

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/microsoft/typescript-go/internal/vfs"
)

type packageObservation struct{ Kind, Path, Value string }
type packageSnapshot struct {
	CaseSensitive bool
	Observations  map[string]packageObservation
}

// packageCapture records absence as well as bytes. A replay miss is a fault,
// never authority to choose a new resolution fallback or consult the OS.
type packageCapture struct {
	base     vfs.FS
	snapshot packageSnapshot
	replay   bool
	fault    error
	mu       sync.Mutex
}

func capturePackages(base vfs.FS) *packageCapture {
	return &packageCapture{base: base, snapshot: packageSnapshot{CaseSensitive: base.UseCaseSensitiveFileNames(), Observations: map[string]packageObservation{}}}
}
func replayPackages(s packageSnapshot) *packageCapture {
	return &packageCapture{snapshot: packageSnapshot{CaseSensitive: s.CaseSensitive, Observations: maps.Clone(s.Observations)}, replay: true}
}
func packagePath(p string) string { return filepath.ToSlash(filepath.Clean(p)) }
func (f *packageCapture) observe(kind, p string, read func() any, into any) {
	// Preserve the exact queried spelling; lexical cleaning can change a path
	// through a symlink and cannot merge two filesystem observations.
	p = filepath.ToSlash(p)
	key := kind + "\x00" + p
	f.mu.Lock()
	defer f.mu.Unlock()
	observation, ok := f.snapshot.Observations[key]
	if !ok {
		if f.replay {
			if f.fault == nil {
				f.fault = fmt.Errorf("PackageSnapshotIncomplete: %s %s", kind, p)
			}
			return
		}
		value, err := json.Marshal(read())
		if err != nil {
			panic(err)
		}
		observation = packageObservation{kind, p, string(value)}
		f.snapshot.Observations[key] = observation
	}
	if err := json.Unmarshal([]byte(observation.Value), into); err != nil {
		panic(err)
	}
}
func (f *packageCapture) UseCaseSensitiveFileNames() bool { return f.snapshot.CaseSensitive }
func (f *packageCapture) FileExists(p string) (v bool) {
	f.observe("file", p, func() any { return f.base.FileExists(p) }, &v)
	return
}
func (f *packageCapture) DirectoryExists(p string) (v bool) {
	f.observe("directory", p, func() any { return f.base.DirectoryExists(p) }, &v)
	return
}
func (f *packageCapture) ReadFile(p string) (string, bool) {
	var v struct {
		Text  string
		Found bool
	}
	f.observe("read", p, func() any {
		t, ok := f.base.ReadFile(p)
		return struct {
			Text  string
			Found bool
		}{t, ok}
	}, &v)
	return v.Text, v.Found
}
func (f *packageCapture) Realpath(p string) (v string) {
	f.observe("realpath", p, func() any { return f.base.Realpath(p) }, &v)
	return
}
func sortedPackageEntries(v vfs.Entries) vfs.Entries {
	v.Files = slices.Clone(v.Files)
	v.Directories = slices.Clone(v.Directories)
	slices.Sort(v.Files)
	slices.Sort(v.Directories)
	v.Symlinks = maps.Clone(v.Symlinks)
	return v
}
func (f *packageCapture) GetAccessibleEntries(p string) (v vfs.Entries) {
	f.observe("entries", p, func() any { return sortedPackageEntries(f.base.GetAccessibleEntries(p)) }, &v)
	return
}
func (f *packageCapture) unsupported(operation string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	err := fmt.Errorf("PackageSnapshotIncomplete: unsupported filesystem operation %s", operation)
	if f.fault == nil {
		f.fault = err
	}
	return err
}
func (f *packageCapture) WriteFile(string, string) error             { return f.unsupported("WriteFile") }
func (f *packageCapture) AppendFile(string, string) error            { return f.unsupported("AppendFile") }
func (f *packageCapture) Remove(string) error                        { return f.unsupported("Remove") }
func (f *packageCapture) Chtimes(string, time.Time, time.Time) error { return f.unsupported("Chtimes") }
func (f *packageCapture) Stat(string) vfs.FileInfo                   { f.unsupported("Stat"); return nil }
func (f *packageCapture) WalkDir(string, vfs.WalkDirFunc) error      { return f.unsupported("WalkDir") }
func (f *packageCapture) Fault() error                               { f.mu.Lock(); defer f.mu.Unlock(); return f.fault }
func (f *packageCapture) Freeze() (packageSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return packageSnapshot{CaseSensitive: f.snapshot.CaseSensitive, Observations: maps.Clone(f.snapshot.Observations)}, f.fault
}
func (s packageSnapshot) Revalidate(base vfs.FS) error {
	if base.UseCaseSensitiveFileNames() != s.CaseSensitive {
		return fmt.Errorf("PackageSnapshotChanged: filesystem case policy")
	}
	check := capturePackages(base)
	keys := slices.Sorted(maps.Keys(s.Observations))
	for _, key := range keys {
		a := s.Observations[key]
		switch a.Kind {
		case "file":
			check.FileExists(a.Path)
		case "directory":
			check.DirectoryExists(a.Path)
		case "read":
			check.ReadFile(a.Path)
		case "realpath":
			check.Realpath(a.Path)
		case "entries":
			check.GetAccessibleEntries(a.Path)
		default:
			return fmt.Errorf("PackageSnapshotIncomplete: observation %s", a.Kind)
		}
		if check.snapshot.Observations[key].Value != a.Value {
			return fmt.Errorf("PackageSnapshotChanged: %s %s", a.Kind, a.Path)
		}
	}
	return check.Fault()
}
