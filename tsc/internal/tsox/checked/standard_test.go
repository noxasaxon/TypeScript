package checked

import (
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"
)

func TestStandardNodeLibraryIdentityAndOverload(t *testing.T) {
	source := `import {readFile as load} from "node:fs/promises"; export async function entry():Promise<string>{return load("file.txt","utf8");}`
	p, diagnostics := New("entry.ts", map[string]string{"entry.ts": source})
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	if len(p.RuntimeFiles) != 1 || p.RuntimeFiles[0].FileName() == StandardNodeDeclarationPath {
		t.Fatal("compiler declaration entered runtime module order")
	}
	if got, want := StandardLibraryFingerprint(), fmt.Sprintf("%x", sha256.Sum256([]byte(standardNodeDeclarations+"\x00"+standardNodeGlobals))); got != want {
		t.Fatal("library fingerprint does not cover exact embedded bytes")
	}
	for _, bad := range []string{
		`import {readFile} from "node:fs/promises";readFile("file.txt","hex");`,
		`import {readFile} from "node:fs/promises";readFile("file.txt");`,
		`import {readFile} from "node:fs";readFile("file.txt","utf8");`,
	} {
		if _, ds := New("entry.ts", map[string]string{"entry.ts": bad}); len(ds) == 0 {
			t.Fatalf("unsupported standard module or overload accepted: %s", bad)
		}
	}
	if _, ds := New("entry.ts", map[string]string{"entry.ts": source, StandardNodeDeclarationPath: `declare module "node:fs/promises" { export function readFile(path:string,encoding:string):Promise<string>; }`}); len(ds) == 0 {
		t.Fatal("caller replaced compiler-owned intrinsic identity")
	}
	if _, ds := New("entry.ts", map[string]string{"entry.ts": `export const setting:string|undefined=process.env.SETTING;`}); len(ds) != 0 {
		t.Fatal(ds)
	}
	if _, ds := New("entry.ts", map[string]string{"entry.ts": source, StandardNodeGlobalsPath: `declare const process: any;`}); len(ds) == 0 {
		t.Fatal("caller replaced compiler-owned Node globals")
	}
}

func TestStandardConfiguredRuntimeImport(t *testing.T) {
	directory, _ := projectFixture(t, map[string]string{"src/main.ts": `import {readFile as load} from "node:fs/promises";export async function entry():Promise<string>{return load("file.txt","utf8");}`})
	p, ds := ReadProject(filepath.Join(directory, "tsconfig.json"), "src/main.ts")
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	if _, ds = p.Check(); len(ds) != 0 {
		t.Fatal(ds)
	}
}
