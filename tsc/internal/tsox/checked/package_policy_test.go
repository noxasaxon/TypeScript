package checked

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/tsoptions"
	"github.com/microsoft/typescript-go/internal/vfs/osvfs"
	"github.com/microsoft/typescript-go/tsox/graph"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func packagePrototypeDir(t *testing.T) string {
	t.Helper()
	base := sourcefixture.Get(t, "package-output")
	if base == "" {
		t.Fatal("explicit scratch artifact directory required")
	}
	p, err := os.MkdirTemp(base, "case-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(p) })
	return p
}
func packageWrite(t *testing.T, p, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}
func packageNode(t *testing.T, dir, code string) string {
	t.Helper()
	cmd := exec.Command("node", "--input-type=module", "-e", code)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Node: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}
func packageConfig(t *testing.T, p string, fs *packageCapture) *tsoptions.ParsedCommandLine {
	t.Helper()
	cfg, ds := tsoptions.GetParsedCommandLineOfConfigFile(filepath.Join(p, "tsconfig.json"), nil, nil, configHost{fs, p}, nil)
	if len(ds) > 0 || len(cfg.GetConfigFileParsingDiagnostics()) > 0 {
		t.Fatal(ds)
	}
	return cfg
}
func TestPackagePrototypeSnapshot(t *testing.T) {
	dir := packagePrototypeDir(t)
	p := filepath.Join(dir, "present.ts")
	missing := filepath.Join(dir, "nearer.json")
	packageWrite(t, p, "before")
	fs := capturePackages(osvfs.FS())
	if !fs.FileExists(p) || fs.FileExists(missing) {
		t.Fatal("existence")
	}
	if _, ok := fs.ReadFile(p); !ok {
		t.Fatal("read")
	}
	fs.DirectoryExists(dir)
	fs.Realpath(p)
	fs.GetAccessibleEntries(dir)
	snap, err := fs.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	if err = snap.Revalidate(osvfs.FS()); err != nil {
		t.Fatal(err)
	}
	packageWrite(t, p, "after")
	packageWrite(t, missing, "new")
	replay := replayPackages(snap)
	if text, _ := replay.ReadFile(p); text != "before" {
		t.Fatal("replay consulted OS")
	}
	if replay.FileExists(missing) {
		t.Fatal("negative observation lost")
	}
	if replay.Fault() != nil {
		t.Fatal(replay.Fault())
	}
	if snap.Revalidate(osvfs.FS()) == nil {
		t.Fatal("changed capture accepted")
	}
	replay.ReadFile(filepath.Join(dir, "unobserved"))
	if replay.Fault() == nil {
		t.Fatal("unobserved replay request became absence")
	}
}
func TestPackagePrototypePolicyAndDepth(t *testing.T) {
	dir := packagePrototypeDir(t)
	packageWrite(t, filepath.Join(dir, "package.json"), `{"type":"module"}`)
	packageWrite(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"strict":true,"module":"NodeNext","allowImportingTsExtensions":true,"noEmit":true},"files":["entry.ts","other.ts"]}`)
	packageWrite(t, filepath.Join(dir, "entry.ts"), `import value from "chain-a";value(42);`)
	packageWrite(t, filepath.Join(dir, "other.ts"), `const retained:string=42;export{};`)
	packageWrite(t, filepath.Join(dir, "node_modules/chain-a/package.json"), `{"main":"index.js","type":"commonjs"}`)
	packageWrite(t, filepath.Join(dir, "node_modules/chain-a/index.js"), `module.exports=require("chain-b");`)
	packageWrite(t, filepath.Join(dir, "node_modules/chain-b/package.json"), `{"main":"index.js","type":"commonjs"}`)
	packageWrite(t, filepath.Join(dir, "node_modules/chain-b/index.js"), "/** @param {string} value */\nmodule.exports=function(value){return value;};")
	fs := capturePackages(osvfs.FS())
	cfg := packageConfig(t, dir, fs)
	if _, _, err := dependencyConfig(cfg, ProjectOptions{"silent"}, 0, nil); err == nil {
		t.Fatal("invalid policy accepted")
	}
	original := *cfg.CompilerOptions()
	plain, err := completeDependencyInference(cfg, ProjectOptions{}, nil, fs)
	if err != nil {
		t.Fatal(err)
	}
	codes := func(v dependencyCheck) []int {
		var codes []int
		for _, d := range v.Diagnostics {
			codes = append(codes, int(d.Code()))
		}
		return codes
	}
	if !slices.Contains(codes(plain), 7016) || !slices.Contains(codes(plain), 2322) {
		t.Fatalf("default diagnostics changed: %v", codes(plain))
	}
	inferred, err := completeDependencyInference(cfg, ProjectOptions{DependencyTypesInferJS}, nil, fs)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(codes(inferred), 2345) || !slices.Contains(codes(inferred), 2322) || slices.Contains(codes(inferred), 7016) {
		t.Fatalf("incomplete/filtered diagnostics: %v", codes(inferred))
	}
	if len(inferred.Policy.ConfiguredRoots) != 2 || inferred.Policy.EffectiveDepth < 2 || inferred.Policy.EffectiveAllowJS != core.TSTrue || cfg.CompilerOptions().AllowJs != original.AllowJs || cfg.CompilerOptions().MaxNodeModuleJsDepth != original.MaxNodeModuleJsDepth {
		t.Fatal("policy lost roots or mutated config")
	}
	snap, err := fs.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	if err = snap.Revalidate(osvfs.FS()); err != nil {
		t.Fatal(err)
	}
	replay := replayPackages(snap)
	again, err := completeDependencyInference(cfg, ProjectOptions{DependencyTypesInferJS}, nil, replay)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(codes(again)) != fmt.Sprint(codes(inferred)) {
		t.Fatal("replay diagnostics changed")
	}
}
func TestPackagePrototypeRuntimeNode(t *testing.T) {
	dir := packagePrototypeDir(t)
	if packageNode(t, dir, "console.log(process.versions.node)") != "24.20.0" {
		t.Fatal("pinned Node required")
	}
	packageWrite(t, filepath.Join(dir, "package.json"), `{"type":"module"}`)
	cases := []struct {
		name, metadata string
		files          map[string]string
		want           string
		unsupported    bool
	}{
		{"adjacent-ts", `{"main":"entry.js","type":"commonjs"}`, map[string]string{"entry.js": "module.exports=1;", "entry.ts": "export default 2;"}, "entry.js", false},
		{"types-version", `{"main":"index.js","type":"commonjs","typesVersions":{"*":{"*":["mapped.js"]}}}`, map[string]string{"index.js": "module.exports=1;", "mapped.js": "module.exports=2;"}, "index.js", false},
		{"sync-condition", `{"type":"module","exports":{"module-sync":"./sync.js","default":"./fallback.js"}}`, map[string]string{"sync.js": "export const value=1;", "fallback.js": "export const value=2;"}, "sync.js", false},
		{"ordered-condition", `{"type":"module","exports":{"default":"./first.js","node":"./second.js"}}`, map[string]string{"first.js": "export const value=1;", "second.js": "export const value=2;"}, "first.js", false},
		{"index-default", `{"type":"commonjs"}`, map[string]string{"index.js": "module.exports=1;"}, "index.js", false},
		{"extension-fallback", `{"main":"lib","type":"commonjs"}`, map[string]string{"lib.js": "module.exports=1;"}, "lib.js", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base := filepath.Join(dir, "node_modules", c.name)
			packageWrite(t, filepath.Join(base, "package.json"), c.metadata)
			for name, s := range c.files {
				packageWrite(t, filepath.Join(base, name), s)
			}
			want := packagePath(filepath.Join(base, c.want))
			for _, mode := range []RuntimeMode{RuntimeImportESM, RuntimeRequire} {
				code := fmt.Sprintf(`import{fileURLToPath}from'node:url';import{createRequire}from'node:module';console.log(%s);`, func() string {
					if mode == RuntimeRequire {
						return fmt.Sprintf("createRequire(import.meta.url).resolve(%q)", c.name)
					}
					return fmt.Sprintf("fileURLToPath(import.meta.resolve(%q))", c.name)
				}())
				if got := packageNode(t, dir, code); got != want {
					t.Fatalf("Node %s: %s != %s", mode, got, want)
				}
				fs := capturePackages(osvfs.FS())
				r := packageResolver{fs}
				result, err := r.ResolveRuntime(RuntimeImport{filepath.Join(dir, "entry.mjs"), c.name, mode, graph.Position{Line: 1, Column: 1}})
				if c.unsupported {
					if err == nil {
						t.Fatal("unsupported selection silently approximated")
					}
					continue
				}
				if err != nil || result.Module != want {
					t.Fatalf("resolver: %+v %v", result, err)
				}
				snap, _ := fs.Freeze()
				replayed, err := (&packageResolver{replayPackages(snap)}).ResolveRuntime(result.Edge)
				if err != nil || replayed.Module != want {
					t.Fatal(replayed, err)
				}
			}
		})
	}
}
func TestPackagePrototypeFrozenChecker(t *testing.T) {
	dir := sourcefixture.Get(t, "portfolio")
	if dir == "" {
		t.Fatal("explicit frozen installed portfolio required")
	}
	fs := capturePackages(osvfs.FS())
	cfg := packageConfig(t, dir, fs)
	var implementations []string
	runtime := map[string]RuntimeResolution{}
	for _, name := range []string{"cookie", "escape-html"} {
		edge := RuntimeImport{filepath.Join(dir, "cookie-route/route.ts"), name, RuntimeImportESM, graph.Position{Line: 1, Column: 1}}
		r, err := (&packageResolver{fs}).ResolveRuntime(edge)
		if err != nil {
			t.Fatal(err)
		}
		runtime[name] = r
		implementations = append(implementations, r.Module)
		want := packageNode(t, dir, fmt.Sprintf(`import{fileURLToPath}from'node:url';console.log(fileURLToPath(import.meta.resolve(%q)));`, name))
		if r.Module != want {
			t.Fatal("frozen Node runtime path", r, want)
		}
	}
	result, err := completeDependencyInference(cfg, ProjectOptions{DependencyTypesInferJS}, implementations, fs)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Diagnostics) != 0 || len(result.Policy.ConfiguredRoots) != 10 {
		t.Fatalf("whole configured strict project: roots%d diagnostics%v", len(result.Policy.ConfiguredRoots), result.Diagnostics)
	}
	cookie := result.Program.ResolveModuleName("cookie", filepath.Join(dir, "cookie-route/route.ts"), core.ModuleKindESNext)
	if !strings.HasSuffix(cookie.ResolvedFileName, ".d.ts") || result.Program.GetSourceFile(runtime["cookie"].Module) == nil {
		t.Fatal("declaration/runtime identities collapsed")
	}
	snap, err := fs.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	if err = snap.Revalidate(osvfs.FS()); err != nil {
		t.Fatal(err)
	}
	again, err := completeDependencyInference(cfg, ProjectOptions{DependencyTypesInferJS}, implementations, replayPackages(snap))
	if err != nil || len(again.Diagnostics) > 0 {
		t.Fatal("immutable replay", err)
	}
	snapshotBytes, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(sourcefixture.Get(t, "package-output"), "frozen-snapshot.json"), snapshotBytes, 0600); err != nil {
		t.Fatal(err)
	}
	runtimeHashes := map[string]string{}
	for name, value := range runtime {
		text, ok := fs.ReadFile(value.Module)
		if !ok {
			t.Fatal(name)
		}
		runtimeHashes[name] = fmt.Sprintf("%x", sha256.Sum256([]byte(text)))
	}
	if runtimeHashes["cookie"] != "b810b6ef93afbced6f907f0d9536afd01528ac93ea95bfc51675214e4ee13fb2" || runtimeHashes["escape-html"] != "42a7f91883d0c5ce9292dda4e017e1f8664d34b09276d89fb6f3859c29d1ca9b" {
		t.Fatal("frozen actual JS inputs changed", runtimeHashes)
	}
	report := map[string]any{"runtimeHashes": runtimeHashes, "snapshotSHA256": fmt.Sprintf("%x", sha256.Sum256(snapshotBytes)), "policy": result.Policy, "runtime": runtime, "checkerEdges": result.Edges, "snapshotObservations": len(snap.Observations), "passes": result.Passes, "semanticDiagnostics": len(result.Program.GetSemanticDiagnostics(context.Background(), nil)), "standardLibraryFingerprint": StandardLibraryFingerprint()}
	data, _ := json.MarshalIndent(report, "", "  ")
	if err = os.WriteFile(filepath.Join(sourcefixture.Get(t, "package-output"), "frozen-report.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPackagePrototypeNegativeObservation(t *testing.T) {
	dir := packagePrototypeDir(t)
	missing := filepath.Join(dir, "node_modules/nearer/package.json")
	fs := capturePackages(osvfs.FS())
	if fs.FileExists(missing) {
		t.Fatal("fixture")
	}
	snap, err := fs.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	packageWrite(t, missing, `{"main":"new.js"}`)
	if snap.Revalidate(osvfs.FS()) == nil {
		t.Fatal("newly appearing negative lookup accepted")
	}
	replay := replayPackages(snap)
	if replay.FileExists(missing) || replay.Fault() != nil {
		t.Fatal("snapshot absence not retained")
	}
	// Caller mutation of a returned observation map cannot mutate the capture.
	delete(snap.Observations, "file\x00"+missing)
	second, _ := fs.Freeze()
	if len(second.Observations) != 1 {
		t.Fatal("snapshot map aliases capture")
	}
}
func TestPackagePrototypeCycleAndDeclarations(t *testing.T) {
	dir := packagePrototypeDir(t)
	packageWrite(t, filepath.Join(dir, "package.json"), `{"type":"module"}`)
	packageWrite(t, filepath.Join(dir, "tsconfig.json"), `{"compilerOptions":{"strict":true,"module":"NodeNext","allowImportingTsExtensions":true,"noEmit":true},"files":["entry.ts","broken.d.ts"]}`)
	packageWrite(t, filepath.Join(dir, "entry.ts"), `import run from "typed";import value from "cycle-a";run(42);value("ok");`)
	packageWrite(t, filepath.Join(dir, "broken.d.ts"), `declare const missing: MissingDeclaredType;`)
	packageWrite(t, filepath.Join(dir, "node_modules/typed/package.json"), `{"main":"index.js","types":"index.d.ts","type":"commonjs"}`)
	packageWrite(t, filepath.Join(dir, "node_modules/typed/index.js"), `module.exports=function(x){return x;};`)
	packageWrite(t, filepath.Join(dir, "node_modules/typed/index.d.ts"), `declare function run(x:string):string;export=run;`)
	for _, name := range []string{"cycle-a", "cycle-b"} {
		packageWrite(t, filepath.Join(dir, "node_modules", name, "package.json"), `{"main":"index.js","type":"commonjs"}`)
	}
	packageWrite(t, filepath.Join(dir, "node_modules/cycle-a/index.js"), "require('cycle-b');\n/** @param {string} value */\nmodule.exports=function(value){return value;};")
	packageWrite(t, filepath.Join(dir, "node_modules/cycle-b/index.js"), `require("cycle-a");module.exports=1;`)
	fs := capturePackages(osvfs.FS())
	cfg := packageConfig(t, dir, fs)
	impl := filepath.Join(dir, "node_modules/typed/index.js")
	result, err := completeDependencyInference(cfg, ProjectOptions{DependencyTypesInferJS}, []string{impl}, fs)
	if err != nil {
		t.Fatal(err)
	}
	codes := []int{}
	for _, d := range result.Diagnostics {
		codes = append(codes, int(d.Code()))
	}
	if !slices.Contains(codes, 2304) || !slices.Contains(codes, 2345) {
		t.Fatalf("declaration/root diagnostics suppressed: %v", codes)
	}
	for _, name := range []string{"cycle-a", "cycle-b"} {
		if result.Program.GetSourceFile(filepath.Join(dir, "node_modules", name, "index.js")) == nil {
			t.Fatal("cycle inference omitted source")
		}
	}
	if !strings.HasSuffix(result.Program.ResolveModuleName("typed", filepath.Join(dir, "entry.ts"), core.ModuleKindESNext).ResolvedFileName, "index.d.ts") || result.Program.GetSourceFile(impl) == nil {
		t.Fatal("implementation root replaced type declaration")
	}
}
