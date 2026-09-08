package graph

// Intrinsic value storage is selected only with producer/receiver provenance.
// A library interface annotation alone does not establish native object identity.
const (
	// NullableString is exactly string|null. Optional still denotes undefined.
	TypeNullableString      TypeKind       = "nullable-string"
	TypeHeaders             TypeKind       = "intrinsic-headers"
	TypeURL                 TypeKind       = "intrinsic-url"
	TypeURLSearchParams     TypeKind       = "intrinsic-url-search-params"
	ExpressionPlatformValue ExpressionKind = "platform-value"
)

type PlatformValueKind string

const (
	PlatformRequestMethod   PlatformValueKind = "request-method"
	PlatformRequestURL      PlatformValueKind = "request-url"
	PlatformRequestHeaders  PlatformValueKind = "request-headers"
	PlatformHeadersGet      PlatformValueKind = "headers-get"
	PlatformURLNew          PlatformValueKind = "url-new"
	PlatformURLPathname     PlatformValueKind = "url-pathname"
	PlatformURLSearchParams PlatformValueKind = "url-search-params"
	PlatformSearchParamsGet PlatformValueKind = "search-params-get"
	PlatformEnvGet          PlatformValueKind = "env-get"
	PlatformResponseNew     PlatformValueKind = "response-new"
	PlatformResponseJSON    PlatformValueKind = "response-json"
)
const PlatformNode24Collected = "node24-collected-v1"

// Receiver and all arguments remain on Expression in source evaluation order.
// Kind is resolved against an actual intrinsic symbol; native admission must
// additionally establish receiver origin and absence of overriding mutations.
type PlatformValue struct {
	Kind     PlatformValueKind
	Contract string
	// ResponseInit entries index Arguments after the body, preserving source evaluation.
	ResponseInit []string
}
