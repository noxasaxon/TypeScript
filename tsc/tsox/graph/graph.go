// Package graph defines the plain-data semantic graph exchanged across the
// fork boundary. It deliberately contains no compiler AST, checker, symbol,
// or type pointers.
package graph

import "fmt"

// Program is the checked subset program consumed by the Rust emitter.
type Program struct {
	SourcePath string
	Shapes     []Shape
	Statements []*Statement
}

// BindingID preserves checker-resolved lexical identity without retaining a
// compiler symbol pointer. Zero means that an expression has no binding.
type BindingID uint32

// ShapeID identifies one checker-resolved named interface or type alias.
// Zero is reserved for values that have no supported named shape.
type ShapeID uint32

// Position is a one-based source position suitable for diagnostics.
type Position struct {
	Line   int
	Column int
}

// TypeKind is one of the value categories required by the current subset.
type TypeKind string

const (
	TypeNumber   TypeKind = "number"
	TypeString   TypeKind = "string"
	TypeBoolean  TypeKind = "boolean"
	TypeFunction TypeKind = "function"
	TypeVoid     TypeKind = "void"
	TypeObject   TypeKind = "object"
	TypeArray    TypeKind = "array"
)

// Type is a checked value type. Parameters and Result are populated only for
// TypeFunction.
type Type struct {
	Kind       TypeKind
	Parameters []Type
	Result     *Type
	Shape      ShapeID
	Element    *Type
}

// Shape is one checker-resolved named object declaration. Fields retain
// declaration order.
type Shape struct {
	ID       ShapeID
	Position Position
	Name     string
	Fields   []Field
}

// Field is one required property in a named shape.
type Field struct {
	Position Position
	Name     string
	Type     Type
}

// FunctionType constructs the function shape used by declarations, arrows,
// and inferred function-valued variables.
func FunctionType(parameters []Type, result Type) Type {
	return Type{Kind: TypeFunction, Parameters: parameters, Result: &result}
}

// Parameter is a checked function or arrow parameter.
type Parameter struct {
	Position Position
	Binding  BindingID
	Name     string
	Type     Type
}

// StatementKind identifies a normalized statement form.
type StatementKind string

const (
	StatementVariable   StatementKind = "variable"
	StatementExpression StatementKind = "expression"
	StatementPrint      StatementKind = "print"
	StatementIf         StatementKind = "if"
	StatementWhile      StatementKind = "while"
	StatementFor        StatementKind = "for"
	StatementForOf      StatementKind = "for-of"
	StatementFunction   StatementKind = "function"
	StatementReturn     StatementKind = "return"
)

// Statement stores only fields used by its Kind. Blocks are normalized into
// statement slices on their owning control-flow node.
type Statement struct {
	Kind       StatementKind
	Position   Position
	Binding    BindingID
	Name       string
	Mutable    bool
	Type       Type
	Parameters []Parameter
	ReturnType Type
	Value      *Expression
	Arguments  []*Expression
	Condition  *Expression
	Init       []*Statement
	Increment  *Expression
	Then       []*Statement
	Else       []*Statement
	Body       []*Statement
}

// ExpressionKind identifies a normalized expression form.
type ExpressionKind string

const (
	ExpressionNumber      ExpressionKind = "number"
	ExpressionString      ExpressionKind = "string"
	ExpressionBoolean     ExpressionKind = "boolean"
	ExpressionIdentifier  ExpressionKind = "identifier"
	ExpressionBinary      ExpressionKind = "binary"
	ExpressionUnary       ExpressionKind = "unary"
	ExpressionAssignment  ExpressionKind = "assignment"
	ExpressionUpdate      ExpressionKind = "update"
	ExpressionCall        ExpressionKind = "call"
	ExpressionMethodCall  ExpressionKind = "method-call"
	ExpressionTemplate    ExpressionKind = "template"
	ExpressionArrow       ExpressionKind = "arrow"
	ExpressionObject      ExpressionKind = "object"
	ExpressionProperty    ExpressionKind = "property"
	ExpressionArray       ExpressionKind = "array"
	ExpressionIndex       ExpressionKind = "index"
	ExpressionArrayLength ExpressionKind = "array-length"
)

// PropertyValue is one declaration-named object-literal field value.
type PropertyValue struct {
	Position Position
	Name     string
	Value    *Expression
}

// Expression stores only fields used by its Kind. Chunks has exactly one more
// entry than Expressions for a template expression.
type Expression struct {
	Kind        ExpressionKind
	Position    Position
	Binding     BindingID
	Type        Type
	Number      float64
	String      string
	Boolean     bool
	Name        string
	Operator    string
	Prefix      bool
	Left        *Expression
	Right       *Expression
	Operand     *Expression
	Callee      *Expression
	Arguments   []*Expression
	Chunks      []string
	Expressions []*Expression
	Parameters  []Parameter
	ReturnType  Type
	Body        []*Statement
	Receiver    *Expression
	Index       *Expression
	Properties  []PropertyValue
}

// IsCallbackArrayMethod reports whether name uses the callback iteration
// protocol shared across extraction, evidence selection, and emission.
func IsCallbackArrayMethod(name string) bool {
	switch name {
	case "forEach", "map", "filter", "some", "every", "findIndex", "reduce":
		return true
	default:
		return false
	}
}

// Diagnostic is the single clean fence returned when extraction cannot
// preserve a source construct's semantics in this slice.
type Diagnostic struct {
	SourcePath string
	Position   Position
	Construct  string
	Message    string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%s:%d:%d: %s", d.SourcePath, d.Position.Line, d.Position.Column, d.Message)
}

// Result contains either a Program or one Diagnostic. Extractors stop at the
// first source-order fence so callers never receive a partial graph.
type Result struct {
	Program     *Program
	Diagnostics []Diagnostic
}
