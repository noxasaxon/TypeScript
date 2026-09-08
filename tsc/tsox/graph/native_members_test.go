package graph

import (
	"reflect"
	"testing"
)

func TestNativeRetainedMembersCurrentEdges(t *testing.T) {
	source := &Expression{Kind: ExpressionString, Type: Type{Kind: TypeString}, String: "pending"}
	source.MarkSourceOnly()
	restricted := &Statement{Kind: StatementReturn}
	restricted.MarkSourceOnly()
	typ := Type{Kind: TypeSourceUnclassified}
	cases := map[string]NativeMemberRoots{}
	// Every direct expression edge, statement sequence and parameter default
	// of the current container types is covered independently of operation kind.
	for _, root := range []any{&Expression{}, &Statement{}, &AsyncAwait{}, &AsyncProducer{}, &Parameter{}, &Program{}, &AsyncProgram{}, &AsyncStage{}, &AsyncFlow{}, &AsyncBlock{}, &AsyncProtectedRegion{}, &AsyncHelper{}} {
		v := reflect.ValueOf(root).Elem()
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			if !field.CanSet() {
				continue
			}
			var payload reflect.Value
			switch field.Type() {
			case reflect.TypeFor[*Expression]():
				payload = reflect.ValueOf(source)
			case reflect.TypeFor[[]*Expression]():
				payload = reflect.ValueOf([]*Expression{source})
			case reflect.TypeFor[[]*Statement]():
				payload = reflect.ValueOf([]*Statement{restricted})
			case reflect.TypeFor[[]Parameter]():
				payload = reflect.ValueOf([]Parameter{{Default: source}})
			default:
				continue
			}
			copy := reflect.New(v.Type())
			copy.Elem().Set(v)
			copy.Elem().Field(i).Set(payload)
			key := v.Type().Name() + "." + v.Type().Field(i).Name
			switch x := copy.Interface().(type) {
			case *Program:
				cases[key] = NativeMemberRoots{Program: x}
			case *AsyncProgram:
				cases[key] = NativeMemberRoots{Async: x}
			case *AsyncStage:
				cases[key] = NativeMemberRoots{Async: &AsyncProgram{Stages: []AsyncStage{*x}}}
			case *AsyncFlow:
				cases[key] = NativeMemberRoots{Async: &AsyncProgram{Flow: x}}
			case *AsyncBlock:
				cases[key] = NativeMemberRoots{Async: &AsyncProgram{Flow: &AsyncFlow{Blocks: []AsyncBlock{*x}}}}
			case *AsyncProtectedRegion:
				cases[key] = NativeMemberRoots{Statements: []*Statement{{Protected: x}}}
			case *AsyncHelper:
				cases[key] = NativeMemberRoots{Async: &AsyncProgram{Helpers: []*AsyncHelper{x}}}
			case *Expression:
				cases[key] = NativeMemberRoots{Expressions: []*Expression{x}}
			case *Statement:
				cases[key] = NativeMemberRoots{Statements: []*Statement{x}}
			case *AsyncAwait:
				cases[key] = NativeMemberRoots{Async: &AsyncProgram{Stages: []AsyncStage{{Await: *x}}}}
			case *AsyncProducer:
				cases[key] = NativeMemberRoots{Producers: []*AsyncProducer{x}}
			case *Parameter:
				cases[key] = NativeMemberRoots{Expressions: []*Expression{{Parameters: []Parameter{*x}}}}
			}
		}
	}
	cases["arrow.body"] = NativeMemberRoots{Expressions: []*Expression{{Body: []*Statement{restricted}}}}
	cases["arrow.default"] = NativeMemberRoots{Expressions: []*Expression{{Parameters: []Parameter{{Default: source}}}}}
	cases["arrow.args"] = NativeMemberRoots{Expressions: []*Expression{{Arguments: []*Expression{source}}}}
	cases["arrow.template"] = NativeMemberRoots{Expressions: []*Expression{{Expressions: []*Expression{source}}}}
	cases["object.property"] = NativeMemberRoots{Expressions: []*Expression{{Properties: []PropertyValue{{Value: source}}}}}
	cases["producer.args"] = NativeMemberRoots{Producers: []*AsyncProducer{{Arguments: []*Expression{source}}}}
	cases["protected.finally"] = NativeMemberRoots{Statements: []*Statement{{Protected: &AsyncProtectedRegion{Finally: []*Statement{restricted}}}}}
	cases["async.helper.default"] = NativeMemberRoots{Async: &AsyncProgram{Helpers: []*AsyncHelper{{Parameters: []Parameter{{Default: source}}}}}}
	cases["async.helper.body"] = NativeMemberRoots{Async: &AsyncProgram{Helpers: []*AsyncHelper{{Program: &AsyncProgram{After: []*Statement{restricted}}}}}}
	cases["async.flow.condition"] = NativeMemberRoots{Async: &AsyncProgram{Flow: &AsyncFlow{Blocks: []AsyncBlock{{Condition: source}}}}}
	cases["shape.field.type"] = NativeMemberRoots{Program: &Program{Shapes: []Shape{{Fields: []Field{{Type: typ}}}}}}
	cases["type.result"] = NativeMemberRoots{Types: []Type{{Result: &typ}}}
	cases["type.element"] = NativeMemberRoots{Types: []Type{{Element: &typ}}}
	cases["type.parameters"] = NativeMemberRoots{Types: []Type{{Parameters: []Type{typ}}}}
	for name, roots := range cases {
		t.Run(name, func(t *testing.T) {
			if RequireNativeMembers(roots) == nil {
				t.Fatal("retained source edge accepted")
			}
		})
	}
	t.Log(len(cases), "current retained-member paths")
}
func TestNativeRetainedMembersSharingCyclesAndSourceKinds(t *testing.T) {
	x := &Expression{Kind: ExpressionString, Type: Type{Kind: TypeString}, String: "native"}
	p := &Program{Statements: []*Statement{{Kind: StatementReturn, Value: &Expression{Kind: ExpressionArrow, Parameters: []Parameter{{Default: x}}, Body: []*Statement{{Kind: StatementReturn, Value: x}}}}}}
	if e := RequireNativeMembers(NativeMemberRoots{Program: p}); e != nil {
		t.Fatal("native closure/default/shared DAG", e)
	}
	cyclic := &Expression{Kind: ExpressionProperty}
	cyclic.Receiver = cyclic
	if RequireNativeMembers(NativeMemberRoots{Expressions: []*Expression{cyclic}}) == nil {
		t.Fatal("expression cycle accepted")
	}
	s := &Statement{Kind: StatementIf}
	s.Body = []*Statement{s}
	if RequireNativeMembers(NativeMemberRoots{Statements: []*Statement{s}}) == nil {
		t.Fatal("statement cycle accepted")
	}
	producer := &AsyncProducer{Kind: ProducerBodyJSON, Contract: ProducerBodyJSONCollectedNode24, Receiver: x, Arguments: []*Expression{x}}
	if e := RequireNativeMembers(NativeMemberRoots{Producers: []*AsyncProducer{producer}}); e != nil {
		t.Fatal("native producer", e)
	}
	for _, kind := range []ExpressionKind{ExpressionSourceInvoke, ExpressionCallAsync, ExpressionAsyncCallable, ExpressionPromiseAll, ExpressionPromiseGlobal} {
		if RequireNativeMembers(NativeMemberRoots{Expressions: []*Expression{{Kind: kind}}}) == nil {
			t.Fatal("source operation accepted", kind)
		}
	}
	for _, kind := range []TypeKind{TypeSourceUnclassified, TypeAsyncCallable, TypePromiseValue} {
		if RequireNativeMembers(NativeMemberRoots{Types: []Type{{Kind: kind}}}) == nil {
			t.Fatal("source schema accepted", kind)
		}
	}
}

func TestNativeRetainedMembersLimits(t *testing.T) {
	huge := &Expression{Kind: ExpressionString, StringUnits: make([]uint16, 2000001)}
	if RequireNativeMembers(NativeMemberRoots{Expressions: []*Expression{huge}}) == nil {
		t.Fatal("oversized retained graph did not stop at budget")
	}
	x := &Expression{Kind: ExpressionString}
	for i := 0; i < 600; i++ {
		x = &Expression{Kind: ExpressionProperty, Receiver: x}
	}
	if RequireNativeMembers(NativeMemberRoots{Expressions: []*Expression{x}}) == nil {
		t.Fatal("deep retained graph did not stop at budget")
	}
}
