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

// ReceiverTarget is an assignable, side-effect-free prefix of an array
// receiver. Place uses non-null assertions on nullable parents; it is evaluated
// only from the original index/argument, after optional short circuiting.
type ReceiverTarget struct {
	Place       string
	Type        TypeIdentity // Non-nullable declared type, for replacement donors.
	Clearable   bool         // Declared type includes undefined, rather than just null.
	Donors      []string     // Compatible, visible initialized locals; nearest declaration first.
	RootBinding bool         // Rebinding this prefix writes a lexical root.
}

// ReceiverEffectSite exposes a scalar index or method argument whose evaluation
// follows receiver capture. Targets are writable receiver/owner prefixes, from
// outermost to innermost. Sites with effectful receiver paths are excluded.
type ReceiverEffectSite struct {
	Kind      string // "index" or "argument"
	Statement Span
	Operand   Span
	Type      TypeIdentity
	Targets   []ReceiverTarget
	TopLevel  bool // Statement is directly in the source file's statement list.
}

// Result contains either complete checked sites or checker diagnostics.
type Result struct {
	Calls           []CallSite
	Literals        []LiteralSite
	Bindings        []Binding
	Identifiers     []string
	ReceiverEffects []ReceiverEffectSite
	Diagnostics     []graph.Diagnostic
}

// SourceSites groups coordinates and binding identities within one source
// file. Spans and binding IDs are local to SourcePath, never to another module.
type SourceSites struct {
	SourcePath string
	Sites      Result
}

// ModulesResult contains all checked source files, ordered by source path, or
// diagnostics with no partial sites. Type-only dependencies are included.
type ModulesResult struct {
	Files       []SourceSites
	Diagnostics []graph.Diagnostic
}
