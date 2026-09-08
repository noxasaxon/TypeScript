package extract

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
)

func TestStandardProducerUnchangedValidator(t *testing.T) {
	for _, across := range []bool{false, true} {
		name := "body_validator"
		middle := ""
		if across {
			name = "body_validator_suspend"
			middle = `const alias=raw; const text=await readFile("input.txt","utf8"); console.log(text);`
		}
		t.Run(name, func(t *testing.T) {
			sources := map[string]string{}
			for _, file := range []string{"validation.ts", "model.ts"} {
				data, err := os.ReadFile(filepath.Join("testdata", "json-layout", file))
				if err != nil {
					t.Fatal(err)
				}
				sources[file] = string(data)
			}
			argument := "raw"
			if across {
				argument = "alias"
			}
			sources["consumer.ts"] = `import {validateTask} from "./validation.ts";
import {readFile} from "node:fs/promises";
export async function handle(request:Request):Promise<Response>{
const raw:unknown=await request.json(); ` + middle + `
const result=validateTask(` + argument + `); if(result.ok)return new Response("ok"); return new Response("bad");}`
			result := ExtractAsyncFiles("consumer.ts", sources, "handle", "")
			if result.Program == nil {
				t.Fatal(result.Diagnostics)
			}
			p := result.Program
			fixture := filepath.Join("..", "..", "..", "..", "..", "..", "go", "internal", "evidence", "testdata", "async-json", name+".json")
			// This is the maintained outer-workspace provenance gate. Missing
			// serialized evidence must fail just like a mismatching graph.
			if err := compareStandardProducerFixture(fixture, sources, p); err != nil {
				t.Fatal(err)
			}
			if p.Platform != graph.PlatformStandard || p.Stages[0].Await.Producer == nil || p.Stages[0].Await.Producer.Kind != graph.ProducerBodyJSON {
				t.Fatal("missing actual body producer")
			}
			if across && (len(p.Stages) != 2 || p.Stages[1].Await.Producer.Kind != graph.ProducerFileUTF8) {
				t.Fatal("missing actual file producer")
			}

		})
	}
}

func TestStandardConfiguredAugmentationIsNotIntrinsic(t *testing.T) {
	directory := t.TempDir()
	files := map[string]string{
		"package.json":      `{"type":"module"}`,
		"tsconfig.json":     `{"compilerOptions":{"strict":true,"module":"NodeNext","noEmit":true,"allowImportingTsExtensions":true},"files":["entry.ts","augmentation.d.ts"]}`,
		"entry.ts":          `export async function handle(request:Request):Promise<Response>{const text=await request.text();return new Response(text);}`,
		"augmentation.d.ts": `interface Request { applicationOnly: string; }`,
	}
	for name, value := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	project, ds := checked.ReadProject(filepath.Join(directory, "tsconfig.json"), "entry.ts")
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	program, ds := project.Check()
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for file := range program.Files {
		if filepath.Base(file.FileName()) == "augmentation.d.ts" {
			t.Fatal("witness must cover a configured root outside runtime traversal")
		}
	}
	result := ExtractAsyncChecked(project.Entry, program, "handle", "")
	if result.Program != nil || len(result.Diagnostics) != 1 {
		t.Fatal("configured global augmentation acquired intrinsic Request identity", result.Diagnostics)
	}
}

// Keep the mandatory file read and equality check behind the same test seam so
// the missing-file regression exercises the path used by the maintained gate.
func compareStandardProducerFixture(path string, sources map[string]string, program *graph.AsyncProgram) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("required serialized producer fixture: %w", err)
	}
	var saved struct {
		Sources map[string]string
		Program *graph.AsyncProgram
	}
	if err = json.Unmarshal(data, &saved); err != nil {
		return err
	}
	// JSON round-tripping normalizes omitted empty slices consistently.
	currentBytes, err := json.Marshal(struct {
		Sources map[string]string
		Program *graph.AsyncProgram
	}{sources, program})
	if err != nil {
		return err
	}
	priorBytes, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	var current, prior any
	if err = json.Unmarshal(currentBytes, &current); err != nil {
		return err
	}
	if err = json.Unmarshal(priorBytes, &prior); err != nil {
		return err
	}
	if !reflect.DeepEqual(current, prior) {
		return fmt.Errorf("serialized producer fixture differs from actual extraction")
	}
	return nil
}

func TestStandardProducerMissingFixtureFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	if err := compareStandardProducerFixture(path, map[string]string{}, &graph.AsyncProgram{}); err == nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing required producer fixture accepted: %v", err)
	}
}
