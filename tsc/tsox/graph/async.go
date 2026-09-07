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
	Binding  BindingID
	Name     string
	Argument *Expression
}

type AsyncResult struct {
	Program     *AsyncProgram
	Diagnostics []Diagnostic
}

const StatementThrow StatementKind = "throw"
