package extract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
	sourcefixture "github.com/microsoft/typescript-go/tsox/testfixture"
)

func TestJSONClosedLayoutUnchangedValidator(t *testing.T) {
	root := filepath.Join("testdata", "json-layout")
	sources := map[string]string{}
	for _, name := range []string{"validation.ts", "model.ts"} {
		source, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		sources[name] = string(source)
		// The fork fixture is an exact copy so standalone fork tests need no
		// parent checkout. In the integrated release, verify the frozen source.
		frozen := filepath.Join("..", "..", "..", "..", "..", "..", "release", "portfolio", "web-api", name)
		if original, err := os.ReadFile(frozen); err == nil && string(original) != string(source) {
			t.Fatalf("%s fixture differs from frozen source", name)
		}
	}
	checkedProgram, diagnostics := checked.New("validation.ts", sources)
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	result := extractChecked("validation.ts", checkedProgram, true)
	if result.Program == nil {
		t.Fatal(result.Diagnostics)
	}
	if len(result.Program.EntryExports) != 1 || result.Program.EntryExports[0].Name != "validateTask" {
		t.Fatal("lost unchanged validator export")
	}
	unions := 0
	for _, shape := range result.Program.Shapes {
		if len(shape.Alternatives) != 0 {
			unions++
			if len(shape.Alternatives) != 2 || shape.Discriminant != "ok" {
				t.Fatal("lost result alternatives")
			}
		}
	}
	if unions != 1 {
		t.Fatalf("got %d closed unions", unions)
	}
	if directory := os.Getenv("TSOX_JSON_FIXTURES"); directory != "" {
		encoded, err := json.MarshalIndent(struct {
			Source  string
			Sources map[string]string
			Program *graph.Program
		}{sources["validation.ts"], sources, result.Program}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(directory, "validate_task.json"), append(encoded, '\n'), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// No public admission until the backend consumes these obligations.
	public := ExtractChecked("validation.ts", checkedProgram)
	if public.Program != nil {
		t.Fatal("private layout work opened public admission")
	}
}

func TestJSONClosedLayoutsKeepGenericInstantiationsDistinct(t *testing.T) {
	source := `interface A { count: number } interface B { name: string }
 type Choice<T> = { tag: "yes"; payload: T } | { tag: "no"; message: string };
 function first(): Choice<A> { return {tag: "yes", payload: {count: 1}}; }
 function second(): Choice<B> { return {tag: "yes", payload: {name: "x"}}; }`
	result := extractSource("generic.ts", source, true)
	if result.Program == nil {
		t.Fatal(result.Diagnostics)
	}
	functions := []*graph.Statement{}
	for _, s := range result.Program.Statements {
		if s.Kind == graph.StatementFunction {
			functions = append(functions, s)
		}
	}
	if len(functions) != 2 || functions[0].ReturnType.Shape == functions[1].ReturnType.Shape {
		t.Fatal("generic alias symbol collapsed distinct storage instantiations")
	}
	for _, fn := range functions {
		value := fn.Body[0].Value
		if value.Kind != graph.ExpressionClosedValue || value.Operand == nil || value.Operand.Kind != graph.ExpressionObject {
			t.Fatal("closed result lost its actual construction")
		}
	}
}

func TestJSONClosedLayoutsRejectUnprovedDiscriminantsAndRecursion(t *testing.T) {
	for _, source := range []string{
		`type Value = {x: number} | {y: string}; function make(): Value { return {x: 1}; }`,
		`interface Value { next?: Value } function make(): Value { return {}; }`,
		`type Value = {ok: true; x: number} | {ok: false; y: string}; function make(flag: true): Value { return {ok: flag, x: 1}; }`,
	} {
		result := extractSource("fenced.ts", source, true)
		if result.Program != nil || len(result.Diagnostics) == 0 {
			t.Fatalf("unproved layout admitted: %s", source)
		}
	}
}

// The Go backend fixtures retain actual source alongside their graph. When the
// fork is tested inside the parent checkout, ensure none drifts from extraction.
// Standalone fork tests still exercise the local source witnesses above.
func TestJSONBackendFixturesMatchCurrentExtraction(t *testing.T) {
	directory := filepath.Join("..", "..", "..", "..", "..", "..", "go", "internal", "evidence", "testdata", "json-values")
	files, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		parent := filepath.Clean(filepath.Join(directory, "..", "..", "..", "..", ".."))
		integrated, parentErr := jsonIntegratedFixtureParent(parent)
		if parentErr != nil {
			t.Fatal(parentErr)
		}
		if integrated {
			t.Fatalf("required parent backend fixture directory is absent: %s", directory)
		}
		t.Skip("parent checkout is absent in standalone fork")
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if filepath.Ext(file.Name()) != ".json" {
			continue
		}
		bytes, err := os.ReadFile(filepath.Join(directory, file.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var fixture struct {
			Source  string
			Sources map[string]string
			Program *graph.Program
		}
		if err = json.Unmarshal(bytes, &fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Source == "" || fixture.Program == nil {
			continue
		}
		t.Run(file.Name(), func(t *testing.T) {
			var result graph.Result
			if len(fixture.Sources) != 0 {
				checkedProgram, diagnostics := checked.New(fixture.Program.SourcePath, fixture.Sources)
				if len(diagnostics) != 0 {
					t.Fatal(diagnostics)
				}
				result = extractChecked(fixture.Program.SourcePath, checkedProgram, true)
			} else {
				result = extractSource(fixture.Program.SourcePath, fixture.Source, true)
			}
			if result.Program == nil {
				t.Fatal(result.Diagnostics)
			}
			if !reflect.DeepEqual(result.Program, fixture.Program) {
				t.Fatal("backend graph fixture differs from current extraction of its actual source")
			}
		})
	}
}

// Distinguish a missing required fixture from a genuinely absent parent checkout.
// The exact expected outer parent is checked, not an arbitrary ancestor search.
func jsonIntegratedFixtureParent(parent string) (bool, error) {
	present := 0
	for _, name := range []string{"go.work", "CONTEXT.md"} {
		info, err := os.Stat(filepath.Join(parent, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if info.Mode().IsRegular() {
			present++
		}
	}
	return present > 0, nil
}
func TestJSONFixtureParentAbsenceContract(t *testing.T) {
	root := sourcefixture.Get(t, "project-output")
	present, err := jsonIntegratedFixtureParent(root)
	if err != nil || present {
		t.Fatal("absent parent misclassified", present, err)
	}
	for _, name := range []string{"go.work", "CONTEXT.md"} {
		if err = os.WriteFile(filepath.Join(root, name), []byte("parent marker"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// No fixture directory exists; parent presence must prevent the standalone skip.
	present, err = jsonIntegratedFixtureParent(root)
	if err != nil || !present {
		t.Fatal("missing integrated fixture became standalone", present, err)
	}
	if _, err = os.Stat(filepath.Join(root, "go/internal/evidence/testdata/json-values")); !os.IsNotExist(err) {
		t.Fatal("test unexpectedly has fixture", err)
	}
}
