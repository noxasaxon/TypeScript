// Package checked owns the bounded multi-file checker domain shared by graph
// extraction and entry-source mutation discovery.
package checked

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/bundled"
	"github.com/microsoft/typescript-go/internal/compiler"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/parser"
	"github.com/microsoft/typescript-go/internal/scanner"
	"github.com/microsoft/typescript-go/internal/tsoptions"
	"github.com/microsoft/typescript-go/internal/tspath"
	"github.com/microsoft/typescript-go/internal/vfs/vfstest"
	"github.com/microsoft/typescript-go/tsox/graph"
)

type Program struct {
	Compiler     *compiler.Program
	Entry        *ast.SourceFile
	Files        map[*ast.SourceFile]string
	RuntimeFiles []*ast.SourceFile
}

// Normalize gives map keys a stable absolute identity without consulting cwd.
func Normalize(name string) string {
	return path.Clean("/" + strings.TrimPrefix(filepath.ToSlash(name), "/"))
}

// ReadSources snapshots only explicit relative .ts dependencies. It does not
// consult package.json, tsconfig, node_modules, extension search, or indexes.
// The entry is canonicalized once; symlink dependencies are rejected because
// their additional realpath identity changes are outside this slice.
func ReadSources(entry string) (string, map[string]string, []graph.Diagnostic) {
	entry, err := filepath.Abs(entry)
	if err != nil {
		return "", nil, []graph.Diagnostic{sourceError(entry, err)}
	}
	canonical, err := filepath.EvalSymlinks(entry)
	if err != nil {
		return "", nil, []graph.Diagnostic{sourceError(entry, err)}
	}
	entry = canonical
	sources := make(map[string]string)
	var visit func(string, *ast.SourceFile, *ast.Node) *graph.Diagnostic
	visit = func(name string, importer *ast.SourceFile, importNode *ast.Node) *graph.Diagnostic {
		name = filepath.Clean(name)
		key := filepath.ToSlash(name)
		if _, ok := sources[key]; ok {
			return nil
		}
		readError := func(err error) *graph.Diagnostic {
			if importer != nil {
				return diagnostic(importer, importer.FileName(), importNode, "ModuleSource", key+": "+err.Error())
			}
			d := sourceError(key, err)
			return &d
		}
		real, err := filepath.EvalSymlinks(name)
		if err != nil {
			return readError(err)
		}
		if real != name {
			return readError(fmt.Errorf("unsupported module path through symlink"))
		}
		contents, err := os.ReadFile(name)
		if err != nil {
			return readError(err)
		}
		file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: key, Path: tspath.Path(key)}, string(contents), core.ScriptKindTS)
		imports, diagnostic := moduleImports(file, key)
		if diagnostic != nil {
			return diagnostic
		}
		sources[key] = string(contents)
		for _, dependency := range imports {
			if diagnostic := visit(filepath.Join(filepath.Dir(name), filepath.FromSlash(dependency.name)), file, dependency.node); diagnostic != nil {
				return diagnostic
			}
		}
		return nil
	}
	if diagnostic := visit(entry, nil, nil); diagnostic != nil {
		return "", nil, []graph.Diagnostic{*diagnostic}
	}
	return filepath.ToSlash(entry), sources, nil
}

func sourceError(name string, err error) graph.Diagnostic {
	return graph.Diagnostic{SourcePath: name, Position: graph.Position{Line: 1, Column: 1}, Construct: "ModuleSource", Message: err.Error()}
}

// New checks a complete source snapshot and computes ECMAScript dependency
// evaluation order. Import aliases stay in the same checker's symbol domain.
func New(entry string, sources map[string]string) (*Program, []graph.Diagnostic) {
	files := make(map[string]string, len(sources))
	labels := make(map[string]string, len(sources))
	keys := make([]string, 0, len(sources))
	for name := range sources {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		key := Normalize(name)
		if _, duplicate := files[key]; duplicate {
			return nil, []graph.Diagnostic{sourceError(name, fmt.Errorf("duplicate normalized module source"))}
		}
		files[key], labels[key] = sources[name], filepath.ToSlash(name)
	}
	entryKey := Normalize(entry)
	if _, ok := files[entryKey]; !ok {
		return nil, []graph.Diagnostic{sourceError(entry, fmt.Errorf("missing entry module source"))}
	}
	fs := bundled.WrapFS(vfstest.FromMap(files, true))
	host := compiler.NewCompilerHost("/", fs, bundled.LibPath(), nil, nil)
	options := &core.CompilerOptions{Strict: core.TSTrue, ModuleDetection: core.ModuleDetectionKindForce, Module: core.ModuleKindESNext, ModuleResolution: core.ModuleResolutionKindBundler, AllowImportingTsExtensions: core.TSTrue, NoEmit: core.TSTrue, VerbatimModuleSyntax: core.TSTrue}
	config := tsoptions.NewParsedCommandLine(options, []string{entryKey}, tspath.ComparePathsOptions{UseCaseSensitiveFileNames: true, CurrentDirectory: "/"})
	program := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host, SingleThreaded: core.TSTrue})
	program.BindSourceFiles()
	result := &Program{Compiler: program, Entry: program.GetSourceFile(entryKey), Files: make(map[*ast.SourceFile]string)}
	if result.Entry == nil {
		return nil, []graph.Diagnostic{sourceError(entry, fmt.Errorf("TypeScript program did not load entry source"))}
	}
	states := make(map[string]int)
	dependencies := make(map[*ast.SourceFile][]*ast.SourceFile)
	var all []*ast.SourceFile
	var visit func(string, *ast.Node, *ast.SourceFile) *graph.Diagnostic
	visit = func(key string, importNode *ast.Node, importer *ast.SourceFile) *graph.Diagnostic {
		if states[key] == 1 {
			return diagnostic(importer, result.Files[importer], importNode, "ModuleCycle", "unsupported module dependency cycle")
		}
		if states[key] == 2 {
			return nil
		}
		file := program.GetSourceFile(key)
		if file == nil {
			if importer != nil {
				return diagnostic(importer, result.Files[importer], importNode, "ModuleSource", "missing relative module source "+key)
			}
			d := sourceError(labels[key], fmt.Errorf("missing relative module source"))
			return &d
		}
		result.Files[file] = labels[key]
		if ds := program.GetSyntacticDiagnostics(context.Background(), file); len(ds) != 0 {
			d := TypeScriptDiagnostic(file, labels[key], ds[0])
			return &d
		}
		imports, d := moduleImports(file, labels[key])
		if d != nil {
			return d
		}
		states[key] = 1
		for _, dependency := range imports {
			target := path.Clean(path.Join(path.Dir(key), dependency.name))
			if d := visit(target, dependency.node, file); d != nil {
				return d
			}
			if !dependency.typeOnly {
				dependencies[file] = append(dependencies[file], program.GetSourceFile(target))
			}
		}
		states[key] = 2
		all = append(all, file)
		return nil
	}
	if diagnostic := visit(entryKey, nil, nil); diagnostic != nil {
		return nil, []graph.Diagnostic{*diagnostic}
	}
	for _, file := range all {
		if ds := program.GetSemanticDiagnostics(context.Background(), file); len(ds) != 0 {
			return nil, []graph.Diagnostic{TypeScriptDiagnostic(file, result.Files[file], ds[0])}
		}
	}
	seen := make(map[*ast.SourceFile]bool)
	var order func(*ast.SourceFile)
	order = func(file *ast.SourceFile) {
		if seen[file] {
			return
		}
		seen[file] = true
		for _, dependency := range dependencies[file] {
			order(dependency)
		}
		result.RuntimeFiles = append(result.RuntimeFiles, file)
	}
	order(result.Entry)
	return result, nil
}

type dependency struct {
	name     string
	node     *ast.Node
	typeOnly bool
}

func moduleImports(file *ast.SourceFile, sourcePath string) ([]dependency, *graph.Diagnostic) {
	var imports []dependency
	for _, statement := range file.Statements.Nodes {
		switch statement.Kind {
		case ast.KindImportDeclaration:
			data := statement.AsImportDeclaration()
			if data.Modifiers() != nil || data.Attributes != nil || data.ModuleSpecifier == nil || data.ModuleSpecifier.Kind != ast.KindStringLiteral {
				return nil, diagnostic(file, sourcePath, statement, "ModuleImport", "unsupported import declaration or attributes")
			}
			name, d := moduleSpecifier(file, sourcePath, data.ModuleSpecifier)
			if d != nil {
				return nil, d
			}
			typeOnly := false
			if data.ImportClause != nil {
				clause := data.ImportClause.AsImportClause()
				if clause.Name() != nil || clause.NamedBindings == nil || clause.NamedBindings.Kind != ast.KindNamedImports || (clause.PhaseModifier != ast.KindUnknown && clause.PhaseModifier != ast.KindTypeKeyword) {
					return nil, diagnostic(file, sourcePath, data.ImportClause, "ModuleImport", "unsupported import: only named, import type, and side-effect imports are supported")
				}
				typeOnly = clause.PhaseModifier == ast.KindTypeKeyword
				for _, specifier := range clause.NamedBindings.AsNamedImports().Elements.Nodes {
					data := specifier.AsImportSpecifier()
					if data.IsTypeOnly || data.Name().Kind != ast.KindIdentifier || (data.PropertyName != nil && data.PropertyName.Kind != ast.KindIdentifier) {
						return nil, diagnostic(file, sourcePath, specifier, "ModuleImport", "unsupported import specifier: use declaration-level import type and identifier names")
					}
				}
			}
			imports = append(imports, dependency{name: name, node: statement, typeOnly: typeOnly})
		case ast.KindExportDeclaration:
			data := statement.AsExportDeclaration()
			if data.Modifiers() != nil || data.Attributes != nil || data.ExportClause == nil || data.ExportClause.Kind != ast.KindNamedExports {
				return nil, diagnostic(file, sourcePath, statement, "ModuleExport", "unsupported export: expected a named export list without attributes")
			}
			for _, specifier := range data.ExportClause.AsNamedExports().Elements.Nodes {
				item := specifier.AsExportSpecifier()
				if item.Name().Kind != ast.KindIdentifier || item.Name().Text() == "default" || (item.PropertyName != nil && (item.PropertyName.Kind != ast.KindIdentifier || item.PropertyName.Text() == "default")) {
					return nil, diagnostic(file, sourcePath, specifier, "ModuleExport", "unsupported export specifier: expected non-default identifier names")
				}
			}
			if data.ModuleSpecifier != nil {
				name, d := moduleSpecifier(file, sourcePath, data.ModuleSpecifier)
				if d != nil {
					return nil, d
				}
				// Node retains loading for inline-only type lists and empty lists.
				// Only declaration-level export type erases the runtime edge.
				imports = append(imports, dependency{name: name, node: statement, typeOnly: data.IsTypeOnly})
			}
		case ast.KindExportAssignment:
			return nil, diagnostic(file, sourcePath, statement, "ModuleExport", "unsupported default export or export assignment")
		case ast.KindImportEqualsDeclaration:
			return nil, diagnostic(file, sourcePath, statement, "ModuleImport", "unsupported import equals declaration")
		}
	}
	var found *graph.Diagnostic
	var visit func(*ast.Node) bool
	visit = func(node *ast.Node) bool {
		if node.Kind == ast.KindCallExpression && node.AsCallExpression().Expression.Kind == ast.KindImportKeyword {
			found = diagnostic(file, sourcePath, node, "DynamicImport", "unsupported dynamic import")
			return true
		}
		return node.ForEachChild(visit)
	}
	file.AsNode().ForEachChild(visit)
	return imports, found
}

func moduleSpecifier(file *ast.SourceFile, sourcePath string, node *ast.Node) (string, *graph.Diagnostic) {
	if node.Kind != ast.KindStringLiteral {
		return "", diagnostic(file, sourcePath, node, "ModuleSpecifier", "unsupported module specifier: expected explicit relative .ts source")
	}
	name := node.Text()
	if !(strings.HasPrefix(name, "./") || strings.HasPrefix(name, "../")) || !strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".d.ts") || strings.ContainsAny(name, "\\?#%") {
		return "", diagnostic(file, sourcePath, node, "ModuleSpecifier", "unsupported module specifier: expected explicit relative .ts source")
	}
	return name, nil
}

func diagnostic(file *ast.SourceFile, sourcePath string, node *ast.Node, construct, message string) *graph.Diagnostic {
	pos := scanner.GetTokenPosOfNode(node, file, false)
	line, column := scanner.GetECMALineAndUTF16CharacterOfPosition(file, pos)
	return &graph.Diagnostic{SourcePath: sourcePath, Position: graph.Position{Line: line + 1, Column: int(column) + 1}, Construct: construct, Message: message}
}

func TypeScriptDiagnostic(file *ast.SourceFile, sourcePath string, d *ast.Diagnostic) graph.Diagnostic {
	position := graph.Position{Line: 1, Column: 1}
	if d.File() != nil && d.Pos() >= 0 {
		line, column := scanner.GetECMALineAndUTF16CharacterOfPosition(d.File(), d.Pos())
		position = graph.Position{Line: line + 1, Column: int(column) + 1}
	}
	return graph.Diagnostic{SourcePath: sourcePath, Position: position, Construct: "TypeScriptDiagnostic", Message: "TypeScript diagnostic: " + d.String()}
}
