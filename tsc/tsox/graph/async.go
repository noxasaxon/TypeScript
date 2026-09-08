package graph

// AsyncProgram is the separately admitted sequential host entrypoint. The
// ordinary Program cannot accidentally interpret an await as synchronous work.
type AsyncProgram struct {
	Platform     AsyncPlatformKind
	Module       *Program
	Position     Position
	Input        Parameter
	Result       Type
	Stages       []AsyncStage
	After        []*Statement
	Catch        []*Statement
	CatchBinding *Parameter
	HasCatch     bool
	Finally      []*Statement
	// Flow represents conditional joins and cyclic loop edges around suspension.
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
// Type is the fulfillment value. The host contract still takes/fulfills strings;
// direct helper results use their actual type. Rejections currently use strings.
type AsyncAwait struct {
	// Promise belongs only to the explicit provisional callable-body lane.
	Promise *Expression
	// Producer is exclusive with the legacy Host/Argument and direct Helper lanes.
	Producer *AsyncProducer
	Position Position
	Host     BindingID
	// Helper selects an owned child continuation. Argument then contains an
	// analysis-only typed call projection, never a synchronous executable call.
	Helper   BindingID
	Binding  BindingID
	Name     string
	Type     Type
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
	Regions []AsyncRegion
	Entry   int
	Blocks  []AsyncBlock
	Body    []*Statement
}

// AsyncBlock ends with a host suspension, conditional successors, or Next.
// Next == -1 denotes terminal completion; terminal statements remain in Before.
// Await refers to the corresponding AsyncProgram.Stages operation.
type AsyncBlock struct {
	Region           int
	Phase            AsyncRegionPhase
	Completion       AsyncCompletionKind
	Before           []*Statement
	Await            int // -1 for synchronous blocks
	Condition        *Expression
	Next, Then, Else int
}

const StatementAsyncAwait StatementKind = "async-await"

// AsyncHelper has its own promise settlement boundary. Parameters and saved
// locals have selected owning storage; no Promise value escapes into the ordinary graph.
type AsyncHelper struct {
	Binding    BindingID
	Name       string
	Parameters []Parameter
	Program    *AsyncProgram
}

// FulfillmentType preserves graphs built through the original string-host API.
// Newly extracted awaits always record their actual Type.
func (a AsyncAwait) FulfillmentType() Type {
	if a.Type.Kind == "" {
		return Type{Kind: TypeString}
	}
	return a.Type
}
