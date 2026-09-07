package tsox

import (
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/internal/tsox/extract"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// Project holds an immutable configured source snapshot. SourceFiles returns
// a defensive copy of source/config/package bytes for fingerprinting.
// Entry and ConfigPath are canonical absolute paths. Checking never rereads disk.
// The private snapshot also protects checking from accidental public map changes.
type Project struct {
	Entry      string
	ConfigPath string
	snapshot   *checked.Project
}

// ReadProject loads the temporary strict NodeNext ESM project domain. Entry is
// relative to the config directory (or absolute), and must be a configured root.
// Runtime imports currently require explicit relative .ts sources; real package
// compilation and Fetch support are separate, still-required release milestones.
func ReadProject(configPath, entry string) (*Project, []graph.Diagnostic) {
	p, ds := checked.ReadProject(configPath, entry)
	if len(ds) != 0 {
		return nil, ds
	}
	return &Project{Entry: p.Entry, ConfigPath: p.ConfigPath, snapshot: p}, nil
}

func ExtractProject(project *Project) graph.Result {
	if project == nil {
		return graph.Result{Diagnostics: invalidProject()}
	}
	p, ds := project.snapshot.Check()
	if len(ds) != 0 {
		return graph.Result{Diagnostics: ds}
	}
	return extract.ExtractChecked(project.snapshot.Entry, p)
}

func ExtractAsyncProject(project *Project, entryName, hostName string) graph.AsyncResult {
	if project == nil {
		return graph.AsyncResult{Diagnostics: invalidProject()}
	}
	p, ds := project.snapshot.Check()
	if len(ds) != 0 {
		return graph.AsyncResult{Diagnostics: ds}
	}
	selected, localName, diagnostic := checked.AsyncEntry(p, entryName)
	if diagnostic != nil {
		return graph.AsyncResult{Diagnostics: []graph.Diagnostic{*diagnostic}}
	}
	return extract.ExtractAsyncChecked(project.snapshot.Entry, selected, localName, hostName)
}

func invalidProject() []graph.Diagnostic {
	return []graph.Diagnostic{{Construct: "ProjectSource", Position: graph.Position{Line: 1, Column: 1}, Message: "project must be created by ReadProject"}}
}

// SourceFiles returns a defensive copy; modifying it never changes compilation.
func (p *Project) SourceFiles() map[string]string {
	if p == nil {
		return nil
	}
	return p.snapshot.SourceFiles()
}
