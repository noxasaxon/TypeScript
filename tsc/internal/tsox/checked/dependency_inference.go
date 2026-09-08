package checked

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/bundled"
	"github.com/microsoft/typescript-go/internal/compiler"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/module"
	"github.com/microsoft/typescript-go/internal/tsoptions"
	"github.com/microsoft/typescript-go/internal/tspath"
	"github.com/microsoft/typescript-go/internal/vfs"
)

type packageStandardFS struct{ vfs.FS }

func (f packageStandardFS) ReadFile(p string) (string, bool) {
	switch p {
	case StandardNodeDeclarationPath:
		return standardNodeDeclarations, true
	case StandardNodeGlobalsPath:
		return standardNodeGlobals, true
	}
	return f.FS.ReadFile(p)
}
func (f packageStandardFS) FileExists(p string) bool {
	if p == StandardNodeDeclarationPath || p == StandardNodeGlobalsPath {
		return true
	}
	return f.FS.FileExists(p)
}
func (f packageStandardFS) DirectoryExists(p string) bool {
	if p == "/__tsox_lib__" || p == filepath.Dir(StandardNodeDeclarationPath) {
		return true
	}
	return f.FS.DirectoryExists(p)
}

type dependencyEdge struct {
	Importer, Specifier, Target string
	Mode                        core.ResolutionMode
}
type dependencyCheck struct {
	Program     *compiler.Program
	Policy      dependencyPolicyRecord
	Edges       []dependencyEdge
	Diagnostics []*ast.Diagnostic
	Passes      int
}

func completeDependencyInference(config *tsoptions.ParsedCommandLine, policy ProjectOptions, implementations []string, fs *packageCapture) (dependencyCheck, error) {
	depth := 0
	previous := ""
	seen := map[string]bool{}
	for pass := 1; ; pass++ {
		effective, record, err := dependencyConfig(config, policy, depth, implementations)
		if err != nil {
			return dependencyCheck{}, err
		}
		host := compiler.NewCompilerHost(config.GetCurrentDirectory(), bundled.WrapFS(packageStandardFS{fs}), bundled.LibPath(), nil, nil)
		program := compiler.NewProgram(compiler.ProgramOptions{Config: standardConfig(effective), Host: host, SingleThreaded: core.TSTrue})
		program.BindSourceFiles()
		var edges []dependencyEdge
		missing := map[string]bool{}
		program.ForEachResolvedModule(func(resolved *module.ResolvedModule, name string, mode core.ResolutionMode, importer tspath.Path) {
			target := ""
			if resolved != nil {
				target = resolved.ResolvedFileName
			}
			importerName := string(importer)
			if file := program.GetSourceFileByPath(importer); file != nil {
				importerName = file.FileName()
			}
			edges = append(edges, dependencyEdge{importerName, name, target, mode})
			if target != "" && (strings.HasSuffix(target, ".js") || strings.HasSuffix(target, ".cjs") || strings.HasSuffix(target, ".mjs")) {
				seen[target] = true
				if program.GetSourceFile(target) == nil {
					missing[target] = true
				}
			}
		}, nil)
		slices.SortFunc(edges, func(a, b dependencyEdge) int { return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)) })
		if err := fs.Fault(); err != nil {
			return dependencyCheck{}, err
		}
		if record.Policy == DependencyTypesInferJS && len(missing) > 0 {
			omitted := strings.Join(slices.Sorted(maps.Keys(missing)), "\x00")
			if omitted == previous || record.EffectiveDepth >= record.ConfiguredDepth+len(seen)+1 {
				return dependencyCheck{}, fmt.Errorf("DependencyInferenceIncomplete: resolved JavaScript is absent after depth %d: %s", record.EffectiveDepth, omitted)
			}
			previous = omitted
			depth = record.EffectiveDepth + 1
			continue
		}
		// Diagnostics are taken only from the completed program. No intermediate
		// zero-diagnostic result is accepted as evidence of inference completeness.
		ds := slices.Clone(program.GetProgramDiagnostics())
		ds = append(ds, program.GetGlobalDiagnostics(context.Background())...)
		ds = append(ds, program.GetSyntacticDiagnostics(context.Background(), nil)...)
		ds = append(ds, program.GetSemanticDiagnostics(context.Background(), nil)...)
		if err := fs.Fault(); err != nil {
			return dependencyCheck{}, err
		}
		return dependencyCheck{program, record, edges, ds, pass}, nil
	}
}
