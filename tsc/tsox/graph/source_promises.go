package graph

// Source promise operations retain actual syntax/evaluation while startup,
// intrinsic behavior, multiplicity and payload ownership are still unproved.
const (
	ExpressionPromiseAll    ExpressionKind = "source-promise-all"
	ExpressionPromiseGlobal ExpressionKind = "source-promise-global"
)
