package checked

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/microsoft/typescript-go/internal/module"
	"github.com/microsoft/typescript-go/tsox/graph"
)

type RuntimeMode string

const (
	RuntimeImportESM RuntimeMode = "import"
	RuntimeRequire   RuntimeMode = "require"
)

type RuntimeImport struct {
	Importer, Specifier string
	Mode                RuntimeMode
	Position            graph.Position
}
type RuntimeResolution struct {
	Module, Format, PackageScope, Selection string
	Edge                                    RuntimeImport
}
type packageResolver struct{ fs *packageCapture }
type packageScope struct {
	path string
	data *packageJSONValue
}

func (r *packageResolver) readPackage(p string) (*packageJSONValue, error) {
	text, ok := r.fs.ReadFile(p)
	if !ok {
		return nil, fmt.Errorf("unreadable package metadata %s", p)
	}
	v, err := parsePackageJSON(text)
	if err != nil {
		return nil, fmt.Errorf("invalid package metadata %s: %w", p, err)
	}
	return v, nil
}
func (r *packageResolver) nearestScope(source string) (packageScope, error) {
	for dir := filepath.Dir(source); ; dir = filepath.Dir(dir) {
		if filepath.Base(dir) == "node_modules" {
			break
		}
		name := filepath.Join(dir, "package.json")
		if r.fs.FileExists(name) {
			value, err := r.readPackage(name)
			return packageScope{name, value}, err
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return packageScope{}, nil
}
func (r *packageResolver) finishRuntime(edge RuntimeImport, target, selection string) (RuntimeResolution, error) {
	if !r.fs.FileExists(target) {
		return RuntimeResolution{}, fmt.Errorf("missing exact runtime target %s; legacy fallback is not implemented", target)
	}
	if packagePath(r.fs.Realpath(target)) != packagePath(target) {
		return RuntimeResolution{}, fmt.Errorf("runtime symlink/case identity is not implemented")
	}
	if filepath.Ext(target) == ".ts" && strings.Contains(packagePath(target), "/node_modules/") {
		return RuntimeResolution{}, fmt.Errorf("Node cannot strip package TypeScript implementation sources")
	}
	switch filepath.Ext(target) {
	case ".js", ".mjs", ".cjs", ".ts":
	default:
		return RuntimeResolution{}, fmt.Errorf("runtime extension is not implemented")
	}
	scope, err := r.nearestScope(target)
	if err != nil {
		return RuntimeResolution{}, err
	}
	text, ok := r.fs.ReadFile(target)
	if !ok {
		return RuntimeResolution{}, fmt.Errorf("cannot capture runtime source")
	}
	format, err := packageSourceFormat(target, text, scope.data.get("type").stringValue())
	if err != nil {
		return RuntimeResolution{}, err
	}
	if err = r.fs.Fault(); err != nil {
		return RuntimeResolution{}, err
	}
	scopePath := ""
	if scope.path != "" {
		scopePath = packagePath(scope.path)
	}
	return RuntimeResolution{packagePath(target), format, scopePath, selection, edge}, nil
}
func (r *packageResolver) ResolveRuntime(edge RuntimeImport) (result RuntimeResolution, err error) {
	defer func() {
		if err != nil {
			position := edge.Position
			position.SourcePath = edge.Importer
			err = &projectDiagnosticError{Diagnostic: graph.Diagnostic{SourcePath: edge.Importer, Position: position, Construct: "PackageRuntimeResolution", Message: err.Error()}, Cause: err}
		}
	}()
	if edge.Mode != RuntimeImportESM && edge.Mode != RuntimeRequire {
		return result, fmt.Errorf("unknown runtime mode")
	}
	if StandardModule(edge.Specifier) {
		return RuntimeResolution{Module: edge.Specifier, Format: "builtin", Selection: "compiler-standard-registry", Edge: edge}, nil
	}
	if strings.HasPrefix(edge.Specifier, "./") || strings.HasPrefix(edge.Specifier, "../") {
		if strings.ContainsAny(edge.Specifier, "%?#\\") {
			return result, fmt.Errorf("encoded/query runtime path is not implemented")
		}
		return r.finishRuntime(edge, filepath.Join(filepath.Dir(edge.Importer), edge.Specifier), "relative-exact")
	}
	scope, err := r.nearestScope(edge.Importer)
	if err != nil {
		return result, err
	}
	if strings.HasPrefix(edge.Specifier, "#") {
		if edge.Specifier == "#" || strings.HasPrefix(edge.Specifier, "#/") {
			return result, fmt.Errorf("invalid package import name")
		}
		target, err := packageMapTarget(scope.data.get("imports"), edge.Specifier, filepath.Dir(scope.path), edge.Mode, true)
		if err != nil {
			return result, err
		}
		if !target.selected() || target.blocked {
			return result, fmt.Errorf("package import is not defined")
		}
		if target.external != "" {
			external := edge
			external.Importer = scope.path
			external.Specifier = target.external
			resolved, err := r.ResolveRuntime(external)
			if err != nil {
				return result, err
			}
			resolved.Edge = edge
			resolved.Selection = "imports-external/" + resolved.Selection
			return resolved, nil
		}
		return r.finishRuntime(edge, target.path, "imports-exact")
	}
	name, rest := module.ParsePackageName(edge.Specifier)
	if name == "" || strings.HasPrefix(name, ".") || strings.ContainsAny(name, "\\%:#") || filepath.IsAbs(name) || strings.HasPrefix(name, "@") && (strings.Count(name, "/") != 1 || strings.HasSuffix(name, "/") || strings.HasPrefix(name, "@/")) {
		return result, fmt.Errorf("invalid or unsupported package name")
	}
	if strings.ContainsAny(rest, "%?#\\") || strings.Contains(rest, "..") {
		return result, fmt.Errorf("encoded/traversing package subpath is not implemented")
	}
	directory := ""
	metadata := (*packageJSONValue)(nil)
	if scope.data.get("name").stringValue() == name {
		exports := scope.data.get("exports")
		if exports != nil && exports.kind != 'n' {
			directory = filepath.Dir(scope.path)
			metadata = scope.data
		}
	}
	if directory == "" {
		for dir := filepath.Dir(edge.Importer); ; dir = filepath.Dir(dir) {
			if filepath.Base(dir) != "node_modules" {
				candidate := filepath.Join(dir, "node_modules", name)
				if r.fs.DirectoryExists(candidate) {
					directory = candidate
					break
				}
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	if directory == "" {
		return result, fmt.Errorf("package not found: %s", name)
	}
	if packagePath(r.fs.Realpath(directory)) != packagePath(directory) {
		return result, fmt.Errorf("package symlink/case identity is not implemented")
	}
	if metadata == nil {
		packageFile := filepath.Join(directory, "package.json")
		if r.fs.FileExists(packageFile) {
			metadata, err = r.readPackage(packageFile)
			if err != nil {
				return result, err
			}
		}
	}
	exports := metadata.get("exports")
	if exports != nil && exports.kind != 'n' {
		subpath := "."
		if rest != "" {
			subpath = "./" + rest
		}
		target, err := packageMapTarget(exports, subpath, directory, edge.Mode, false)
		if err != nil {
			return result, err
		}
		if !target.selected() || target.blocked {
			return result, fmt.Errorf("package subpath is not exported")
		}
		return r.finishRuntime(edge, target.path, "exports-exact")
	}
	target, selection := filepath.Join(directory, "index.js"), "index.js"
	if rest != "" {
		target = filepath.Join(directory, rest)
		selection = "subpath-exact"
	} else if main := metadata.get("main").stringValue(); main != "" {
		if filepath.IsAbs(main) || strings.ContainsAny(main, "\\%?#") {
			return result, fmt.Errorf("non-relative/URL main is not implemented")
		}
		target = filepath.Join(directory, main)
		relative, _ := filepath.Rel(directory, target)
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return result, fmt.Errorf("main outside package is not implemented")
		}
		selection = "main"
	}
	if filepath.Ext(target) == "" {
		return result, fmt.Errorf("extension/directory main fallback is not implemented")
	}
	return r.finishRuntime(edge, target, selection)
}
