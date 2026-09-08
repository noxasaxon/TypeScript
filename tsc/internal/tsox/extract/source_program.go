package extract

import (
	"fmt"

	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
)

func (h ScopedTypedBodyHandle) OriginalCallableTarget(x *graph.Expression) (checked.SourceCallableTarget, error) {
	if h.owner == nil {
		return checked.SourceCallableTarget{}, fmt.Errorf("registered source body required")
	}
	source, err := h.owner.SourceOf(h, x)
	if err != nil {
		return checked.SourceCallableTarget{}, err
	}
	return h.owner.scope.OriginalCallableTarget(source)
}
