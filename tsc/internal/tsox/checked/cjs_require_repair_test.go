package checked

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

// The .cjs fixture is an actual runtime implementation root under infer-js.
// No synthetic ambient signature supplies its body or loader identity.
func TestCJSRequireWrapperIdentityNode(t *testing.T) {
	cases := []struct {
		Name, Source, Node string
		Reject             bool
	}{
		{"exact-var", "var require;\nconst target='./hidden.cjs';\nconsole.log(require(target));\n", "41", true},
		{"block-var", "{var require;}\nconsole.log(require('./hidden.cjs'));", "41", true},
		{"false-block-var", "if(false){var require;}\nconsole.log(require('./hidden.cjs'));", "41", true},
		{"for-var", "for(var require;false;){}\nconsole.log(require('./hidden.cjs'));", "41", true},
		{"closure-outer-var", "var require;function read(){return require('./hidden.cjs')}console.log(read());", "41", true},
		{"late-initializer", "console.log(require('./hidden.cjs'));var require=function(){return 42};", "41", true},
		{"conditional-initializer", "if(false){var require=function(){return 42}}console.log(require('./hidden.cjs'));", "41", true},
		{"self-initializer", "var require=require;console.log(require('./hidden.cjs'));", "41", true},
		{"early-source-initializer", "var require=function(){return 42};console.log(require(0));", "42", true},
		{"destructured-var", "var {require}={require:require};console.log(require('./hidden.cjs'));", "41", true},
		{"nested-var", "function run(){var require=function(n){return n+1};return require(41)}console.log(run());", "42", false},
		{"nested-block-var", "function run(){{var require=function(n){return n+1}}return require(41)}console.log(run());", "42", false},
		{"block-let", "{let require=function(n){return n+1};console.log(require(41));}", "42", false},
		{"block-const", "{const require=function(n){return n+1};console.log(require(41));}", "42", false},
		{"parameter", "function run(require){return require(41)}console.log(run(n=>n+1));", "42", false},
		{"top-function", "function require(n){return n+1}console.log(require(41));", "42", false},
		{"top-function-var", "var require;function require(n){return n+1}console.log(require(41));", "42", false},
		{"class-static-var", "class C {static {var require=n=>n+1;console.log(require(41));}}", "42", false},
	}
	records := map[string]any{}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			dir := loaderRepairProject(t, `import './dep.cjs';export {};`)
			packageWrite(t, filepath.Join(dir, "dep.cjs"), test.Source)
			packageWrite(t, filepath.Join(dir, "hidden.cjs"), "module.exports=41;\n")
			node := packageNode(t, dir, `await import('./entry.ts')`)
			if node != test.Node {
				t.Fatal(node, test.Node)
			}
			project, ds := ReadProjectWithOptions(filepath.Join(dir, "tsconfig.json"), "entry.ts", ProjectOptions{DependencyTypesInferJS})
			if test.Reject {
				if project != nil || len(ds) != 1 || ds[0].Construct != "SourceRuntimeDependency" || ds[0].SourcePath != filepath.Join(dir, "dep.cjs") || ds[0].Position.Line < 1 || ds[0].Position.Column < 1 {
					t.Fatalf("expected positioned wrapper boundary: project=%v diagnostics=%+v", project != nil, ds)
				}
				if test.Name == "exact-var" {
					if ds[0].Position.Line != 3 || ds[0].Position.Column != 13 {
						t.Fatal(ds)
					}
					packageWrite(t, filepath.Join(dir, "hidden.cjs"), "module.exports=42;\n")
					if got := packageNode(t, dir, `await import('./entry.ts')`); got != "42" {
						t.Fatal(got)
					}
				}
			} else if project == nil || len(ds) != 0 {
				t.Fatalf("local require rejected: %+v", ds)
			}
			records[test.Name] = map[string]any{"source": test.Source, "node": node, "diagnostics": ds, "reject": test.Reject}
		})
	}
	data, _ := json.MarshalIndent(records, "", "  ")
	if err := os.WriteFile(filepath.Join(sourcefixture.Get(t, "package-output"), "wrapper-node-report.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}
