package graph

// SourceRegistryView is a detached source recovery index, never an admitted
// Program, executed module cache, effect-coverage or native value certificate.
type SourceScopeID string
type SourceFileID uint32
type SourceModuleID uint32
type SourceDeclarationID uint32
type SourceLexicalID uint32
type SourceTemplateID uint32
type SourceEnvironmentID uint32
type SourceInstanceID uint32
type SourceCellID uint32

type SourceSite struct {
	File       SourceFileID
	Start, End int
	Kind       string
}
type SourceFileRecord struct {
	ID                                               SourceFileID
	Path, SHA256                                     string
	DeclarationFile, ConfiguredRoot, SelectedRuntime bool
	OwnedSchema                                      bool
}
type SourceDeclarationRecord struct {
	ID      SourceDeclarationID
	Site    SourceSite
	Binding SourceLexicalID
}
type SourceLexicalRecord struct {
	ID           SourceLexicalID
	Name, Role   string
	Module       SourceModuleID
	Declarations []SourceDeclarationID
}
type SourceStartupRecord struct {
	Ordinal int
	Phase   string
	Site    SourceSite
	Status  string
}
type SourceRuntimeEdge struct{ Specifier, Mode, Module, Format string }
type SourceModuleRecord struct {
	ID                          SourceModuleID
	Identity, Format            string
	File                        SourceFileID
	ClosureOrder                int
	Dependencies                []SourceRuntimeEdge
	Startup                     []SourceStartupRecord
	Templates                   []SourceTemplateID
	Hoisted                     []SourceTemplateID
	StartupStatus, EffectStatus string
}
type SourceTemplateRecord struct {
	ID                               SourceTemplateID
	Module                           SourceModuleID
	Declaration, Body                SourceSite
	Name, BodyStatus, InstanceStatus string
}
type SourceEnvironmentRecord struct {
	ID               SourceEnvironmentID
	Module           SourceModuleID
	SourceModelIndex int
	Parent           SourceEnvironmentID
	Creation         SourceSite
	Status           string
}
type SourceInstanceRecord struct {
	ID               SourceInstanceID
	Module           SourceModuleID
	SourceModelIndex int
	Template         SourceTemplateID
	Environment      SourceEnvironmentID
	Strict           bool
	Status           string
}
type SourceCellRecord struct {
	ID               SourceCellID
	Module           SourceModuleID
	Environment      SourceEnvironmentID
	Binding          SourceLexicalID
	SourceModelIndex int
	Role, Status     string
}
type SourceImportRecord struct {
	Importer, Target                  SourceModuleID
	Local                             SourceLexicalID
	Site                              SourceSite
	Requested, Form, Capture, Status  string
	TypeDeclarations, CandidateBodies []SourceSite
}

type SourceModeledEffect struct {
	Module      SourceModuleID
	Ordinal     int
	Kind        string
	Site        SourceSite
	Environment SourceEnvironmentID
	Cell        SourceCellID
	Status      string
}

type SourceRecoveryObligation struct {
	Kind     string
	Module   SourceModuleID
	Template SourceTemplateID
	Instance SourceInstanceID
	Site     SourceSite
	Reason   string
}
type SourceRegistryView struct {
	Scope               SourceScopeID
	Revision            uint64
	SnapshotFingerprint string
	Files               []SourceFileRecord
	ConfiguredRoots     []SourceFileID
	Modules             []SourceModuleRecord
	Declarations        []SourceDeclarationRecord
	Bindings            []SourceLexicalRecord
	Templates           []SourceTemplateRecord
	Environments        []SourceEnvironmentRecord
	Instances           []SourceInstanceRecord
	Cells               []SourceCellRecord
	Imports             []SourceImportRecord
	ModeledEffects      []SourceModeledEffect
	Obligations         []SourceRecoveryObligation
}
