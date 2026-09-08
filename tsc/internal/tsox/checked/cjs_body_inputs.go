package checked

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
)

type CJSBodyCell struct {
	Module            string
	Environment, Cell int
	Source            SourceNodeID
	Symbol            *ast.Symbol `json:"-"`
	// Identity only. The initialized value is not copied into a body constant.
	NeedsRuntimeBinding bool
}
type CJSWrapperUse struct {
	Source              SourceNodeID
	Parameter           string
	Node                *ast.Node `json:"-"`
	NeedsRuntimeBinding bool
}
type CJSCallableBodyInput struct {
	Program     *Program        `json:"-"`
	State       *CJSExportState `json:"-"`
	Callable    ModuleCallableReference
	Strict      bool
	Cells       []CJSBodyCell
	Wrappers    []CJSWrapperUse
	Obligations []CJSStateObligation
}

// Source project adapter only: the caller supplies explicit realm premises.
// Complete source-state analysis still leaves every native startup obligation.
type CJSBodySourceSnapshot struct {
	syntax              *sourceSyntaxSeal
	sourceDigest        [32]byte
	Program             *Program
	States              map[string]*CJSExportState
	Plans               *RuntimeSourcePlans
	ConfiguredRoots     int
	ConfiguredFiles     []string
	CapturedFingerprint string
	RuntimeModules      []CJSBodyRuntimeModule
}
type CJSBodyRuntimeModule struct {
	ID, Format   string
	Dependencies []RuntimeResolution
}

func ReadCJSBodySourceProject(config, entry string, realm CJSExportBoundary) (*Program, map[string]*CJSExportState, int, error) {
	snapshot, err := ReadCJSBodySourceSnapshot(config, entry, realm)
	if err != nil {
		return nil, nil, 0, err
	}
	return snapshot.Program, snapshot.States, snapshot.ConfiguredRoots, nil
}
func ReadCJSBodySourceSnapshot(config, entry string, realm CJSExportBoundary) (*CJSBodySourceSnapshot, error) {
	project, err := captureDependencyProject(config, entry, ProjectOptions{DependencyTypesInferJS})
	if err != nil {
		return nil, err
	}
	if len(project.Diagnostics) > 0 {
		return nil, fmt.Errorf("strict project diagnostics: %v", project.Diagnostics)
	}
	syntax, err := captureSourceProgramSyntax(project.Program)
	if err != nil {
		return nil, err
	}
	return sourceSnapshotFromDependency(project, realm, syntax)
}

func sourceSnapshotFromDependency(project *dependencyProject, realm CJSExportBoundary, syntax *sourceSyntaxSeal) (*CJSBodySourceSnapshot, error) {
	if err := syntax.validate(project.Program); err != nil {
		return nil, err
	}
	c, done := project.Program.GetTypeChecker(context.Background())
	defer done()
	plans, err := planRuntimeSources(project, c)
	if err != nil {
		return nil, err
	}
	p := &Program{Compiler: project.Program, Entry: project.Program.GetSourceFile(project.Manifest.Entry), Files: map[*ast.SourceFile]string{}}
	// Source declaration authority is independent of selected runtime startup.
	// These exact source files already belong to the captured compiler Program.
	for _, file := range project.Program.GetSourceFiles() {
		if !file.IsDeclarationFile {
			p.Files[file] = file.FileName()
		}
	}
	states := map[string]*CJSExportState{}
	for _, module := range project.Manifest.Modules {
		if module.Format == "builtin" {
			continue
		}
		file := project.Program.GetSourceFile(module.ID)
		if file == nil {
			return nil, fmt.Errorf("missing actual source %s", module.ID)
		}
		p.Files[file] = module.ID
		p.RuntimeFiles = append(p.RuntimeFiles, file)
		if module.Format == "commonjs" {
			state, e := analyzeCJSExportState(plans.Modules[module.ID], c, realm)
			if e != nil {
				return nil, e
			}
			states[module.ID] = state
		}
	}
	captured, err := json.Marshal(struct {
		Snapshot packageSnapshot
		Manifest packageProjectManifest
		Roots    []string
	}{project.Snapshot, project.Manifest, project.Config.FileNames()})
	if err != nil {
		return nil, err
	}
	var runtime []CJSBodyRuntimeModule
	for _, module := range project.Manifest.Modules {
		runtime = append(runtime, CJSBodyRuntimeModule{module.ID, module.Format, slices.Clone(module.Dependencies)})
	}
	snapshot := &CJSBodySourceSnapshot{Program: p, States: states, Plans: plans, ConfiguredRoots: len(project.Config.FileNames()), ConfiguredFiles: slices.Clone(project.Config.FileNames()), CapturedFingerprint: fmt.Sprintf("%x", sha256.Sum256(captured)), RuntimeModules: runtime}
	if e := sealSourceSnapshot(snapshot, syntax); e != nil {
		return nil, e
	}
	return snapshot, nil
}
func CJSBodyInput(p *Program, state *CJSExportState, name string) (*CJSCallableBodyInput, error) {
	if p == nil || state == nil || state.Plan == nil || !state.Complete || state.Plan.Format != "commonjs" {
		return nil, fmt.Errorf("actual complete CJS source state required")
	}
	file := state.Plan.File
	if file == nil || file.IsDeclarationFile || p.Compiler.GetSourceFile(file.FileName()) != file || p.Files[file] == "" {
		return nil, fmt.Errorf("module does not belong to actual immutable Program")
	}
	ref, ok := state.CallableReference(name)
	if !ok {
		return nil, fmt.Errorf("export has no actual evaluated callable instance")
	}
	if ref.Template.Node == nil || ast.GetSourceFileOfNode(ref.Template.Node) != file || sourceNodeID(file, ref.Template.Node) != ref.Template.Declaration {
		return nil, fmt.Errorf("callable source identity mismatch")
	}
	if !cjsSynchronous(ref.Template.Node) {
		return nil, fmt.Errorf("CJS synchronous body contract does not admit async/generator invocation")
	}
	out := &CJSCallableBodyInput{Program: p, State: state, Callable: ref, Strict: state.Instances[ref.Instance].Strict}
	cells, _ := state.CallableEnvironmentCells(name)
	seen := map[*ast.Symbol]bool{}
	for _, cell := range cells {
		if seen[cell.Symbol] {
			continue
		}
		seen[cell.Symbol] = true
		env := -1
		for _, candidate := range state.Environments {
			if slices.Contains(candidate.Cells, cell.ID) {
				env = candidate.ID
				break
			}
		}
		if env < 0 {
			return nil, fmt.Errorf("source cell has no lexical environment")
		}
		out.Cells = append(out.Cells, CJSBodyCell{state.Plan.Module, env, cell.ID, cell.Binding, cell.Symbol, true})
	}
	c, done := p.Compiler.GetTypeChecker(context.Background())
	defer done()
	var visit func(*ast.Node) bool
	visit = func(n *ast.Node) bool {
		if n.Kind == ast.KindIdentifier && (!ast.IsDeclarationName(n) || n.Parent.Kind == ast.KindShorthandPropertyAssignment) && !(n.Parent != nil && n.Parent.Kind == ast.KindPropertyAccessExpression && n.Parent.AsPropertyAccessExpression().Name() == n) {
			name, ambiguous := bodyWrapperBinding(file, n, c)
			if ambiguous {
				out.Obligations = append(out.Obligations, CJSStateObligation{sourceNodeID(file, n), "wrapper var instantiation/value order requires actual wrapper cell proof"})
			}
			if name != "" {
				out.Wrappers = append(out.Wrappers, CJSWrapperUse{sourceNodeID(file, n), name, n, true})
			}
		}
		return n.ForEachChild(visit)
	}
	ref.Template.Node.ForEachChild(visit)
	return out, nil
}
func bodyWrapperBinding(file *ast.SourceFile, n *ast.Node, c *checker.Checker) (string, bool) {
	name := n.Text()
	if name != "exports" && name != "module" && name != "require" && name != "__filename" && name != "__dirname" {
		return "", false
	}
	symbol := c.GetSymbolAtLocation(n)
	if n.Parent != nil && n.Parent.Kind == ast.KindShorthandPropertyAssignment {
		symbol = c.GetShorthandAssignmentValueSymbol(n.Parent)
	}
	if symbol != nil {
		for _, decl := range symbol.Declarations {
			if ast.GetSourceFileOfNode(decl) == file && (cjsWrapperVarDeclaration(decl) != nil || (decl.Kind == ast.KindFunctionDeclaration && decl.Parent == file.AsNode())) {
				return "", true
			}
		}
		for _, decl := range symbol.Declarations {
			source := ast.GetSourceFileOfNode(decl)
			if source != nil && !source.IsDeclarationFile && decl != file.AsNode() {
				if cjsWrapperVarDeclaration(decl) != nil {
					return "", true
				}
				return "", false // Actual nested parameter/function or lexical declaration.
			}
		}
	}
	return name, false // Actual CJS lexical wrapper, not a type-derived value.
}
