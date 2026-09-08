package checked

import (
	"context"
	"encoding/json"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/core"
	"github.com/microsoft/typescript-go/internal/parser"
	"github.com/microsoft/typescript-go/internal/tsoptions"
	"github.com/microsoft/typescript-go/internal/tspath"
	"github.com/microsoft/typescript-go/internal/vfs"
	"github.com/microsoft/typescript-go/internal/vfs/osvfs"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// Project owns one filesystem snapshot and the options parsed from that snapshot.
// SourceFiles includes source files, extended configs, and package metadata, so build
// fingerprints cover every loaded user input. Bundled standard libraries are
// versioned with the compiler. Fields are read-only after ReadProject returns.
type Project struct {
	sourceSyntax *sourceSyntaxSeal
	Entry        string
	ConfigPath   string
	sources      map[string]string
	config       *tsoptions.ParsedCommandLine
	dependency   *dependencyProject
	resolution   []byte
}

type configHost struct {
	fs        vfs.FS
	directory string
}

func (h configHost) FS() vfs.FS                  { return h.fs }
func (h configHost) GetCurrentDirectory() string { return h.directory }

// Capture config/extends/package reads made by upstream config resolution. A
// repeated read always returns the first contents; checking later uses no OS FS.
type recordingFS struct {
	vfs.FS
	sources map[string]string
}

func (f *recordingFS) ReadFile(name string) (string, bool) {
	name = filepath.ToSlash(filepath.Clean(name))
	if text, ok := f.sources[name]; ok {
		return text, true
	}
	text, ok := f.FS.ReadFile(name)
	if ok {
		f.sources[name] = text
	}
	return text, ok
}

// ReadProject supports strict NodeNext ESM projects with explicit relative .ts
// runtime dependencies. Package implementations and Fetch admission remain later
// release work. Entry is resolved relative to the config directory and must be a
// configured root (an excluded file can still be an imported dependency).
func ReadProject(configPath, entry string) (*Project, []graph.Diagnostic) {
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return nil, []graph.Diagnostic{sourceError(configPath, err)}
	}
	configPath, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, []graph.Diagnostic{sourceError(absolute, err)}
	}
	configPath = filepath.ToSlash(configPath)
	directory := filepath.Dir(configPath)
	fs := &recordingFS{FS: osvfs.FS(), sources: map[string]string{}}
	config, ds := tsoptions.GetParsedCommandLineOfConfigFile(configPath, nil, nil, configHost{fs, directory}, nil)
	if len(ds) != 0 {
		return nil, projectDiagnostics(configPath, ds)
	}
	if ds := config.GetConfigFileParsingDiagnostics(); len(ds) != 0 {
		return nil, projectDiagnostics(configPath, ds)
	}
	if message := unsupportedProjectOptions(config); message != "" {
		return nil, []graph.Diagnostic{projectError(configPath, "ProjectOptions", message)}
	}
	if entry == "" {
		return nil, []graph.Diagnostic{projectError(configPath, "ProjectEntry", "project entry source must be specified")}
	}
	if !filepath.IsAbs(entry) {
		entry = filepath.Join(directory, entry)
	}
	entry = filepath.ToSlash(filepath.Clean(entry))
	if !slices.Contains(config.FileNames(), entry) {
		return nil, []graph.Diagnostic{projectError(configPath, "ProjectEntry", "entry is not a root selected by tsconfig files/include/exclude: "+entry)}
	}
	if strings.HasSuffix(entry, ".d.ts") {
		return nil, []graph.Diagnostic{projectError(entry, "ProjectEntry", "entry must be an executable .ts source")}
	}
	visited := map[string]bool{}
	var visit func(string, *ast.SourceFile, *ast.Node) *graph.Diagnostic
	visit = func(name string, importer *ast.SourceFile, importNode *ast.Node) *graph.Diagnostic {
		name = filepath.ToSlash(filepath.Clean(name))
		if visited[name] {
			return nil
		}
		fail := func(message string) *graph.Diagnostic {
			if importer != nil {
				return diagnostic(importer, importer.FileName(), importNode, "ModuleSource", message)
			}
			d := projectError(name, "ModuleSource", message)
			return &d
		}
		if !strings.HasSuffix(name, ".ts") {
			return fail("project currently requires .ts source roots")
		}
		real, err := filepath.EvalSymlinks(name)
		if err != nil {
			return fail(err.Error())
		}
		if filepath.ToSlash(real) != name {
			return fail("unsupported module path through symlink")
		}
		source, ok := fs.ReadFile(name)
		if !ok {
			return fail("cannot read project source " + name)
		}
		visited[name] = true
		if d := snapshotPackage(fs, name); d != nil {
			return d
		}
		file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: name, Path: tspath.Path(name)}, source, core.ScriptKindTS)
		imports, d := moduleImports(file, name)
		if d != nil {
			return d
		}
		for _, dependency := range imports {
			if StandardModule(dependency.name) {
				continue
			}
			if d := visit(filepath.Join(filepath.Dir(name), dependency.name), file, dependency.node); d != nil {
				return d
			}
		}
		return nil
	}
	for _, root := range config.FileNames() {
		if d := visit(root, nil, nil); d != nil {
			return nil, []graph.Diagnostic{*d}
		}
	}
	return &Project{Entry: entry, ConfigPath: configPath, sources: maps.Clone(fs.sources), config: config}, nil
}

func (p *Project) Check() (*Program, []graph.Diagnostic) {
	if p == nil || p.config == nil {
		return nil, []graph.Diagnostic{projectError("", "ProjectSource", "project must be created by ReadProject")}
	}
	if p.dependency != nil {
		return p.checkDependencyProject()
	}
	program, ds := newProgram(p.Entry, p.sources, p.config)
	if len(ds) != 0 {
		return nil, ds
	}
	if d := validateRuntimeNames(program); d != nil {
		return nil, []graph.Diagnostic{*d}
	}
	return program, nil
}

func unsupportedProjectOptions(p *tsoptions.ParsedCommandLine) string {
	o := p.CompilerOptions()
	if o.Strict != core.TSTrue || o.StrictNullChecks == core.TSFalse || o.NoImplicitAny == core.TSFalse || o.NoImplicitThis == core.TSFalse || o.StrictFunctionTypes == core.TSFalse || o.StrictBindCallApply == core.TSFalse || o.StrictPropertyInitialization == core.TSFalse || o.StrictBuiltinIteratorReturn == core.TSFalse || o.UseUnknownInCatchVariables == core.TSFalse || o.NoCheck == core.TSTrue {
		return "project currently requires strict=true without disabled strict checks or noCheck"
	}
	if o.Module != core.ModuleKindNodeNext || o.GetModuleResolutionKind() != core.ModuleResolutionKindNodeNext {
		return "project currently requires module=NodeNext and NodeNext moduleResolution"
	}
	if o.AllowImportingTsExtensions != core.TSTrue && o.RewriteRelativeImportExtensions != core.TSTrue {
		return "project currently requires allowImportingTsExtensions or rewriteRelativeImportExtensions for explicit .ts runtime imports"
	}
	if len(p.ProjectReferences()) != 0 || o.Paths != nil || o.BaseUrl != "" || len(o.RootDirs) != 0 || len(o.ModuleSuffixes) != 0 || len(o.TypeRoots) != 0 || len(o.Types) != 0 || o.AllowJs == core.TSTrue || o.NoResolve == core.TSTrue || o.PreserveSymlinks == core.TSTrue || len(o.CustomConditions) != 0 {
		return "project references, path remapping, custom conditions, external type packages, JavaScript roots, noResolve and preserved symlinks are not yet supported by project loading"
	}
	return ""
}

func snapshotPackage(fs *recordingFS, source string) *graph.Diagnostic {
	for directory := filepath.Dir(source); ; directory = filepath.Dir(directory) {
		name := filepath.ToSlash(filepath.Join(directory, "package.json"))
		if fs.FileExists(name) {
			source, ok := fs.ReadFile(name)
			if !ok {
				d := projectError(name, "ProjectPackage", "cannot read package.json")
				return &d
			}
			var metadata struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(source), &metadata); err != nil {
				d := projectError(name, "ProjectPackage", "invalid package.json: "+err.Error())
				if syntax, ok := err.(*json.SyntaxError); ok {
					prefix := source[:min(len(source), max(0, int(syntax.Offset)-1))]
					d.Position.Line = strings.Count(prefix, "\n") + 1
					d.Position.Column = len(utf16.Encode([]rune(prefix[strings.LastIndex(prefix, "\n")+1:]))) + 1
				}
				return &d
			}
			if metadata.Type != "module" {
				d := projectError(name, "ProjectPackage", "project currently requires nearest package.json type=module")
				return &d
			}
			return nil
		}
		if parent := filepath.Dir(directory); parent == directory {
			break
		}
	}
	d := projectError(source, "ProjectPackage", "project currently requires a package.json with type=module")
	return &d
}

func projectError(name, construct, message string) graph.Diagnostic {
	return graph.Diagnostic{SourcePath: name, Position: graph.Position{Line: 1, Column: 1}, Construct: construct, Message: message}
}

func projectDiagnostics(configPath string, diagnostics []*ast.Diagnostic) []graph.Diagnostic {
	result := make([]graph.Diagnostic, 0, len(diagnostics))
	for _, d := range diagnostics {
		name := configPath
		if d.File() != nil {
			name = d.File().FileName()
		}
		result = append(result, TypeScriptDiagnostic(d.File(), name, d))
	}
	return result
}

// Node strips type syntax, but does not perform TypeScript's semantic import
// elision. Validate retained names with the configured checker rather than
// overriding verbatimModuleSyntax in the user's options.
func validateRuntimeNames(program *Program) *graph.Diagnostic {
	c, done := program.Compiler.GetTypeChecker(context.Background())
	defer done()
	for _, file := range program.RuntimeFiles {
		check := func(node *ast.Node, symbol *ast.Symbol) *graph.Diagnostic {
			if symbol != nil && symbol.Flags&ast.SymbolFlagsAlias != 0 {
				symbol = c.GetAliasedSymbol(symbol)
			}
			if !standardRuntimeSymbol(symbol) && !hasRuntimeValue(symbol) {
				return diagnostic(file, program.Files[file], node, "ModuleRuntimeName", "Node retains this import/export name but its declaration is erased; use import type or export type")
			}
			return nil
		}
		for _, statement := range file.Statements.Nodes {
			switch statement.Kind {
			case ast.KindImportDeclaration:
				clause := statement.AsImportDeclaration().ImportClause
				if clause == nil || clause.AsImportClause().PhaseModifier == ast.KindTypeKeyword {
					continue
				}
				// moduleImports already admitted only named import bindings.
				for _, specifier := range clause.AsImportClause().NamedBindings.AsNamedImports().Elements.Nodes {
					if d := check(specifier, c.GetSymbolAtLocation(specifier.Name())); d != nil {
						return d
					}
				}
			case ast.KindExportDeclaration:
				declaration := statement.AsExportDeclaration()
				if declaration.IsTypeOnly {
					continue
				}
				for _, specifier := range declaration.ExportClause.AsNamedExports().Elements.Nodes {
					if specifier.AsExportSpecifier().IsTypeOnly {
						continue
					}
					if d := check(specifier, c.GetExportSpecifierLocalTargetSymbol(specifier)); d != nil {
						return d
					}
				}
			}
		}
	}
	return nil
}

// Value symbols also include ambient declarations and overload signatures, which
// Node's type stripping erases. A retained name needs an executable declaration.
func hasRuntimeValue(symbol *ast.Symbol) bool {
	if symbol == nil || symbol.Flags&ast.SymbolFlagsValue == 0 {
		return false
	}
	for _, declaration := range symbol.Declarations {
		ambient := false
		for node := declaration; node != nil; node = node.Parent {
			if ast.HasAmbientModifier(node) {
				ambient = true
				break
			}
		}
		if ambient || strings.HasSuffix(ast.GetSourceFileOfNode(declaration).FileName(), ".d.ts") {
			continue
		}
		if declaration.Kind == ast.KindFunctionDeclaration && declaration.AsFunctionDeclaration().Body == nil {
			continue
		}
		if declaration.Kind == ast.KindInterfaceDeclaration || declaration.Kind == ast.KindTypeAliasDeclaration {
			continue
		}
		return true
	}
	return false
}

// AsyncEntry resolves the configured public export to its defining function.
// The runtime module order stays that of the original entry graph, including
// reexport modules; only declaration selection uses the resolved source file.
func AsyncEntry(program *Program, name string) (*Program, string, *graph.Diagnostic) {
	c, done := program.Compiler.GetTypeChecker(context.Background())
	defer done()
	fail := func(message string) (*Program, string, *graph.Diagnostic) {
		d := projectError(program.Entry.FileName(), "ProjectEntry", message)
		return nil, "", &d
	}
	module := program.Entry.AsNode().Symbol()
	if module == nil {
		return fail("entry is not a runtime module")
	}
	for _, exported := range c.GetExportsOfModule(module) {
		if ast.SymbolName(exported) != name {
			continue
		}
		symbol := exported
		if symbol.Flags&ast.SymbolFlagsAlias != 0 {
			symbol = c.GetAliasedSymbol(symbol)
		}
		if !hasRuntimeValue(symbol) {
			return fail("configured entry export has no runtime value: " + name)
		}
		for _, declaration := range symbol.Declarations {
			if declaration.Kind != ast.KindFunctionDeclaration || declaration.AsFunctionDeclaration().Body == nil {
				continue
			}
			file := ast.GetSourceFileOfNode(declaration)
			if _, ok := program.Files[file]; !ok {
				return fail("entry export is outside the executable module graph: " + name)
			}
			selected := *program
			selected.Entry = file
			return &selected, declaration.Name().Text(), nil
		}
		return fail("configured async entry export requires a function declaration: " + name)
	}
	return fail("configured entry export does not exist: " + name)
}

// SourceFiles returns a defensive copy of the exact immutable compiler inputs.
func (p *Project) SourceFiles() map[string]string {
	if p == nil {
		return nil
	}
	return maps.Clone(p.sources)
}
