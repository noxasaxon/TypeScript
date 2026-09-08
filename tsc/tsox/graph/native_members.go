package graph

import (
	"fmt"
	"reflect"
)

// NativeMemberRoots is admission coverage, never an evaluation sequence. All
// retained members matter, including deferred bodies/defaults and producer args.
// This guard refuses pending source members; success issues no native proof.
type NativeMemberRoots struct {
	Program     *Program
	Async       *AsyncProgram
	Statements  []*Statement
	Expressions []*Expression
	Producers   []*AsyncProducer
	Types       []Type
}
type NativeMemberError struct {
	Position Position
	Reason   string
}

func (e *NativeMemberError) Error() string { return e.Reason }

// RequireNativeMembers follows every field of the plain graph schema, including
// fields omitted from serialization. It has no dependency on execution walkers.
// New unsupported payload kinds fail closed instead of silently dropping edges.
func RequireNativeMembers(roots NativeMemberRoots) error {
	type address struct {
		typ     reflect.Type
		pointer uintptr
	}
	type item struct {
		value    reflect.Value
		depth    int
		exit     *address
		position Position
	}
	stack := []item{{value: reflect.ValueOf(roots)}}
	active, done := map[address]bool{}, map[address]bool{}
	visited := 0
	for len(stack) > 0 {
		work := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if work.exit != nil {
			delete(active, *work.exit)
			done[*work.exit] = true
			continue
		}
		visited++
		if visited > 2000000 || work.depth > 512 {
			return &NativeMemberError{work.position, "retained native graph exceeds bounded admission domain"}
		}
		v := work.value
		if !v.IsValid() {
			continue
		}
		position := work.position
		reject := func(reason string) error { return &NativeMemberError{position, reason} }
		if v.CanInterface() {
			switch value := v.Interface().(type) {
			case *Program:
				if value != nil && (value.IsSourceOnly() || value.SourceRecovery != nil) {
					return reject("source-only Program has unresolved native obligations")
				}
			case *Statement:
				if value != nil {
					position = value.Position
					if value.IsSourceOnly() || value.Kind == StatementUnresolvedReturn || value.Kind == StatementReturnPromise {
						return reject("source-only retained statement has no native proof")
					}
				}
			case *Expression:
				if value != nil {
					position = value.Position
					if value.IsSourceOnly() || value.Pending != nil {
						return reject("source-only retained expression has no native proof")
					}
					switch value.Kind {
					case ExpressionSourceInvoke, ExpressionCallAsync, ExpressionAsyncCallable, ExpressionPromiseAll, ExpressionPromiseGlobal:
						return reject("source-only retained operation has no native proof")
					}
				}
			case Type:
				if value.Kind == TypeSourceUnclassified || value.Kind == TypeAsyncCallable || value.Kind == TypePromiseValue {
					return reject("source-only retained schema has no native carrier")
				}
			case *SourcePending:
				if value != nil {
					return reject("source-only pending member has no native proof")
				}
			case *SourceRecoveryStamp:
				if value != nil {
					return reject("source-only recovery stamp has unresolved native obligations")
				}
			}
		}
		push := func(child reflect.Value) {
			stack = append(stack, item{value: child, depth: work.depth + 1, position: position})
		}
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				continue
			}
			key := address{v.Type(), v.Pointer()}
			if active[key] {
				return reject("cyclic retained graph has no native admission contract")
			}
			if done[key] {
				continue
			}
			active[key] = true
			stack = append(stack, item{exit: &key})
			push(v.Elem())
		case reflect.Interface:
			if !v.IsNil() {
				push(v.Elem())
			}
		case reflect.Struct:
			if v.Type().PkgPath() != reflect.TypeFor[Program]().PkgPath() {
				return reject(fmt.Sprintf("unclassified retained graph payload %s", v.Type()))
			}
			for i := v.NumField() - 1; i >= 0; i-- {
				push(v.Field(i))
			}
		case reflect.Array, reflect.Slice:
			if v.Len() > 2000000-visited-len(stack) {
				return reject("retained native graph exceeds bounded admission domain")
			}
			for i := v.Len() - 1; i >= 0; i-- {
				push(v.Index(i))
			}
		case reflect.Map:
			if v.Len() > (2000000-visited-len(stack))/2 {
				return reject("retained native graph exceeds bounded admission domain")
			}
			// Graph maps carry metadata/edges, not execution order. Visit both sides.
			for _, key := range v.MapKeys() {
				push(v.MapIndex(key))
				push(key)
			}
		case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64, reflect.String:
		default:
			return reject(fmt.Sprintf("unclassified retained graph payload %s", v.Type()))
		}
	}
	return nil
}
