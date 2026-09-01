// Package mutation defines the pointer-free source-coordinate surface used by
// offline mutation tools. It contains no compiler AST, checker, symbol, or type
// pointers.
package mutation

import "github.com/microsoft/typescript-go/tsox/graph"

// Span is a half-open UTF-8 byte range in the supplied source text.
type Span struct {
	Start int
	End   int
}

// TypeIdentity is the conservative checker identity used for mutation
// compatibility. Named is populated for named object shapes.
type TypeIdentity struct {
	Kind  string
	Named string
}

// Argument describes one call argument and, when it is a direct identifier,
// the checker-resolved binding it names.
type Argument struct {
	Span    Span
	Type    TypeIdentity
	Binding uint32
}

// CallSite describes one checked call expression and its containing statement.
type CallSite struct {
	Span           Span
	Statement      Span
	Arguments      []Argument
	ParameterTypes []TypeIdentity
}

// Binding describes a checker-resolved source binding and every reference to
// it. Declaration and Initializer are zero spans for non-variable bindings.
type Binding struct {
	ID          uint32
	Name        string
	Type        TypeIdentity
	Declaration Span
	Initializer Span
	Constant    bool
	Uses        []Span
}

// Result contains either complete checked sites or checker diagnostics.
type Result struct {
	Calls       []CallSite
	Bindings    []Binding
	Identifiers []string
	Diagnostics []graph.Diagnostic
}
