package checked

import (
	"fmt"

	"github.com/microsoft/typescript-go/internal/bundled"
)

// SourceSnapshot registers source recovery against the already checked immutable
// dependency capture. It does not run normal extraction, consult disk, or certify
// runtime startup and native body semantics.
func (p *Project) SourceSnapshot() (*CJSBodySourceSnapshot, error) {
	if p == nil || p.dependency == nil || p.config == nil {
		return nil, fmt.Errorf("ProjectSource: captured dependency project required")
	}
	d := p.dependency
	if err := p.sourceSyntax.validate(d.Program); err != nil {
		return nil, err
	}
	if d.Program == nil || d.Config != p.config || p.Entry != d.Manifest.Entry || p.ConfigPath != d.Config.ConfigName() {
		return nil, fmt.Errorf("ProjectSource: captured project identity changed")
	}
	if len(d.Diagnostics) != 0 {
		return nil, fmt.Errorf("ProjectSource: configured checker diagnostics remain")
	}
	// Verify exact captured bytes and root membership without an OS fallback.
	// Bundled/compiler-owned libraries use the same immutable host overlay as
	// checking, while every external read must already exist in the replay.
	replay := replayPackages(d.Snapshot)
	fs := bundled.WrapFS(packageStandardFS{replay})
	for _, file := range d.Program.GetSourceFiles() {
		text, found := fs.ReadFile(file.FileName())
		if replay.fault != nil || !found || text != file.Text() {
			return nil, fmt.Errorf("ProjectSource: compiler file differs from captured bytes: %s", file.FileName())
		}
	}
	for _, path := range d.Config.FileNames() {
		if d.Program.GetSourceFile(path) == nil {
			return nil, fmt.Errorf("ProjectSource: configured root absent from captured Program: %s", path)
		}
	}
	return sourceSnapshotFromDependency(d, CJSExportBoundary{}, p.sourceSyntax)
}
