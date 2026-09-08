package graph

import "fmt"

// TypeSourceUnclassified is a source schema without an evaluated value domain.
// It is neither JSON unknown nor a native storage category.
const TypeSourceUnclassified TypeKind = "source-unclassified"
const ExpressionSourceInvoke ExpressionKind = "source-invoke"

type SourceRecoveryStamp struct {
	Scope               SourceScopeID
	Revision            uint64
	SnapshotFingerprint string
}
type SourcePending struct {
	CapturedCells []SourceCellID
	// Source candidates are not a complete runtime target set.
	CandidateSetComplete bool
	Kind                 string
	Site                 SourceSite
	ImportLocal          SourceLexicalID
	Candidates           []SourceInstanceID
	Obligations          []string
}
type RecoveredSourceBody struct {
	NoOps       []SourceSite
	Registry    SourceRegistryView
	Template    SourceTemplateID
	Async       bool
	Body        []*Statement
	Parameters  []BindingID
	Bindings    map[BindingID]SourceLexicalID
	Expressions map[*Expression]SourceSite `json:"-"`
	Statements  map[*Statement]SourceSite  `json:"-"`
	Obligations []string
}

func (b *RecoveredSourceBody) Stamp() *SourceRecoveryStamp {
	return &SourceRecoveryStamp{Scope: b.Registry.Scope, Revision: b.Registry.Revision, SnapshotFingerprint: b.Registry.SnapshotFingerprint}
}
func (b *RecoveredSourceBody) RejectNative() error {
	return fmt.Errorf("source-only recovery: startup/body/import and evaluated operation proof remain unresolved")
}
