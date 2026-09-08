package tsox

import (
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
)

type SourceIntrinsicCall = checked.SourceIntrinsicCall

func (b RegisteredSourceBody) OriginalConsoleOutput(statement *graph.Statement) (SourceIntrinsicCall, error) {
	return b.owner.OriginalConsoleOutput(statement)
}
