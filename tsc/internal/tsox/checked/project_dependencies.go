package checked

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/microsoft/typescript-go/tsox/graph"
)

// ReadProjectWithOptions separates whole-project checking from the executable
// entry closure. The final compiler Program uses the validated snapshot replay.
func ReadProjectWithOptions(configPath, entry string, options ProjectOptions) (*Project, []graph.Diagnostic) {
	loaded, err := captureDependencyProject(configPath, entry, options)
	fail := func(err error) (*Project, []graph.Diagnostic) {
		var positioned *projectDiagnosticError
		if errors.As(err, &positioned) {
			return nil, []graph.Diagnostic{positioned.Diagnostic}
		}
		return nil, []graph.Diagnostic{projectError(configPath, "ProjectResolution", err.Error())}
	}
	if err != nil {
		return fail(err)
	}
	if len(loaded.Diagnostics) != 0 {
		return nil, projectDiagnostics(configPath, loaded.Diagnostics)
	}
	sources := map[string]string{}
	for _, observed := range loaded.Snapshot.Observations {
		if observed.Kind != "read" {
			continue
		}
		var value struct {
			Text  string
			Found bool
		}
		if err := json.Unmarshal([]byte(observed.Value), &value); err != nil {
			return fail(err)
		}
		if !value.Found {
			continue
		}
		name := packagePath(observed.Path)
		if prior, exists := sources[name]; exists && prior != value.Text {
			return fail(fmt.Errorf("conflicting source identity %s", name))
		}
		sources[name] = value.Text
	}
	snapshotBytes, err := json.Marshal(loaded.Snapshot)
	if err != nil {
		return fail(err)
	}
	digest := sha256.Sum256(snapshotBytes)
	metadata, err := json.MarshalIndent(struct {
		Version        int                    `json:"version"`
		Resolution     packageProjectManifest `json:"resolution"`
		SnapshotSHA256 string                 `json:"snapshotSHA256"`
	}{1, loaded.Manifest, hex.EncodeToString(digest[:])}, "", "  ")
	if err != nil {
		return fail(err)
	}
	syntax, err := captureSourceProgramSyntax(loaded.Program)
	if err != nil {
		return fail(err)
	}
	return &Project{Entry: loaded.Manifest.Entry, ConfigPath: loaded.Config.ConfigName(), sources: sources,
		config: loaded.Config, dependency: loaded, sourceSyntax: syntax, resolution: append(metadata, '\n')}, nil
}

func (p *Project) ResolutionManifest() []byte {
	if p == nil {
		return nil
	}
	return slices.Clone(p.resolution)
}

func (p *Project) checkDependencyProject() (*Program, []graph.Diagnostic) {
	labels := map[string]string{}
	for name := range p.sources {
		labels[Normalize(name)] = name
	}
	program, ds := runtimeProgram(p.dependency.Program, p.Entry, labels)
	if len(ds) != 0 {
		return nil, ds
	}
	// Existing relative-ESM extraction is admitted only when its actual module
	// identities/order agree with the independently resolved runtime closure.
	// Package/CJS implementation binding remains a separate extraction seam.
	modules := map[string]packageModule{}
	for _, module := range p.dependency.Manifest.Modules {
		modules[module.ID] = module
	}
	for _, file := range program.RuntimeFiles {
		name := file.FileName()
		module, ok := modules[name]
		if !ok || module.Format != "module-typescript" || filepath.Ext(name) != ".ts" {
			return nil, []graph.Diagnostic{projectError(name, "ModuleImplementation", "runtime module binding is not implemented for this source format")}
		}
		imports, d := moduleImports(file, name)
		if d != nil {
			return nil, []graph.Diagnostic{*d}
		}
		index := 0
		for _, dependency := range imports {
			if dependency.typeOnly {
				continue
			}
			if index >= len(module.Dependencies) {
				return nil, []graph.Diagnostic{projectError(name, "ModuleImplementation", "runtime edge is absent from captured resolution")}
			}
			edge := module.Dependencies[index]
			index++
			target := packagePath(filepath.Join(filepath.Dir(name), dependency.name))
			if StandardModule(dependency.name) {
				target = dependency.name
			}
			if edge.Edge.Specifier != dependency.name || edge.Module != target {
				return nil, []graph.Diagnostic{*diagnostic(file, name, dependency.node, "ModuleImplementation", "runtime dependency identity/order differs from captured resolution")}
			}
		}
		if index != len(module.Dependencies) {
			return nil, []graph.Diagnostic{projectError(name, "ModuleImplementation", "captured runtime edge is absent from executable graph")}
		}
		delete(modules, name)
	}
	if len(modules) != 0 {
		return nil, []graph.Diagnostic{projectError(p.Entry, "ModuleImplementation", "executable module graph differs from captured runtime resolution")}
	}
	if d := validateRuntimeNames(program); d != nil {
		return nil, []graph.Diagnostic{*d}
	}
	return program, nil
}
