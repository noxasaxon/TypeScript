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
// it. Initializer is a zero span for non-variable bindings.
type Binding struct {
	ID          uint32
	Name        string
	Type        TypeIdentity
	Declaration Span
	Initializer Span
	Constant    bool
	Uses        []Span
}

// LiteralSlot describes one replaceable value inside a literal. Field is
// populated for object-literal properties and empty for array elements.
type LiteralSlot struct {
	Span  Span
	Type  TypeIdentity
	Field string
}

// LiteralSite describes one checked object or array literal and its
// containing statement. Insertion is the byte position where an array append
// is inserted; it is zero for object literals.
type LiteralSite struct {
	Span        Span
	Statement   Span
	Kind        string
	Slots       []LiteralSlot
	ElementType TypeIdentity
	Insertion   int
}

// Result contains either complete checked sites or checker diagnostics.
type Result struct {
	Calls       []CallSite
	Literals    []LiteralSite
	Bindings    []Binding
	Identifiers []string
	Diagnostics []graph.Diagnostic
}
