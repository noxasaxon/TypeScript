package checked

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// SourceRecoveryScope is a compile-time identity index. It neither executes
// module initialization nor certifies complete effect or native value coverage.
// Builders sharing this scope must serialize registration, like checker access.
type SourceRecoveryScope struct {
	integrity    *sourceSyntaxIntegrity
	snapshot     *CJSBodySourceSnapshot
	view         graph.SourceRegistryView
	files        map[*ast.SourceFile]graph.SourceFileID
	nodes        map[*ast.Node]bool
	modules      map[string]graph.SourceModuleID
	bindings     map[*ast.Symbol]graph.SourceLexicalID
	declarations map[*ast.Node]graph.SourceDeclarationID
	templates    map[*ast.Node]graph.SourceTemplateID
	wrappers     map[graph.SourceModuleID]map[string]graph.SourceLexicalID
}

// ScopedBinding cannot be constructed from a graph ID or another scope's node.
type ScopedBinding struct {
	owner *SourceRecoveryScope
	id    graph.SourceLexicalID
}

func NewSourceRecoveryScope(snapshot *CJSBodySourceSnapshot) (*SourceRecoveryScope, error) {
	if snapshot == nil || snapshot.Program == nil || snapshot.Program.Compiler == nil || snapshot.Plans == nil || snapshot.CapturedFingerprint == "" {
		return nil, fmt.Errorf("actual captured Program and runtime source plans required")
	}
	if err := snapshot.validateSeal(); err != nil {
		return nil, err
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	s := &SourceRecoveryScope{snapshot: snapshot, files: map[*ast.SourceFile]graph.SourceFileID{}, nodes: map[*ast.Node]bool{}, modules: map[string]graph.SourceModuleID{}, bindings: map[*ast.Symbol]graph.SourceLexicalID{}, declarations: map[*ast.Node]graph.SourceDeclarationID{}, templates: map[*ast.Node]graph.SourceTemplateID{}, wrappers: map[graph.SourceModuleID]map[string]graph.SourceLexicalID{}}
	s.view.Scope = graph.SourceScopeID(hex.EncodeToString(token[:]))
	s.view.SnapshotFingerprint = snapshot.CapturedFingerprint
	s.view.Revision = 1
	files := append([]*ast.SourceFile(nil), snapshot.Program.Compiler.GetSourceFiles()...)
	sort.Slice(files, func(i, j int) bool { return files[i].FileName() < files[j].FileName() })
	for _, file := range files {
		if snapshot.Program.Compiler.GetSourceFile(file.FileName()) != file {
			return nil, fmt.Errorf("foreign compiler file")
		}
		id := graph.SourceFileID(len(s.view.Files) + 1)
		s.files[file] = id
		s.view.Files = append(s.view.Files, graph.SourceFileRecord{ID: id, Path: file.FileName(), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(file.Text()))), DeclarationFile: file.IsDeclarationFile, OwnedSchema: snapshot.Program.Files[file] == file.FileName()})
		var visit func(*ast.Node) bool
		visit = func(n *ast.Node) bool {
			if n == nil {
				return false
			}
			s.nodes[n] = true
			n.ForEachChild(visit)
			return false
		}
		visit(file.AsNode())
	}
	if len(snapshot.ConfiguredFiles) != snapshot.ConfiguredRoots {
		return nil, fmt.Errorf("configured root inventory mismatch")
	}
	for _, path := range snapshot.ConfiguredFiles {
		file := snapshot.Program.Compiler.GetSourceFile(path)
		id := s.files[file]
		if id == 0 {
			return nil, fmt.Errorf("configured root missing from actual Program: %s", path)
		}
		s.view.ConfiguredRoots = append(s.view.ConfiguredRoots, id)
		s.view.Files[id-1].ConfiguredRoot = true
	}
	runtimeModules := append([]CJSBodyRuntimeModule(nil), snapshot.RuntimeModules...)
	knownModules := map[string]bool{}
	for _, module := range runtimeModules {
		knownModules[module.ID] = true
	}
	// Captured builtin dependency edges have runtime identity but no source file
	// or ModuleExportPlan. Keep that role explicit; this grants no producer proof.
	for _, module := range snapshot.RuntimeModules {
		for _, edge := range module.Dependencies {
			if edge.Format == "builtin" && !knownModules[edge.Module] {
				knownModules[edge.Module] = true
				runtimeModules = append(runtimeModules, CJSBodyRuntimeModule{ID: edge.Module, Format: "builtin"})
			}
		}
	}
	sourceModules := 0
	for i, module := range runtimeModules {
		if s.modules[module.ID] != 0 {
			return nil, fmt.Errorf("duplicate runtime module identity")
		}
		id := graph.SourceModuleID(len(s.view.Modules) + 1)
		s.modules[module.ID] = id
		record := graph.SourceModuleRecord{ID: id, Identity: module.ID, Format: module.Format, ClosureOrder: i, StartupStatus: "unproved", EffectStatus: "unproved"}
		for _, edge := range module.Dependencies {
			record.Dependencies = append(record.Dependencies, graph.SourceRuntimeEdge{Specifier: edge.Edge.Specifier, Mode: string(edge.Edge.Mode), Module: edge.Module, Format: edge.Format})
		}
		if module.Format == "builtin" {
			record.StartupStatus = "external-contract-pending"
			s.view.Obligations = append(s.view.Obligations, graph.SourceRecoveryObligation{Kind: "builtin-contract", Module: id, Reason: "captured runtime builtin identity; checked declaration, producer and effect contracts remain unproved"})
			s.view.Modules = append(s.view.Modules, record)
			continue
		}
		sourceModules++
		plan := snapshot.Plans.Modules[module.ID]
		if plan == nil || plan.Module != module.ID || plan.Format != module.Format || plan.File == nil || plan.File.IsDeclarationFile || snapshot.Program.Compiler.GetSourceFile(module.ID) != plan.File || snapshot.Program.Files[plan.File] != module.ID {
			return nil, fmt.Errorf("runtime source plan identity mismatch: %s", module.ID)
		}
		record.File = s.files[plan.File]
		s.view.Files[record.File-1].SelectedRuntime = true
		if len(plan.Initialization) != len(plan.File.Statements.Nodes) {
			return nil, fmt.Errorf("incomplete original module operations")
		}
		for ordinal, op := range plan.Initialization {
			if op.Node != plan.File.Statements.Nodes[ordinal] {
				return nil, fmt.Errorf("original module operation order changed")
			}
			if !s.validSource(op.Node, op.Source) {
				return nil, fmt.Errorf("initializer source mismatch")
			}
			record.Startup = append(record.Startup, graph.SourceStartupRecord{Ordinal: ordinal, Phase: op.Phase, Site: s.site(op.Node), Status: "source-retained-unproved"})
		}
		actualFunctions := map[*ast.Node]bool{}
		var visitFunctions func(*ast.Node) bool
		visitFunctions = func(n *ast.Node) bool {
			if n == nil {
				return false
			}
			if ast.IsFunctionLike(n) && n.Body() != nil {
				actualFunctions[n] = true
			}
			n.ForEachChild(visitFunctions)
			return false
		}
		visitFunctions(plan.File.AsNode())
		if len(actualFunctions) != len(plan.Functions) {
			return nil, fmt.Errorf("incomplete original module body templates")
		}
		seenFunctions := map[*ast.Node]bool{}
		for _, function := range plan.Functions {
			if !actualFunctions[function.Node] || seenFunctions[function.Node] {
				return nil, fmt.Errorf("template coverage mismatch")
			}
			seenFunctions[function.Node] = true
			if !s.validSource(function.Node, function.Declaration) || !s.validSource(function.Node.Body(), function.Body) {
				return nil, fmt.Errorf("template source mismatch")
			}
			tid := s.templates[function.Node]
			if tid == 0 {
				tid = graph.SourceTemplateID(len(s.view.Templates) + 1)
				s.templates[function.Node] = tid
				s.view.Templates = append(s.view.Templates, graph.SourceTemplateRecord{ID: tid, Module: id, Declaration: s.site(function.Node), Body: s.sourceSite(function.Body), Name: function.Name, BodyStatus: "unproved", InstanceStatus: "not-recovered"})
				s.view.Obligations = append(s.view.Obligations, graph.SourceRecoveryObligation{Kind: "body-effects", Module: id, Template: tid, Site: s.site(function.Node), Reason: "actual body retained; operation, call, value and effect proof pending"})
			}
			record.Templates = append(record.Templates, tid)
		}
		for _, hoisted := range plan.HoistedFunctions {
			id := s.templates[hoisted.Node]
			if id == 0 {
				return nil, fmt.Errorf("hoisted template omitted")
			}
			record.Hoisted = append(record.Hoisted, id)
		}
		s.view.Modules = append(s.view.Modules, record)
		s.view.Obligations = append(s.view.Obligations, graph.SourceRecoveryObligation{Kind: "startup", Module: id, Reason: "ordered source operations retained; normal startup, effects and native creation remain unproved"})
	}
	if len(snapshot.Plans.Modules) != sourceModules {
		return nil, fmt.Errorf("runtime source plan coverage mismatch")
	}
	c, done := snapshot.Program.Compiler.GetTypeChecker(context.Background())
	defer done()
	// Canonicalize original lexical declaration symbols before adding evaluated
	// environment cells. Type/property symbols are not callable/value premises.
	for _, file := range files {
		if file.IsDeclarationFile {
			continue
		}
		var visit func(*ast.Node) bool
		visit = func(n *ast.Node) bool {
			if n == nil {
				return false
			}
			if n.Kind == ast.KindIdentifier && ast.IsDeclarationName(n) && lexicalContext(n) {
				s.binding(n, c)
			}
			n.ForEachChild(visit)
			return false
		}
		visit(file.AsNode())
	}
	for _, imp := range snapshot.Plans.Imports {
		importer, target := s.modules[imp.Importer], s.modules[imp.Module]
		if importer == 0 || target == 0 || imp.CheckerLocal == nil {
			return nil, fmt.Errorf("import source/module identity omitted")
		}
		local := s.internSymbol(imp.CheckerLocal, importer)
		row := graph.SourceImportRecord{Importer: importer, Target: target, Local: local, Site: s.sourceSite(imp.Local), Requested: imp.Requested, Form: imp.Form, Capture: imp.Capture, Status: "export-binding-and-invocation-unproved"}
		for _, site := range imp.TypeDeclarations {
			row.TypeDeclarations = append(row.TypeDeclarations, s.sourceSite(site))
		}
		for _, site := range imp.CandidateBodies {
			row.CandidateBodies = append(row.CandidateBodies, s.sourceSite(site))
		}
		s.view.Imports = append(s.view.Imports, row)
	}
	for _, record := range s.view.Modules {
		if record.Format != "commonjs" {
			continue
		}
		for _, name := range []string{"exports", "module", "require", "__filename", "__dirname"} {
			binding := s.wrapper(record.ID, name)
			s.view.Cells = append(s.view.Cells, graph.SourceCellRecord{ID: graph.SourceCellID(len(s.view.Cells) + 1), Module: record.ID, Binding: binding, SourceModelIndex: -1, Role: "cjs-wrapper-parameter", Status: "runtime-instantiation-pending"})
		}
		s.view.Cells = append(s.view.Cells, graph.SourceCellRecord{ID: graph.SourceCellID(len(s.view.Cells) + 1), Module: record.ID, SourceModelIndex: -1, Role: "module-record-exports", Status: "runtime-instantiation-pending"})
		state := snapshot.States[record.Identity]
		if state == nil || state.Plan != snapshot.Plans.Modules[record.Identity] {
			return nil, fmt.Errorf("source module state identity mismatch")
		}
		envs := map[int]graph.SourceEnvironmentID{}
		for i, env := range state.Environments {
			if env == nil || env.ID != i {
				return nil, fmt.Errorf("invalid source environment index")
			}
			id := graph.SourceEnvironmentID(len(s.view.Environments) + 1)
			envs[env.ID] = id
			s.view.Environments = append(s.view.Environments, graph.SourceEnvironmentRecord{ID: id, Module: record.ID, SourceModelIndex: i, Creation: s.sourceSite(env.Creation), Status: "source-modeled-native-pending"})
		}
		cellIDs := map[int]graph.SourceCellID{}
		for _, env := range state.Environments {
			eid := envs[env.ID]
			if env.Parent >= 0 {
				if envs[env.Parent] == 0 {
					return nil, fmt.Errorf("missing parent environment")
				}
				s.view.Environments[eid-1].Parent = envs[env.Parent]
			}
			for _, index := range env.Cells {
				if index < 0 || index >= len(state.Cells) || state.Cells[index] == nil || state.Cells[index].ID != index {
					return nil, fmt.Errorf("invalid source cell index")
				}
				cell := state.Cells[index]
				binding := s.internSymbol(cell.Symbol, record.ID)
				if binding == 0 {
					return nil, fmt.Errorf("source cell has no lexical symbol")
				}
				if cellIDs[index] != 0 {
					return nil, fmt.Errorf("source cell belongs to multiple environments")
				}
				if s.view.Bindings[binding-1].Role == "cjs-wrapper-parameter" {
					for i := range s.view.Cells {
						target := &s.view.Cells[i]
						if target.Module == record.ID && target.Binding == binding && target.Role == "cjs-wrapper-parameter" {
							if target.SourceModelIndex != -1 {
								return nil, fmt.Errorf("duplicate wrapper parameter cell")
							}
							target.Environment = eid
							target.SourceModelIndex = index
							cellIDs[index] = target.ID
							break
						}
					}
					if cellIDs[index] == 0 {
						return nil, fmt.Errorf("missing wrapper parameter cell")
					}
				} else {
					cellIDs[index] = graph.SourceCellID(len(s.view.Cells) + 1)
					s.view.Cells = append(s.view.Cells, graph.SourceCellRecord{ID: cellIDs[index], Module: record.ID, Environment: eid, Binding: binding, SourceModelIndex: index, Role: "lexical-environment-cell", Status: "source-modeled-native-pending"})
				}
			}
		}
		if len(cellIDs) != len(state.Cells) {
			return nil, fmt.Errorf("source environment cell omitted")
		}
		for i, effect := range state.Effects {
			row := graph.SourceModeledEffect{Module: record.ID, Ordinal: i, Kind: effect.Kind, Site: s.sourceSite(effect.Source), Environment: envs[effect.Environment], Status: "source-model-operation-native-pending"}
			if effect.Cell != nil {
				row.Cell = cellIDs[*effect.Cell]
				if row.Cell == 0 {
					return nil, fmt.Errorf("source effect references absent cell")
				}
			}
			s.view.ModeledEffects = append(s.view.ModeledEffects, row)
		}
		for i, instance := range state.Instances {
			tid := s.templates[instance.Template.Node]
			eid := envs[instance.Environment]
			if instance.ID != i || tid == 0 || eid == 0 || !s.validSource(instance.Template.Node, instance.Template.Declaration) {
				return nil, fmt.Errorf("source instance identity mismatch")
			}
			id := graph.SourceInstanceID(len(s.view.Instances) + 1)
			s.view.Instances = append(s.view.Instances, graph.SourceInstanceRecord{ID: id, Module: record.ID, SourceModelIndex: i, Template: tid, Environment: eid, Strict: instance.Strict, Status: "source-modeled-native-pending"})
			s.view.Templates[tid-1].InstanceStatus = "source-modeled-native-pending"
			s.view.Obligations = append(s.view.Obligations, graph.SourceRecoveryObligation{Kind: "callable-instance", Module: record.ID, Template: tid, Instance: id, Reason: "source instance/environment identity is not runtime allocation, call-domain or body proof"})
		}
		for _, obligation := range state.Obligations {
			s.view.Obligations = append(s.view.Obligations, graph.SourceRecoveryObligation{Kind: "source-state", Module: record.ID, Site: s.sourceSite(obligation.Source), Reason: obligation.Reason})
		}
	}
	var e error
	s.integrity = snapshot.syntax.integrity
	e = s.validateSourceSyntax()
	if e != nil {
		return nil, e
	}
	return s, nil
}
func (s *SourceRecoveryScope) validSource(n *ast.Node, source SourceNodeID) bool {
	return n != nil && s.nodes[n] && sourceNodeID(ast.GetSourceFileOfNode(n), n) == source
}
func (s *SourceRecoveryScope) site(n *ast.Node) graph.SourceSite {
	return s.sourceSite(sourceNodeID(ast.GetSourceFileOfNode(n), n))
}
func (s *SourceRecoveryScope) sourceSite(source SourceNodeID) graph.SourceSite {
	return graph.SourceSite{File: s.files[s.snapshot.Program.Compiler.GetSourceFile(source.Module)], Start: source.Start, End: source.End, Kind: source.Kind}
}
func (s *SourceRecoveryScope) wrapper(module graph.SourceModuleID, name string) graph.SourceLexicalID {
	if s.wrappers[module] == nil {
		s.wrappers[module] = map[string]graph.SourceLexicalID{}
	}
	if id := s.wrappers[module][name]; id != 0 {
		return id
	}
	id := graph.SourceLexicalID(len(s.view.Bindings) + 1)
	s.wrappers[module][name] = id
	s.view.Bindings = append(s.view.Bindings, graph.SourceLexicalRecord{ID: id, Module: module, Name: name, Role: "cjs-wrapper-parameter"})
	s.view.Revision++
	return id
}
func (s *SourceRecoveryScope) internSymbol(symbol *ast.Symbol, module graph.SourceModuleID) graph.SourceLexicalID {
	if symbol == nil {
		return 0
	}
	if id := s.bindings[symbol]; id != 0 {
		return id
	}
	id := graph.SourceLexicalID(len(s.view.Bindings) + 1)
	s.bindings[symbol] = id
	record := graph.SourceLexicalRecord{ID: id, Module: module, Name: symbol.Name, Role: "lexical-value-symbol"}
	for _, declaration := range symbol.Declarations {
		if !s.nodes[declaration] {
			continue
		}
		did := s.declarations[declaration]
		if did == 0 {
			did = graph.SourceDeclarationID(len(s.view.Declarations) + 1)
			s.declarations[declaration] = did
			s.view.Declarations = append(s.view.Declarations, graph.SourceDeclarationRecord{ID: did, Site: s.site(declaration), Binding: id})
		}
		record.Declarations = append(record.Declarations, did)
	}
	s.view.Bindings = append(s.view.Bindings, record)
	s.view.Revision++
	return id
}
func (s *SourceRecoveryScope) binding(n *ast.Node, c *checker.Checker) graph.SourceLexicalID {
	file := ast.GetSourceFileOfNode(n)
	module := s.modules[s.snapshot.Program.Files[file]]
	symbol := lexicalSymbol(n, c)
	if n.Parent != nil && n.Parent.Kind == ast.KindShorthandPropertyAssignment {
		symbol = c.GetShorthandAssignmentValueSymbol(n.Parent)
	}
	if module != 0 && s.view.Modules[module-1].Format == "commonjs" && n.Kind == ast.KindIdentifier {
		name, ambiguous := bodyWrapperBinding(file, n, c)
		if ambiguous && symbol != nil {
			// Top-level var/function redeclarations belong to the wrapper parameter
			// binding; the initialized value and operation order remain unproved.
			for _, d := range symbol.Declarations {
				if ast.GetSourceFileOfNode(d) == file && (cjsWrapperVarDeclaration(d) != nil || d.Kind == ast.KindFunctionDeclaration && d.Parent == file.AsNode()) {
					name = n.Text()
					break
				}
			}
		}
		if name != "" {
			id := s.wrapper(module, name)
			if symbol != nil {
				for _, d := range symbol.Declarations {
					if ast.GetSourceFileOfNode(d) != file || d == file.AsNode() || !s.nodes[d] {
						continue
					}
					s.bindings[symbol] = id
					if s.declarations[d] == 0 {
						did := graph.SourceDeclarationID(len(s.view.Declarations) + 1)
						s.declarations[d] = did
						s.view.Declarations = append(s.view.Declarations, graph.SourceDeclarationRecord{ID: did, Site: s.site(d), Binding: id})
						s.view.Bindings[id-1].Declarations = append(s.view.Bindings[id-1].Declarations, did)
						s.view.Revision++
					}
				}
			}
			return id
		}
	}
	return s.internSymbol(symbol, module)
}
func (s *SourceRecoveryScope) Binding(node *ast.Node) (ScopedBinding, error) {
	if e := s.ValidateSourceCoverage(); e != nil {
		return ScopedBinding{}, e
	}
	if node == nil || !s.nodes[node] || !lexicalContext(node) {
		return ScopedBinding{}, fmt.Errorf("actual identifier from this source scope required")
	}
	if p := node.Parent; p != nil && p.Kind == ast.KindPropertyAccessExpression && p.AsPropertyAccessExpression().Name() == node {
		return ScopedBinding{}, fmt.Errorf("property name is not a lexical binding")
	}
	c, done := s.snapshot.Program.Compiler.GetTypeChecker(context.Background())
	defer done()
	id := s.binding(node, c)
	if id == 0 {
		return ScopedBinding{}, fmt.Errorf("identifier has no resolved lexical binding")
	}
	return ScopedBinding{s, id}, nil
}
func (s *SourceRecoveryScope) BindingID(handle ScopedBinding) (graph.SourceLexicalID, error) {
	if e := s.ValidateSourceCoverage(); e != nil {
		return 0, e
	}
	if handle.owner != s || handle.id == 0 {
		return 0, fmt.Errorf("foreign source scope binding")
	}
	return handle.id, nil
}
func (s *SourceRecoveryScope) View() graph.SourceRegistryView {
	// Plain records are copied deeply; callers cannot modify registry state.
	bytes, err := json.Marshal(s.view)
	if err != nil {
		panic(err)
	}
	var out graph.SourceRegistryView
	if err = json.Unmarshal(bytes, &out); err != nil {
		panic(err)
	}
	return out
}
