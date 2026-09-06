package checked

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReexportsSnapshotRuntimeOrderAndErasure(t *testing.T) {
	directory := t.TempDir()
	sources := map[string]string{
		"main.ts":        `console.log("entry"); export { value as first } from "./left.ts"; export { value as second } from "./right.ts"; export type { Erased } from "./erased.ts"; export { type Inline } from "./inline.ts"; export {} from "./empty.ts";`,
		"left.ts":        `export { value } from "./state.ts";`,
		"right.ts":       `export { value } from "./state.ts";`,
		"state.ts":       `export let value = 1;`,
		"erased.ts":      `console.log("erased"); export interface Erased { n: number; }`,
		"inline.ts":      `console.log("inline"); export interface Inline { n: number; }`,
		"empty.ts":       `console.log("empty");`,
		"unreachable.ts": `not valid TypeScript`,
	}
	for name, source := range sources {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entry, snapshot, diagnostics := ReadSources(filepath.Join(directory, "main.ts"))
	if len(diagnostics) != 0 || len(snapshot) != 7 {
		t.Fatalf("snapshot: %+v %+v", snapshot, diagnostics)
	}
	// Complete type-checking sources are snapshotted even when their runtime
	// dependency is erased. Later edits must not leak into this checked domain.
	for name := range sources {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(`invalid after snapshot`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	program, diagnostics := New(entry, snapshot)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	var order []string
	for _, file := range program.RuntimeFiles {
		order = append(order, filepath.Base(program.Files[file]))
	}
	if want := []string{"state.ts", "left.ts", "right.ts", "inline.ts", "empty.ts", "main.ts"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("runtime order %v, want %v", order, want)
	}
}

func TestReexportSourceFailuresRetainExporterPosition(t *testing.T) {
	directory := t.TempDir()
	entry := filepath.Join(directory, "main.ts")
	barrel := filepath.Join(directory, "barrel.ts")
	if err := os.WriteFile(entry, []byte(`export {} from "./barrel.ts";`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(barrel, []byte("\n  export {} from \"./missing.ts\";"), 0o600); err != nil {
		t.Fatal(err)
	}
	check := func() {
		t.Helper()
		_, _, diagnostics := ReadSources(entry)
		if len(diagnostics) != 1 || filepath.Base(diagnostics[0].SourcePath) != "barrel.ts" || diagnostics[0].Construct != "ModuleSource" || diagnostics[0].Position.Line != 2 || diagnostics[0].Position.Column != 3 {
			t.Fatalf("export diagnostic: %+v", diagnostics)
		}
	}
	check()
	if err := os.Symlink(entry, filepath.Join(directory, "missing.ts")); err != nil {
		t.Fatal(err)
	}
	check()
}
