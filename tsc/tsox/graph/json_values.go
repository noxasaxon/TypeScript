package graph

// These categories and operations are staged contracts. Public extractors do
// not emit them until the evidence and backend consumers implement the domain.
const (
	// TypeUnknown describes storage whose runtime kind is not statically known.
	// It does not prove JSON origin, immutability, or a checker's narrowed shape.
	// Boundary admission must establish the incoming domain independently.
	TypeUnknown TypeKind = "unknown"
	TypeNull    TypeKind = "null"
)

const (
	ExpressionNull ExpressionKind = "null"
	// JSONParse evaluates Operand as text and parses at runtime; syntax errors
	// throw. Its TypeUnknown result is not the type of an annotation or cast.
	ExpressionJSONParse ExpressionKind = "json-parse"
	// TypeOf returns the JS typeof string: null and arrays both yield object.
	ExpressionTypeOf ExpressionKind = "typeof"
	// IsArray is the checker-resolved Array.isArray builtin applied to Operand.
	ExpressionIsArray ExpressionKind = "is-array"
	// UnknownProperty reads Receiver[Index] in source evaluation order. The
	// result includes undefined; null/undefined receivers throw. This operation
	// does not establish own lookup or exclude inherited properties.
	ExpressionUnknownProperty ExpressionKind = "unknown-property"
	// HasProperty evaluates Index then Receiver (the order of source `in`).
	// Primitive RHS throws. Prototype traversal is part of its semantics until
	// separate origin/effect evidence justifies a cheaper own-property lookup.
	ExpressionHasProperty ExpressionKind = "has-property"
	// UnknownProjection is an UNPROVED obligation to read Operand as Type.
	// The original unknown operand is retained. Evidence must prove a matching
	// successful control edge and unchanged value/place version before emission;
	// checker narrowing alone must never turn this into an unchecked unwrap.
	// Supported targets are scalar leaves or arrays of TypeUnknown elements.
	ExpressionUnknownProjection ExpressionKind = "unknown-projection"
	// StringLength counts UTF16 code units, independent of native encoding.
	ExpressionStringLength ExpressionKind = "string-length"
	// StringTrim uses the ECMAScript WhiteSpace/LineTerminator set on Operand.
	ExpressionStringTrim ExpressionKind = "string-trim"
)
