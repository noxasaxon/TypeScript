package graph

type TypedSourceBody struct {
	Registry            SourceRegistryView
	SourceModelInstance int
	Captures            []AsyncCapturedCallable
	Calls               []AsyncCallableInvocation
	Targets             []TypedSourceTarget
	Template            SourceTemplateID
	Source              SourceSite
	Program             *Program
	Function            *Statement
	Async               *AsyncProgram
	Parameters          []Parameter
	Bindings            map[BindingID][]SourceLexicalID
	Expressions         map[*Expression]SourceSite `json:"-"`
	Statements          map[*Statement]SourceSite  `json:"-"`
	GraphExpressions    []*Expression              `json:"-"`
	UnmappedExpressions int
	Obligations         []string
}

type TypedSourceTarget struct {
	Template                             SourceTemplateID
	SourceModelInstance, SourceModelCell int
}
