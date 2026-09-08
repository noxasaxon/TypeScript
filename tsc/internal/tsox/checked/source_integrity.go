package checked

import (
	"fmt"
	"github.com/microsoft/typescript-go/internal/compiler"
	"reflect"
	"strings"

	"github.com/microsoft/typescript-go/internal/ast"
)

// sourceSyntaxIntegrity retains original field locations as read-only reflection
// values. It observes syntax, not checker results. No unsafe access or writable
// reflection is used. Capturing field locations also detects replacement of a
// parent/list/data pointer before following the old descendant fields.
type sourceSyntaxIntegrity struct {
	fields []sourceSyntaxField
	maps   []sourceSyntaxMap
}
type sourceSyntaxMap struct {
	value  reflect.Value
	digest [32]byte
}
type sourceSyntaxField struct {
	value   reflect.Value
	integer uint64
	text    string
	length  int
	dynamic reflect.Type
}

// sourceSyntaxSeal is issued only during the private reader's original capture,
// before its compiler Program is exposed. Source registration cannot rebaseline it.
type sourceSyntaxSeal struct {
	program   *compiler.Program
	files     []*ast.SourceFile
	integrity *sourceSyntaxIntegrity
}

func captureSourceProgramSyntax(program *compiler.Program) (*sourceSyntaxSeal, error) {
	if program == nil {
		return nil, fmt.Errorf("original compiler Program required")
	}
	for _, t := range []reflect.Type{reflect.TypeFor[ast.SourceFile](), reflect.TypeFor[ast.Node](), reflect.TypeFor[ast.NodeBase](), reflect.TypeFor[ast.NodeDefault](), reflect.TypeFor[ast.DeclarationBase](), reflect.TypeFor[ast.ExportableBase](), reflect.TypeFor[ast.LocalsContainerBase](), reflect.TypeFor[ast.FlowNodeBase](), reflect.TypeFor[ast.CompositeBase]()} {
		if err := requireSourceSyntaxSchema(t); err != nil {
			return nil, err
		}
	}
	out := &sourceSyntaxIntegrity{}
	seal := &sourceSyntaxSeal{program: program, files: append([]*ast.SourceFile(nil), program.GetSourceFiles()...), integrity: out}
	nodes := map[*ast.Node]bool{}
	var visit func(*ast.Node) bool
	visit = func(n *ast.Node) bool {
		if n == nil || nodes[n] {
			return false
		}
		nodes[n] = true
		n.ForEachChild(visit)
		return false
	}
	for _, file := range seal.files {
		v := reflect.ValueOf(file).Elem()
		// Every SourceFile field is classified by an exact, versioned schema. This
		// exclusion list contains binder/diagnostic/lazy caches, not parsed directives.
		for i := 0; i < v.NumField(); i++ {
			name := v.Type().Field(i).Name
			if sourceFileCache[name] {
				continue
			}
			if err := out.capture(v.Field(i)); err != nil {
				return nil, fmt.Errorf("SourceFile.%s: %w", name, err)
			}
		}
		visit(file.AsNode())
	}
	for node := range nodes {
		v := reflect.ValueOf(node).Elem()
		for _, name := range []string{"Kind", "Flags", "Loc", "Parent"} {
			if err := out.capture(v.FieldByName(name)); err != nil {
				return nil, err
			}
		}
		dataField := v.FieldByName("data")
		if dataField.IsNil() {
			return nil, fmt.Errorf("original syntax has no payload")
		}
		data := dataField.Elem()
		out.fields = append(out.fields, sourceSyntaxField{value: dataField, integer: uint64(data.Pointer()), dynamic: data.Type()})
		if node.Kind != ast.KindSourceFile {
			if err := out.capture(data.Elem()); err != nil {
				return nil, err
			}
		}
	}
	return seal, nil
}
func (s *sourceSyntaxSeal) validate(program *compiler.Program) error {
	if s == nil || program != s.program || s.integrity == nil {
		return fmt.Errorf("original captured syntax seal required")
	}
	files := program.GetSourceFiles()
	if len(files) != len(s.files) {
		return fmt.Errorf("compiler source inventory changed")
	}
	for i, f := range files {
		if f != s.files[i] {
			return fmt.Errorf("compiler source identity changed")
		}
	}
	return s.integrity.validate()
}
func sealSourceSnapshot(snapshot *CJSBodySourceSnapshot, syntax *sourceSyntaxSeal) error {
	if snapshot == nil || snapshot.Program == nil {
		return fmt.Errorf("source snapshot required")
	}
	if err := syntax.validate(snapshot.Program.Compiler); err != nil {
		return err
	}
	if snapshot.syntax != nil {
		return fmt.Errorf("source snapshot already sealed")
	}
	snapshot.syntax = syntax
	digest, err := sourceSnapshotDigest(snapshot)
	if err != nil {
		return err
	}
	snapshot.sourceDigest = digest
	return nil
}
func (s *CJSBodySourceSnapshot) validateSeal() error {
	if s == nil || s.Program == nil {
		return fmt.Errorf("source snapshot required")
	}
	if err := s.syntax.validate(s.Program.Compiler); err != nil {
		return err
	}
	digest, err := sourceSnapshotDigest(s)
	if err != nil || digest != s.sourceDigest {
		return fmt.Errorf("original source snapshot metadata changed")
	}
	return nil
}
func (s *SourceRecoveryScope) validateSourceSyntax() error {
	if s == nil {
		return fmt.Errorf("original source scope required")
	}
	return s.snapshot.validateSeal()
}

// Exact schemas make cache exclusion fail closed when upstream adds/changes a
// field. Other syntax payloads are traversed completely, including new fields.
var sourceSyntaxSchemas = map[string]string{
	"SourceFile":          `NodeBase:ast.NodeBase;DeclarationBase:ast.DeclarationBase;LocalsContainerBase:ast.LocalsContainerBase;CompositeBase:ast.CompositeBase;fileName:string;parseOptions:ast.SourceFileParseOptions;text:string;Statements:*ast.NodeList;EndOfFileToken:*ast.Node;dataMu:sync.Mutex;data:map[ast.sourceFileDataKey]interface {};diagnostics:[]*ast.Diagnostic;jsDiagnostics:[]*ast.Diagnostic;jsdocDiagnostics:[]*ast.Diagnostic;LanguageVariant:core.LanguageVariant;ScriptKind:core.ScriptKind;IsDeclarationFile:bool;ContainsNonASCII:bool;UsesUriStyleNodeCoreModules:core.Tristate;Identifiers:map[string]string;IdentifierCount:int;imports:[]*ast.Node;ModuleAugmentations:[]*ast.Node;AmbientModuleNames:[]string;CommentDirectives:[]ast.CommentDirective;jsdocCache:map[*ast.Node][]*ast.Node;jsdocMu:sync.RWMutex;hasLazyJSDoc:bool;ReparsedClones:[]*ast.Node;Pragmas:[]ast.Pragma;ReferencedFiles:[]*ast.FileReference;TypeReferenceDirectives:[]*ast.FileReference;LibReferenceDirectives:[]*ast.FileReference;CheckJsDirective:*ast.CheckJsDirective;NodeCount:int;TextCount:int;CommonJSModuleIndicator:*ast.Node;ExternalModuleIndicator:*ast.Node;isBound:atomic.Bool;bindOnce:sync.Once;bindDiagnostics:[]*ast.Diagnostic;BindSuggestionDiagnostics:[]*ast.Diagnostic;EndFlowNode:*ast.FlowNode;SymbolCount:int;ClassifiableNames:collections.Set[string];PatternAmbientModules:[]*ast.PatternAmbientModule;GlobalExports:ast.SymbolTable;ecmaLineMapMu:sync.RWMutex;ecmaLineMap:[]core.TextPos;Hash:xxh3.Uint128;tokenCacheMu:sync.Mutex;tokenCache:map[ast.TokenCacheKey]*ast.Node;tokenFactory:*ast.NodeFactory;declarationMapMu:sync.Mutex;declarationMap:map[string][]*ast.Node;nameTableOnce:sync.Once;nameTable:map[string]int;positionMapOnce:sync.Once;positionMap:*ast.PositionMap;`,
	"Node":                `Kind:ast.Kind;Flags:ast.NodeFlags;Loc:core.TextRange;id:atomic.Uint64;Parent:*ast.Node;data:ast.nodeData;`,
	"NodeBase":            `NodeDefault:ast.NodeDefault;`,
	"NodeDefault":         `Node:ast.Node;`,
	"DeclarationBase":     `Symbol:*ast.Symbol;`,
	"ExportableBase":      `LocalSymbol:*ast.Symbol;`,
	"LocalsContainerBase": `Locals:ast.SymbolTable;NextContainer:*ast.Node;`,
	"FlowNodeBase":        `FlowNode:*ast.FlowNode;`,
	"CompositeBase":       `facts:atomic.Uint32;`,
}

func requireSourceSyntaxSchema(t reflect.Type) error {
	var b strings.Builder
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		fmt.Fprintf(&b, "%s:%s;", f.Name, f.Type)
	}
	expected, ok := sourceSyntaxSchemas[t.Name()]
	if !ok || expected != b.String() {
		return fmt.Errorf("unclassified source syntax schema %s", t)
	}
	return nil
}

var sourceFileCache = map[string]bool{
	"dataMu": true, "data": true, "diagnostics": true, "jsDiagnostics": true, "jsdocDiagnostics": true,
	"imports": true, "ModuleAugmentations": true, "AmbientModuleNames": true,
	"jsdocCache": true, "jsdocMu": true, "hasLazyJSDoc": true, "ReparsedClones": true,
	"isBound": true, "bindOnce": true, "bindDiagnostics": true, "BindSuggestionDiagnostics": true,
	"EndFlowNode": true, "SymbolCount": true, "ClassifiableNames": true, "PatternAmbientModules": true, "GlobalExports": true,
	"ecmaLineMapMu": true, "ecmaLineMap": true, "tokenCacheMu": true, "tokenCache": true, "tokenFactory": true,
	"declarationMapMu": true, "declarationMap": true, "nameTableOnce": true, "nameTable": true, "positionMapOnce": true, "positionMap": true,
}

func (out *sourceSyntaxIntegrity) capture(v reflect.Value) error {
	if !v.IsValid() {
		return fmt.Errorf("missing source syntax field")
	}
	t := v.Type()
	// These upstream bases contain only binder/flow/subtree caches. NodeBase is
	// captured through its canonical *Node above, avoiding the embedded cycle.
	if t.PkgPath() == "github.com/microsoft/typescript-go/internal/ast" {
		switch t.Name() {
		case "NodeBase", "NodeDefault", "DeclarationBase", "ExportableBase", "LocalsContainerBase", "FlowNodeBase", "CompositeBase":
			return requireSourceSyntaxSchema(t)
		}
	}
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			// These are explicitly checker-created flow links, never AST child links.
			if field.Type() == reflect.TypeFor[*ast.FlowNode]() {
				key := t.Name() + "." + t.Field(i).Name
				switch key {
				case "BodyBase.EndFlowNode", "CaseOrDefaultClause.FallthroughFlowNode", "FunctionDeclaration.ReturnFlowNode", "ConstructorDeclaration.ReturnFlowNode", "ClassStaticBlockDeclaration.ReturnFlowNode", "FunctionExpression.ReturnFlowNode":
					continue
				default:
					return fmt.Errorf("unclassified flow-cache field %s", key)
				}
			}
			if err := out.capture(field); err != nil {
				return fmt.Errorf("source syntax %s.%s: %w", t, t.Field(i).Name, err)
			}
		}
	case reflect.Pointer:
		out.fields = append(out.fields, sourceSyntaxField{value: v, integer: uint64(v.Pointer())})
		if !v.IsNil() && t != reflect.TypeFor[*ast.Node]() {
			if err := out.capture(v.Elem()); err != nil {
				return err
			}
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := out.capture(v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		digest, err := sourceMetadataDigest(v)
		if err != nil {
			return err
		}
		out.maps = append(out.maps, sourceSyntaxMap{v, digest})
	case reflect.Slice:
		// Retain the list header and each original entry. A same-text foreign node or
		// reordered startup/parameter/property list is not an original source member.
		out.fields = append(out.fields, sourceSyntaxField{value: v, integer: uint64(v.Pointer()), length: v.Len()})
		for i := 0; i < v.Len(); i++ {
			if err := out.capture(v.Index(i)); err != nil {
				return err
			}
		}
	case reflect.Bool:
		n := uint64(0)
		if v.Bool() {
			n = 1
		}
		out.fields = append(out.fields, sourceSyntaxField{value: v, integer: n})
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		out.fields = append(out.fields, sourceSyntaxField{value: v, integer: uint64(v.Int())})
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		out.fields = append(out.fields, sourceSyntaxField{value: v, integer: v.Uint()})
	case reflect.String:
		out.fields = append(out.fields, sourceSyntaxField{value: v, text: v.String()})
	default:
		return fmt.Errorf("unclassified syntax payload %s", t)
	}
	return nil
}

func (s *sourceSyntaxIntegrity) validate() error {
	if s == nil {
		return fmt.Errorf("original source syntax authority required")
	}
	for _, field := range s.fields {
		v := field.value
		same := false
		switch v.Kind() {
		case reflect.Interface:
			same = !v.IsNil() && v.Elem().Type() == field.dynamic && uint64(v.Elem().Pointer()) == field.integer
		case reflect.Pointer:
			same = uint64(v.Pointer()) == field.integer
		case reflect.Slice:
			same = v.Len() == field.length && uint64(v.Pointer()) == field.integer
		case reflect.String:
			same = v.String() == field.text
		case reflect.Bool:
			same = v.Bool() == (field.integer != 0)
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			same = uint64(v.Int()) == field.integer
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			same = v.Uint() == field.integer
		}
		if !same {
			return fmt.Errorf("original source syntax changed (%s)", v.Type())
		}
	}
	for _, m := range s.maps {
		digest, err := sourceMetadataDigest(m.value)
		if err != nil || digest != m.digest {
			return fmt.Errorf("original source syntax map changed")
		}
	}

	return nil
}
