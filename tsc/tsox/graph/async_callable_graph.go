package graph

// These source-model kinds deliberately have no public emitter/admission lane.
const (
	StatementUnresolvedReturn StatementKind  = "unresolved-source-return"
	TypeAsyncCallable         TypeKind       = "async-callable"
	TypePromiseValue          TypeKind       = "promise-value"
	ExpressionAsyncCallable   ExpressionKind = "async-callable-reference"
	ExpressionCallAsync       ExpressionKind = "call-async"
	StatementReturnPromise    StatementKind  = "return-promise"
)

type AsyncCallableInvocation struct {
	Instance   int
	Template   string
	Expression *Expression
}

type AsyncCapturedCallable struct {
	Binding  BindingID
	Cell     int
	Instance int
	Template string
	Position Position
}
