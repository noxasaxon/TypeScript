package checked

import (
	"path/filepath"
	"strings"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/bundled"
)

// These finite attributes describe actual own data properties. Unknown/accessor
// descriptors are an obligation, never silently converted into data storage.
type CJSDataAttributes struct{ Writable, Enumerable, Configurable bool }
type CJSStartupEffect struct {
	Source           SourceNodeID
	Kind             string
	Environment      int
	Cell             *int
	Callee, Receiver SourceNodeID
	Arguments        []SourceNodeID
	Target           CJSStateValue
	Value            CJSStateValue
	Key              string
	Attributes       CJSDataAttributes
	// This source transfer is neither executable startup nor a no-allocation proof.
	NeedsNativeExecution bool
}

func cjsBundledSymbol(symbol *ast.Symbol) bool {
	if symbol == nil || len(symbol.Declarations) == 0 {
		return false
	}
	for _, declaration := range symbol.Declarations {
		file := ast.GetSourceFileOfNode(declaration)
		if file == nil || !file.IsDeclarationFile || filepath.Clean(filepath.Dir(file.FileName())) != filepath.Clean(bundled.LibPath()) || !strings.HasPrefix(filepath.Base(file.FileName()), "lib.") {
			return false
		}
	}
	return true
}
func (b *cjsStateBuilder) definePropertyCall(n *ast.Node) CJSStateValue {
	call := n.AsCallExpression()
	if call.Expression.Kind != ast.KindPropertyAccessExpression || call.Arguments == nil || len(call.Arguments.Nodes) != 3 {
		return b.fail(n, "call requires actual callable-instance/body/effect proof")
	}
	property := call.Expression.AsPropertyAccessExpression()
	if property.Expression.Kind != ast.KindIdentifier || property.Expression.Text() != "Object" || property.Name().Text() != "defineProperty" || !cjsBundledSymbol(b.checker.GetSymbolAtLocation(property.Expression)) || !cjsBundledSymbol(b.checker.GetSymbolAtLocation(property.Name())) {
		return b.fail(n, "define-property operation lacks actual bundled intrinsic identity")
	}
	if !b.out.Boundary.IntrinsicDefineProperty {
		return b.fail(n, "define-property intrinsic requires incoming realm/callee preservation")
	}
	// The incoming intrinsic fact makes these callee and receiver evaluations
	// non-effectful. All three actual argument evaluations still occur in order.
	args := call.Arguments.Nodes
	target := b.value(args[0])
	key := b.value(args[1])
	descriptor := b.value(args[2])
	if !b.out.Complete {
		return CJSStateValue{Kind: "unknown"}
	}
	if target.Kind != "object" || descriptor.Kind != "object" {
		return b.fail(n, "define-property target/descriptor requires ordinary object proof")
	}
	if key.Kind != "literal" || args[1].Kind != ast.KindStringLiteral {
		return b.fail(args[1], "property key conversion requires evaluated scalar-key contract")
	}
	if !b.out.Boundary.DescriptorPrototypeClean {
		return b.fail(args[2], "descriptor HasProperty/Get requires inherited field absence proof")
	}
	fields := b.out.Objects[descriptor.Object].Own
	if _, exists := fields["get"]; exists {
		return b.fail(args[2], "accessor descriptor remains an actual descriptor operation")
	}
	if _, exists := fields["set"]; exists {
		return b.fail(args[2], "accessor descriptor remains an actual descriptor operation")
	}
	boolean := func(name string) (bool, bool) {
		v, exists := fields[name]
		if !exists {
			return false, true
		}
		if v.Kind != "literal" || v.Literal == nil {
			return false, false
		}
		switch v.Literal.Kind {
		case "KindTrueKeyword":
			return true, true
		case "KindFalseKeyword":
			return false, true
		}
		return false, false
	}
	writable, wok := boolean("writable")
	enumerable, eok := boolean("enumerable")
	configurable, cok := boolean("configurable")
	if !wok || !eok || !cok {
		return b.fail(args[2], "descriptor attribute requires actual ToBoolean value proof")
	}
	value, exists := fields["value"]
	if !exists {
		value = CJSStateValue{Kind: "undefined"}
	}
	name := args[1].Text()
	if _, exists := b.out.Objects[target.Object].Own[name]; exists {
		return b.fail(n, "descriptor redefinition needs ValidateAndApplyPropertyDescriptor proof")
	}
	attributes := CJSDataAttributes{writable, enumerable, configurable}
	b.out.Objects[target.Object].Own[name] = value
	b.out.Objects[target.Object].Descriptors[name] = attributes
	effect := CJSStartupEffect{Source: sourceNodeID(b.out.Plan.File, n), Kind: "define-own-data-property", Environment: b.env.ID, Callee: sourceNodeID(b.out.Plan.File, call.Expression), Receiver: sourceNodeID(b.out.Plan.File, property.Expression), Target: target, Key: name, Attributes: attributes, NeedsNativeExecution: true}
	for _, arg := range args {
		effect.Arguments = append(effect.Arguments, sourceNodeID(b.out.Plan.File, arg))
	}
	b.out.Effects = append(b.out.Effects, effect)
	return target
}

// This reference binds source code to its enclosing module environment contract.
// ProgramInstance must allocate the actual environment/cells. It is not a
// static unique runtime closure or proof that calls satisfy their body domains.
type ModuleCallableReference struct {
	Template                             FunctionSource
	Instance, Environment                int
	ModuleEnvironment                    string
	NeedsInstanceBinding, NeedsBodyProof bool
}

func (s *CJSExportState) CallableReference(name string) (ModuleCallableReference, bool) {
	f, ok := s.ActualCallable(name)
	if !ok {
		return ModuleCallableReference{}, false
	}
	value, _ := s.FinalOwnExport(name)
	return ModuleCallableReference{Template: f, Instance: value.Instance, Environment: s.Instances[value.Instance].Environment, ModuleEnvironment: s.Plan.Module, NeedsInstanceBinding: true, NeedsBodyProof: true}, true
}
