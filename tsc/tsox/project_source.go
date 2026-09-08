package tsox

import (
	"fmt"

	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/internal/tsox/extract"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// RegisteredSourceBodies uses this Project's existing captured compiler Program.
// Handles carry source ownership only; no startup, effect, value or native
// execution certificate follows from successful registration.
// RegisteredSourceProgram retains one registry owner for the complete selected
// source application. Its zero value is invalid. Startup inspection never admits
// runtime execution and cannot be constructed from detached metadata.
type RegisteredSourceProgram struct{ owner *registeredSourceProgram }
type registeredSourceProgram struct {
	export   string
	scope    *checked.SourceRecoveryScope
	registry *extract.ScopedTypedBodies
}
type StartupSourceKind = checked.StartupSourceKind
type StartupSourceOperation = checked.StartupSourceOperation

const (
	StartupSourceUnresolved  = checked.StartupSourceUnresolved
	StartupSourceImport      = checked.StartupSourceImport
	StartupSourceFunction    = checked.StartupSourceFunction
	StartupSourceTypeErasure = checked.StartupSourceTypeErasure
	StartupSourceEmpty       = checked.StartupSourceEmpty
	StartupSourceExternal    = checked.StartupSourceExternal
)

func (p *Project) RegisteredSourceProgram(export string) (RegisteredSourceProgram, error) {
	if p == nil || p.snapshot == nil || p.Entry != p.snapshot.Entry || p.ConfigPath != p.snapshot.ConfigPath {
		return RegisteredSourceProgram{}, fmt.Errorf("ProjectSource: captured public project identity required")
	}
	snapshot, err := p.snapshot.SourceSnapshot()
	if err != nil {
		return RegisteredSourceProgram{}, err
	}
	scope, err := checked.NewSourceRecoveryScope(snapshot)
	if err != nil {
		return RegisteredSourceProgram{}, err
	}
	registry, err := extract.RegisterScopedTypedBodies(scope, export)
	if err != nil {
		return RegisteredSourceProgram{}, err
	}
	return RegisteredSourceProgram{&registeredSourceProgram{export, scope, registry}}, nil
}
func (p RegisteredSourceProgram) OriginalStartup() ([]StartupSourceOperation, error) {
	if p.owner == nil {
		return nil, fmt.Errorf("original registered source program required")
	}
	return p.owner.scope.OriginalStartup()
}
func (p RegisteredSourceProgram) Bodies() ([]RegisteredSourceBody, error) {
	if p.owner == nil {
		return nil, fmt.Errorf("original registered source program required")
	}
	if err := p.owner.scope.ValidateSourceCoverage(); err != nil {
		return nil, err
	}
	handles := p.owner.registry.Handles()
	out := make([]RegisteredSourceBody, len(handles))
	for i, handle := range handles {
		if _, err := handle.ResolveSourceBody(); err != nil {
			return nil, err
		}
		out[i] = RegisteredSourceBody{owner: handle}
	}
	return out, nil
}
func (p *Project) RegisteredSourceBodies(export string) ([]RegisteredSourceBody, error) {
	source, err := p.RegisteredSourceProgram(export)
	if err != nil {
		return nil, err
	}
	return source.Bodies()
}

type SourceCallableTarget = checked.SourceCallableTarget

func (b RegisteredSourceBody) OriginalCallableTarget(x *graph.Expression) (SourceCallableTarget, error) {
	return b.owner.OriginalCallableTarget(x)
}

func (p RegisteredSourceProgram) Entry() (SourceCallableTarget, error) {
	if p.owner == nil {
		return SourceCallableTarget{}, fmt.Errorf("original registered source program required")
	}
	return p.owner.scope.OriginalEntry(p.owner.export)
}
