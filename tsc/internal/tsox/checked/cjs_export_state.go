package checked

import (
	"fmt"
	"slices"

	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/internal/checker"
)

// CJSExportBoundary is an independently proved incoming realm property. It is
// not inferred from a module name or from a syntactically fresh exports object.
// A preceding module may install an inherited setter before this module runs.
type CJSExportBoundary struct {
	PristineObjectPrototype bool
	// Actual incoming realm facts, never inferred from source spelling/type.
	IntrinsicDefineProperty          bool
	DescriptorPrototypeClean         bool
	IntrinsicObjectCreate            bool
	IntrinsicObjectPrototypeToString bool
}
type CJSStateValue struct {
	Kind      string
	Instance  int
	Intrinsic string
	Object    int
	Function  *SourceNodeID
	Literal   *SourceNodeID
}
type CJSStateObject struct {
	Site        SourceNodeID
	Own         map[string]CJSStateValue
	Descriptors map[string]CJSDataAttributes
	Prototype   string
}
type CJSStateObligation struct {
	Source SourceNodeID
	Reason string
}
type CJSExportStep struct {
	Source       SourceNodeID
	Complete     bool
	RecordExport CJSStateValue
}

// This is finite source analysis, not an implementation of JavaScript values.
// Unknown operations stop proof; the entire original initialization plan remains
// attached. A prefix result never certifies final exports or startup admission.
type CJSExportState struct {
	Plan         *ModuleExportPlan `json:"-"`
	Boundary     CJSExportBoundary
	Objects      []CJSStateObject
	Steps        []CJSExportStep
	Obligations  []CJSStateObligation
	Effects      []CJSStartupEffect
	Environments []*CJSStartupEnvironment
	Cells        []*CJSStartupCell
	Instances    []CJSStartupCallable
	Invocations  []CJSStartupInvocation
	Complete     bool
	RecordExport CJSStateValue
}
type cjsStateBuilder struct {
	out             *CJSExportState
	checker         *checker.Checker
	env             *CJSStartupEnvironment
	active          map[*ast.Node]bool
	instantiating   bool
	exports, module CJSStateValue
}

func (b *cjsStateBuilder) fail(n *ast.Node, reason string) CJSStateValue {
	b.out.Obligations = append(b.out.Obligations, CJSStateObligation{sourceNodeID(b.out.Plan.File, n), reason})
	b.out.Complete = false
	return CJSStateValue{Kind: "unknown"}
}
func (b *cjsStateBuilder) object(n *ast.Node) CJSStateValue {
	id := len(b.out.Objects)
	b.out.Objects = append(b.out.Objects, CJSStateObject{Site: sourceNodeID(b.out.Plan.File, n), Own: map[string]CJSStateValue{}, Descriptors: map[string]CJSDataAttributes{}})
	return CJSStateValue{Kind: "object", Object: id}
}
func (b *cjsStateBuilder) function(n *ast.Node) CJSStateValue { return b.callable(n) }
func (b *cjsStateBuilder) property(receiver CJSStateValue, key string, n *ast.Node) CJSStateValue {
	if receiver.Kind == "function" {
		receiver = b.functionObject(receiver, n)
	}
	if receiver.Kind != "object" {
		return b.fail(n, "property read requires an actual ordinary object/descriptor proof")
	}
	if value, ok := b.out.Objects[receiver.Object].Own[key]; ok {
		return value
	}
	return b.fail(n, "missing own property requires an inherited lookup contract")
}
func (b *cjsStateBuilder) write(receiver CJSStateValue, key string, value CJSStateValue, n *ast.Node) {
	if receiver.Kind == "function" {
		receiver = b.functionObject(receiver, n)
	}
	if receiver.Kind != "object" {
		b.fail(n, "property write requires an actual ordinary object/descriptor proof")
		return
	}
	if attributes, exists := b.out.Objects[receiver.Object].Descriptors[key]; exists && !attributes.Writable {
		if b.env.Strict {
			b.fail(n, "strict write to nonwritable own data property throws")
		}
		return
	}
	if _, own := b.out.Objects[receiver.Object].Own[key]; !own && !b.out.Boundary.PristineObjectPrototype {
		b.fail(n, "new property write requires incoming inherited-descriptor proof")
		return
	}
	if key == "__proto__" {
		b.fail(n, "prototype setter is not a data property write")
		return
	}
	b.out.Effects = append(b.out.Effects, CJSStartupEffect{Source: sourceNodeID(b.out.Plan.File, n), Kind: "write-own-data-property", Environment: b.env.ID, Target: receiver, Value: value, Key: key, NeedsNativeExecution: true})
	b.out.Objects[receiver.Object].Own[key] = value
	if _, exists := b.out.Objects[receiver.Object].Descriptors[key]; !exists {
		b.out.Objects[receiver.Object].Descriptors[key] = CJSDataAttributes{true, true, true}
	}
}

type cjsStatePlace struct {
	local   *CJSStartupCell
	wrapper string
	object  CJSStateValue
	key     string
}

func (b *cjsStateBuilder) place(n *ast.Node) cjsStatePlace {
	if n.Kind == ast.KindIdentifier {
		if wrapper := cjsWrapperParameter(b.out.Plan.File, n, b.checker); wrapper != "" {
			return cjsStatePlace{wrapper: wrapper}
		}
		symbol := b.checker.GetSymbolAtLocation(n)
		if cell := b.lookup(symbol); cell != nil {
			return cjsStatePlace{local: cell}
		}
		b.fail(n, "assignment target has no initialized local/wrapper binding")
		return cjsStatePlace{}
	}
	if n.Kind == ast.KindPropertyAccessExpression {
		x := n.AsPropertyAccessExpression()
		return cjsStatePlace{object: b.value(x.Expression), key: x.Name().Text()}
	}
	if n.Kind == ast.KindElementAccessExpression {
		x := n.AsElementAccessExpression()
		receiver := b.value(x.Expression)
		if x.ArgumentExpression.Kind != ast.KindStringLiteral {
			b.fail(n, "computed property key needs evaluated key semantics")
			return cjsStatePlace{}
		}
		return cjsStatePlace{object: receiver, key: x.ArgumentExpression.Text()}
	}
	b.fail(n, "assignment target needs ordinary ordered source lowering")
	return cjsStatePlace{}
}
func (b *cjsStateBuilder) assign(place cjsStatePlace, value CJSStateValue, n *ast.Node) {
	switch {
	case place.wrapper == "exports":
		b.exports = value
	case place.wrapper == "module":
		b.module = value
	case place.local != nil:
		if place.local.Immutable {
			b.fail(n, "assignment to immutable local throws before export settlement")
			return
		}
		place.local.Value = value
		b.out.Effects = append(b.out.Effects, CJSStartupEffect{Source: sourceNodeID(b.out.Plan.File, n), Kind: "write-binding-cell", Environment: b.env.ID, Cell: &place.local.ID, Value: value, NeedsNativeExecution: true})
	case place.object.Kind != "":
		b.write(place.object, place.key, value, n)
	}
}
func (b *cjsStateBuilder) value(n *ast.Node) CJSStateValue {
	if n == nil {
		return CJSStateValue{Kind: "undefined"}
	}
	switch n.Kind {
	case ast.KindCallExpression:
		return b.startupCall(n)
	case ast.KindParenthesizedExpression:
		return b.value(n.AsParenthesizedExpression().Expression)
	case ast.KindIdentifier:
		switch cjsWrapperParameter(b.out.Plan.File, n, b.checker) {
		case "exports":
			return b.exports
		case "module":
			return b.module
		}
		if cell := b.lookup(b.checker.GetSymbolAtLocation(n)); cell != nil {
			return cell.Value
		}
		return b.fail(n, "identifier value needs an actual initialized runtime binding")
	case ast.KindStringLiteral, ast.KindNumericLiteral, ast.KindTrueKeyword, ast.KindFalseKeyword, ast.KindNullKeyword:
		site := sourceNodeID(b.out.Plan.File, n)
		return CJSStateValue{Kind: "literal", Literal: &site}
	case ast.KindRegularExpressionLiteral:
		// Literal evaluation creates a fresh intrinsic RegExp. Its pattern,
		// flags and allocation remain an original executable source operation;
		// no property/call behavior is inferred for this opaque value.
		site := sourceNodeID(b.out.Plan.File, n)
		b.out.Effects = append(b.out.Effects, CJSStartupEffect{Source: site, Kind: "create-regexp", Environment: b.env.ID, NeedsNativeExecution: true})
		return CJSStateValue{Kind: "regexp", Literal: &site}
	case ast.KindFunctionExpression, ast.KindArrowFunction:
		return b.function(n)
	case ast.KindObjectLiteralExpression:
		object := b.object(n)
		for _, property := range n.AsObjectLiteralExpression().Properties.Nodes {
			if property.Kind != ast.KindPropertyAssignment {
				return b.fail(property, "object member requires ordered descriptor/spread/accessor lowering")
			}
			x := property.AsPropertyAssignment()
			key := x.Name()
			if key == nil || key.Kind != ast.KindIdentifier && key.Kind != ast.KindStringLiteral || key.Text() == "__proto__" {
				return b.fail(property, "object key needs evaluated/prototype semantics")
			}
			value := b.value(x.Initializer)
			// Object literal data definitions create own data properties, without
			// invoking inherited setters. Every initializer is still evaluated in order.
			b.out.Objects[object.Object].Own[key.Text()] = value
		}
		return object
	case ast.KindPropertyAccessExpression:
		if value, handled := b.intrinsicCapture(n); handled {
			return value
		}
		x := n.AsPropertyAccessExpression()
		return b.property(b.value(x.Expression), x.Name().Text(), n)
	case ast.KindElementAccessExpression:
		x := n.AsElementAccessExpression()
		receiver := b.value(x.Expression)
		if x.ArgumentExpression.Kind != ast.KindStringLiteral {
			return b.fail(n, "computed key needs evaluated key semantics")
		}
		return b.property(receiver, x.ArgumentExpression.Text(), n)
	case ast.KindBinaryExpression:
		x := n.AsBinaryExpression()
		if x.OperatorToken.Kind != ast.KindEqualsToken {
			return b.fail(n, "operator requires ordinary value/effect lowering")
		}
		// Capture the original LHS reference before evaluating a possibly rebinding
		// RHS. module.exports = (module = ...) must not target the new wrapper value.
		place := b.place(x.Left)
		value := b.value(x.Right)
		b.assign(place, value, n)
		return value
	default:
		return b.fail(n, "operation requires actual body/effect/normal-exit evidence")
	}
}
func analyzeCJSExportState(plan *ModuleExportPlan, c *checker.Checker, boundary CJSExportBoundary) (*CJSExportState, error) {
	if plan == nil || plan.Format != "commonjs" || plan.File == nil || plan.File.IsDeclarationFile {
		return nil, fmt.Errorf("CJS state requires actual commonjs source plan")
	}
	out := &CJSExportState{Plan: plan, Boundary: boundary, Complete: true}
	b := &cjsStateBuilder{out: out, checker: c, active: map[*ast.Node]bool{}}
	b.env = b.environment(nil, plan.File.AsNode())
	b.exports = b.object(plan.File.AsNode())
	b.module = b.object(plan.File.AsNode())
	record := b.module.Object // Persistent loader ModuleRecord: wrapper rebinding cannot replace it.
	out.Objects[record].Own["exports"] = b.exports
	b.instantiate(plan.File.Statements.Nodes)
	for _, operation := range plan.Initialization {
		if out.Complete {
			b.startupStatement(operation.Node)
		}
		out.RecordExport = out.Objects[record].Own["exports"]
		out.Steps = append(out.Steps, CJSExportStep{operation.Source, out.Complete, out.RecordExport})
	}
	return out, nil
}

// FinalOwnExport does not certify Node's lexical named-export discovery. That
// independent linking fact must hold before a named import can be instantiated.
func (s *CJSExportState) FinalOwnExport(name string) (CJSStateValue, bool) {
	if !s.Complete {
		return CJSStateValue{}, false
	}
	if name == "default" || name == "module.exports" {
		return s.RecordExport, true
	}
	if s.RecordExport.Kind != "object" {
		// Functions and primitives have their own intrinsic properties (name,
		// length, string indices, ...). A value identity is not their descriptor
		// proof; never turn an unmodeled own property into undefined.
		return CJSStateValue{}, false
	}
	if value, ok := s.Objects[s.RecordExport.Object].Own[name]; ok {
		return value, true
	}
	return CJSStateValue{Kind: "undefined"}, true
}

// ActualCallable is a final captured value identity only. Its body, closure,
// defaults, receiver and runtime effects still require ordinary function proof.
func (s *CJSExportState) ActualCallable(name string) (FunctionSource, bool) {
	value, ok := s.FinalOwnExport(name)
	if !ok || value.Kind != "function" || value.Function == nil {
		return FunctionSource{}, false
	}
	for _, f := range s.Plan.Functions {
		if f.Body == *value.Function {
			return f, true
		}
	}
	return FunctionSource{}, false
}
func (s *CJSExportState) PendingOperations() []SourceOperation {
	return slices.Clone(s.Plan.Initialization)
}
