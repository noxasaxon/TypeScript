package checked

import (
	"fmt"
	"reflect"

	"github.com/microsoft/typescript-go/internal/ast"
)

// Derive expected effects from the original source nodes, not from Effects.
// The limited cookie prefix remains unresolved under the supplied realm.
func cookiePrefixEffects(plan *ModuleExportPlan, state *CJSExportState) error {
	if len(plan.Initialization) != 16 || len(plan.HoistedFunctions) != 6 {
		return fmt.Errorf("original source operations changed")
	}
	nodeID := func(n *ast.Node) SourceNodeID { return sourceNodeID(plan.File, n) }
	cellFor := func(n *ast.Node) (*int, error) {
		want := nodeID(n)
		found := -1
		for _, cell := range state.Cells {
			if cell.Binding == want {
				if found >= 0 {
					return nil, fmt.Errorf("duplicate source binding")
				}
				found = cell.ID
			}
		}
		if found < 0 {
			return nil, fmt.Errorf("original binding missing: %+v", want)
		}
		return &found, nil
	}
	functionValue := func(fn FunctionSource) (CJSStateValue, error) {
		found := -1
		for _, instance := range state.Instances {
			if instance.Template.Body == fn.Body && instance.Environment == 0 {
				if found >= 0 {
					return CJSStateValue{}, fmt.Errorf("ambiguous original function instance")
				}
				found = instance.ID
			}
		}
		if found < 0 {
			return CJSStateValue{}, fmt.Errorf("original function instance missing")
		}
		body := fn.Body
		return CJSStateValue{Kind: "function", Instance: found, Function: &body}, nil
	}
	var expected []CJSStartupEffect
	for _, fn := range plan.HoistedFunctions {
		name := fn.Node.Name()
		cell, err := cellFor(name)
		if err != nil {
			return err
		}
		value, err := functionValue(fn)
		if err != nil {
			return err
		}
		expected = append(expected, CJSStartupEffect{Source: nodeID(name), Kind: "instantiate-binding-cell", Cell: cell, Value: value, NeedsNativeExecution: true})
	}
	callNode := plan.Initialization[1].Node.AsExpressionStatement().Expression
	call := callNode.AsCallExpression()
	property := call.Expression.AsPropertyAccessExpression()
	args := call.Arguments.Nodes
	markerValue := nodeID(args[2].AsObjectLiteralExpression().Properties.Nodes[0].AsPropertyAssignment().Initializer)
	var arguments []SourceNodeID
	for _, n := range args {
		arguments = append(arguments, nodeID(n))
	}
	expected = append(expected, CJSStartupEffect{Source: nodeID(callNode), Kind: "define-own-data-property", Callee: nodeID(call.Expression), Receiver: nodeID(property.Expression), Arguments: arguments, Target: CJSStateValue{Kind: "object", Object: 0}, Key: "__esModule", NeedsNativeExecution: true})
	// The descriptor effect carries its ordered source arguments; the evaluated
	// value is retained in the modeled target object rather than effect.Value.
	if !reflect.DeepEqual(state.Objects[0].Own["__esModule"], CJSStateValue{Kind: "literal", Literal: &markerValue}) {
		return fmt.Errorf("source descriptor value changed")
	}
	for _, index := range []int{2, 3} {
		n := plan.Initialization[index].Node.AsExpressionStatement().Expression
		assignment := n.AsBinaryExpression()
		key := assignment.Left.AsPropertyAccessExpression().Name().Text()
		var value CJSStateValue
		matched := 0
		for _, fn := range plan.HoistedFunctions {
			if fn.Name == assignment.Right.Text() {
				var err error
				value, err = functionValue(fn)
				if err != nil {
					return err
				}
				matched++
			}
		}
		if matched != 1 {
			return fmt.Errorf("source export has no unique original function")
		}
		expected = append(expected, CJSStartupEffect{Source: nodeID(n), Kind: "write-own-data-property", Target: CJSStateValue{Kind: "object", Object: 0}, Value: value, Key: key, NeedsNativeExecution: true})
	}
	for _, index := range []int{4, 5, 6, 7, 8} {
		declaration := plan.Initialization[index].Node.AsVariableStatement().DeclarationList.AsVariableDeclarationList().Declarations.Nodes[0].AsVariableDeclaration()
		name := declaration.Name()
		cell, err := cellFor(name)
		if err != nil {
			return err
		}
		value := CJSStateValue{Kind: "unknown"}
		if index < 8 {
			if declaration.Initializer.Kind != ast.KindRegularExpressionLiteral {
				return fmt.Errorf("original regexp missing")
			}
			literal := nodeID(declaration.Initializer)
			expected = append(expected, CJSStartupEffect{Source: literal, Kind: "create-regexp", NeedsNativeExecution: true})
			value = CJSStateValue{Kind: "regexp", Literal: &literal}
		}
		expected = append(expected, CJSStartupEffect{Source: nodeID(name), Kind: "initialize-binding-cell", Cell: cell, Value: value, NeedsNativeExecution: true})
	}
	if len(state.Effects) != len(expected) {
		return fmt.Errorf("effect count differs from source prefix")
	}
	for i, want := range expected {
		if !reflect.DeepEqual(state.Effects[i], want) {
			return fmt.Errorf("source effect %d differs: got %+v, want %+v", i, state.Effects[i], want)
		}
	}
	return nil
}
