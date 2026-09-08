package checked

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/compiler"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/parser"
	"github.com/microsoft/typescript-go/internal/tsoptions"
	"github.com/microsoft/typescript-go/internal/tspath"
	"github.com/microsoft/typescript-go/internal/vfs/osvfs"
	"github.com/microsoft/typescript-go/tsox/graph"
)

type packageModule struct {
	ID, Format   string
	Dependencies []RuntimeResolution
}
type packageProjectManifest struct {
	Entry                      string
	Policy                     dependencyPolicyRecord
	Modules                    []packageModule
	CheckerEdges               []dependencyEdge
	StandardLibraryFingerprint string
}
type dependencyProject struct {
	Config      *tsoptions.ParsedCommandLine
	Snapshot    packageSnapshot
	Manifest    packageProjectManifest
	Program     *compiler.Program
	Diagnostics []*ast.Diagnostic
}

func sourceRuntimeImports(file *ast.SourceFile) ([]RuntimeImport, error) {
	var edges []RuntimeImport
	unsupportedAt := func(node *ast.Node, reason string) error {
		return &projectDiagnosticError{Diagnostic: *diagnostic(file, file.FileName(), node, "SourceRuntimeDependency", reason)}
	}
	edge := func(node *ast.Node) {
		position := diagnostic(file, file.FileName(), node, "", "").Position
		position.SourcePath = file.FileName()
		edges = append(edges, RuntimeImport{file.FileName(), node.Text(), RuntimeImportESM, position})
	}
	for _, statement := range file.Statements.Nodes {
		switch statement.Kind {
		case ast.KindImportDeclaration:
			data := statement.AsImportDeclaration()
			if data.Attributes != nil {
				return nil, unsupportedAt(statement, "import attributes need an explicit runtime contract")
			}
			if data.ImportClause != nil {
				phase := data.ImportClause.AsImportClause().PhaseModifier
				if phase != ast.KindUnknown && phase != ast.KindTypeKeyword {
					return nil, unsupportedAt(statement, "import phase is not implemented")
				}
			}
			if data.ImportClause == nil || data.ImportClause.AsImportClause().PhaseModifier != ast.KindTypeKeyword {
				edge(data.ModuleSpecifier)
			}
		case ast.KindExportDeclaration:
			data := statement.AsExportDeclaration()
			if data.Attributes != nil {
				return nil, unsupportedAt(statement, "export attributes need an explicit runtime contract")
			}
			if data.ModuleSpecifier != nil && !data.IsTypeOnly {
				edge(data.ModuleSpecifier)
			}
		}
	}
	return edges, nil
}
func closeRuntimeModules(entry string, fs *packageCapture) ([]packageModule, []string, error) {
	resolver := packageResolver{fs}
	states := map[string]bool{}
	var modules []packageModule
	var implementations []string
	var visit func(string, string) error
	visit = func(name, format string) error {
		if states[name] {
			return nil
		}
		states[name] = true
		if format == "builtin" {
			return nil
		}
		source, ok := fs.ReadFile(name)
		if !ok {
			return fmt.Errorf("missing captured module %s", name)
		}
		kind := core.ScriptKindJS
		if strings.HasSuffix(name, ".ts") {
			kind = core.ScriptKindTS
		} else {
			implementations = append(implementations, name)
		}
		file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: name, Path: tspath.Path(name)}, source, kind)
		if len(file.Diagnostics()) > 0 {
			return &projectDiagnosticError{Diagnostic: TypeScriptDiagnostic(file, name, file.Diagnostics()[0])}
		}
		edges, err := sourceRuntimeImports(file)
		if err != nil {
			return err
		}
		index := len(modules)
		modules = append(modules, packageModule{ID: name, Format: format})
		for _, edge := range edges {
			resolution, err := resolver.ResolveRuntime(edge)
			if err != nil {
				return err
			}
			modules[index].Dependencies = append(modules[index].Dependencies, resolution)
			if err = visit(resolution.Module, resolution.Format); err != nil {
				return err
			}
		}
		return nil
	}
	root, err := resolver.finishRuntime(RuntimeImport{Importer: entry, Specifier: entry, Mode: RuntimeImportESM, Position: graph.Position{Line: 1, Column: 1}}, entry, "entry")
	if err != nil {
		return nil, nil, err
	}
	if err = visit(root.Module, root.Format); err != nil {
		return nil, nil, err
	}
	slices.Sort(implementations)
	return modules, implementations, fs.Fault()
}

// captureDependencyProject is the reusable configured-entry foundation. Public
// CLI wiring and executable module binding/initialization are deliberately separate.
func captureDependencyProject(configPath, entry string, options ProjectOptions) (*dependencyProject, error) {
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return nil, err
	}
	fs := capturePackages(osvfs.FS())
	// Capture the requested link observation before parsing the canonical target.
	// Config-relative paths keep their legacy canonical-directory meaning.
	configPath = packagePath(fs.Realpath(packagePath(absolute)))
	directory := filepath.Dir(configPath)
	config, ds := tsoptions.GetParsedCommandLineOfConfigFile(configPath, nil, nil, configHost{fs, directory}, nil)
	if len(ds) > 0 {
		return &dependencyProject{Diagnostics: ds}, nil
	}
	if ds = config.GetConfigFileParsingDiagnostics(); len(ds) > 0 {
		return &dependencyProject{Diagnostics: ds}, nil
	}
	if message := unsupportedProjectOptions(config); message != "" {
		return nil, fmt.Errorf("ProjectOptions: %s", message)
	}
	if !filepath.IsAbs(entry) {
		entry = filepath.Join(directory, entry)
	}
	entry = packagePath(entry)
	if !slices.Contains(config.FileNames(), entry) || strings.HasSuffix(entry, ".d.ts") {
		return nil, fmt.Errorf("ProjectEntry: selected executable entry is not a configured root")
	}
	// Inference closes independently even when the selected entry does not use a
	// package: every configured strict TS root remains in the checking program.
	initial, err := completeDependencyInference(config, options, nil, fs)
	if err != nil {
		return nil, err
	}
	if len(initial.Diagnostics) > 0 {
		return &dependencyProject{Config: config, Program: initial.Program, Diagnostics: initial.Diagnostics}, nil
	}
	modules, implementations, err := closeRuntimeModules(entry, fs)
	if err != nil {
		return nil, err
	}
	complete, err := completeDependencyInference(config, options, implementations, fs)
	if err != nil {
		return nil, err
	}
	if err := validateSourceRuntimeCalls(complete.Program, modules); err != nil {
		return nil, err
	}
	snapshot, err := fs.Freeze()
	if err != nil {
		return nil, err
	}
	if err = snapshot.Revalidate(osvfs.FS()); err != nil {
		return nil, err
	}
	replay := replayPackages(snapshot)
	replayed, err := completeDependencyInference(config, options, implementations, replay)
	if err != nil {
		return nil, err
	}
	if _, _, err = closeRuntimeModules(entry, replay); err != nil {
		return nil, err
	}
	if err := validateSourceRuntimeCalls(replayed.Program, modules); err != nil {
		return nil, err
	}
	return &dependencyProject{Config: config, Snapshot: snapshot, Manifest: packageProjectManifest{entry, complete.Policy, modules, complete.Edges, StandardLibraryFingerprint()}, Program: replayed.Program, Diagnostics: replayed.Diagnostics}, nil
}
