package checked

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSourcesSnapshotsOnlyExplicitDependenciesAndNormalizesEntry(t *testing.T) {
	directory := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		target := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("app/main.ts", `import { value as first } from "../state.ts"; import { value as second } from ".././state.ts"; console.log(first + second);`)
	write("state.ts", `export const value = 4;`)
	write("unreachable.ts", `this file must never be read`)
	entry, sources, diagnostics := ReadSources(filepath.Join(directory, "app", "main.ts"))
	if len(diagnostics) != 0 || len(sources) != 2 {
		t.Fatalf("snapshot: %s %+v %+v", entry, sources, diagnostics)
	}
	canonical, err := filepath.EvalSymlinks(filepath.Join(directory, "app", "main.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if entry != filepath.ToSlash(canonical) {
		t.Fatalf("entry %q != %q", entry, canonical)
	}
	program, diagnostics := New(entry, sources)
	if len(diagnostics) != 0 || len(program.RuntimeFiles) != 2 {
		t.Fatalf("normalized dependency identity: %+v %+v", program, diagnostics)
	}
	// Mutation overlays must not consult the original file after snapshotting.
	write("state.ts", `export const value: number = "invalid after snapshot";`)
	if _, diagnostics := New(entry, sources); len(diagnostics) != 0 {
		t.Fatalf("snapshot consulted filesystem: %+v", diagnostics)
	}
}

func TestReadSourcesDiagnosesMissingAndSymlinkImportsAtImporter(t *testing.T) {
	directory := t.TempDir()
	main := filepath.Join(directory, "main.ts")
	if err := os.WriteFile(main, []byte("\nimport \"./missing.ts\";"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, diagnostics := ReadSources(main)
	if len(diagnostics) != 1 || filepath.Base(diagnostics[0].SourcePath) != "main.ts" || diagnostics[0].Position.Line != 2 {
		t.Fatalf("missing import provenance: %+v", diagnostics)
	}
	target := filepath.Join(directory, "target.ts")
	if err := os.WriteFile(target, []byte("console.log(1);"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(directory, "missing.ts")); err != nil {
		t.Fatal(err)
	}
	_, _, diagnostics = ReadSources(main)
	if len(diagnostics) != 1 || filepath.Base(diagnostics[0].SourcePath) != "main.ts" || diagnostics[0].Position.Line != 2 {
		t.Fatalf("symlink import provenance: %+v", diagnostics)
	}
}
