package extract

import (
	"fmt"

	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
)

func (h ScopedTypedBodyHandle) OriginalConsoleOutput(statement *graph.Statement) (checked.SourceIntrinsicCall, error) {
	if h.owner == nil {
		return checked.SourceIntrinsicCall{}, fmt.Errorf("registered source body required")
	}
	source, err := h.owner.StatementSource(h, statement)
	if err != nil {
		return checked.SourceIntrinsicCall{}, err
	}
	return h.owner.scope.OriginalConsoleOutput(source)
}
