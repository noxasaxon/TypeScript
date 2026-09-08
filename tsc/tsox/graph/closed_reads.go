package graph

// Closed reads preserve the original declared union receiver. They carry an
// obligation; checker narrowing does not prove that a payload is present.
const (
	ExpressionClosedTag      ExpressionKind = "closed-tag"
	ExpressionClosedProperty ExpressionKind = "closed-property"
)
