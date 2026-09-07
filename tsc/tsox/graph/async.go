package graph

// AsyncProgram is the separately admitted sequential host entrypoint. The
// ordinary Program cannot accidentally interpret an await as synchronous work.
type AsyncProgram struct {
	Module   *Program
	Position Position
	Input    Parameter
	Result   Type
	Stages   []AsyncStage
	After    []*Statement
	Catch    []*Statement
	HasCatch bool
	Finally  []*Statement
	// Flow is present when an await lies inside an acyclic conditional.
	// Stages/After retain the original sequential API when Flow is nil.
	Flow *AsyncFlow
	// Helpers are checker-resolved direct awaited functions; their calls are acyclic.
	Helpers []*AsyncHelper
}

// AsyncStage contains synchronous work followed by one direct host suspension.
// Stages are ordered; After runs following the final completion.
type AsyncStage struct {
	Before []*Statement
	Await  AsyncAwait
}

// AsyncAwait records a checker-resolved host operation and its source position.
// The initial host contract takes and fulfills with a string; errors are strings.
type AsyncAwait struct {
	Position Position
	Host     BindingID
	// Helper selects an owned child continuation. Argument then contains an
	// analysis-only scalar call projection, never a synchronous executable call.
	Helper   BindingID
	Binding  BindingID
	Name     string
	Argument *Expression
}

type AsyncResult struct {
	Program     *AsyncProgram
	Diagnostics []Diagnostic
}

const StatementThrow StatementKind = "throw"

// AsyncFlow shares joins instead of duplicating each branch's suffix. Body is
// the structured analysis projection: StatementAsyncAwait marks each suspension.
// It is never an ordinary executable statement.
type AsyncFlow struct {
	Entry  int
	Blocks []AsyncBlock
	Body   []*Statement
}

// AsyncBlock ends with a host suspension, conditional successors, or Next.
// Next == -1 denotes terminal completion; terminal statements remain in Before.
// Await refers to the corresponding AsyncProgram.Stages operation.
type AsyncBlock struct {
	Before           []*Statement
	Await            int // -1 for synchronous blocks
	Condition        *Expression
	Next, Then, Else int
}

const StatementAsyncAwait StatementKind = "async-await"

// AsyncHelper has its own promise settlement boundary. Parameters and saved
// scalar locals are owned; no Promise value escapes into the ordinary graph.
type AsyncHelper struct {
	Binding    BindingID
	Name       string
	Parameters []Parameter
	Program    *AsyncProgram
}
