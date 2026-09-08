package graph

// Numeric builtins carry every supplied argument, in source evaluation order.
// Only the first supplies the value; absent arguments have the standard builtin
// defaults. These nodes arise only from checker-resolved library bindings.
const (
	// NumberConvert admits primitive conversion domains implemented by the
	// backend. An annotation cannot convert unknown storage into a number.
	ExpressionNumberConvert ExpressionKind = "number-convert"
	// Predicates never coerce: non-number arguments produce false.
	ExpressionNumberIsFinite  ExpressionKind = "number-is-finite"
	ExpressionNumberIsInteger ExpressionKind = "number-is-integer"
)
