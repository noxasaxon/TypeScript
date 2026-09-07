package graph

// AsyncProgram is the separately admitted one-suspension host entrypoint. The
// ordinary Program cannot accidentally interpret an await as synchronous work.
type AsyncProgram struct {
	Module   *Program
	Position Position
	Input    Parameter
	Result   Type
	Before   []*Statement
	Await    AsyncAwait
	After    []*Statement
	Catch    []*Statement
	HasCatch bool
	Finally  []*Statement
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
