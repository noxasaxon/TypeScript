package checked

import (
	"crypto/sha256"
	"fmt"
	"github.com/microsoft/typescript-go/internal/ast"
	"github.com/microsoft/typescript-go/tsox/graph"
)

// ScopedSourceHandle names an actual immutable node in one captured scope.
// Numeric positions or graph-local binding IDs cannot construct this handle.
type ScopedSourceHandle struct {
	owner *SourceRecoveryScope
	node  *ast.Node
}

func (s *SourceRecoveryScope) SourceHandle(n *ast.Node) (ScopedSourceHandle, error) {
	if e := s.ValidateSourceCoverage(); e != nil {
		return ScopedSourceHandle{}, e
	}
	if s == nil || !s.nodes[n] {
		return ScopedSourceHandle{}, fmt.Errorf("source node from this scope required")
	}
	return ScopedSourceHandle{s, n}, nil
}
func (s *SourceRecoveryScope) SourceSite(h ScopedSourceHandle) (graph.SourceSite, error) {
	if e := s.ValidateSourceCoverage(); e != nil {
		return graph.SourceSite{}, e
	}
	if s == nil || h.owner != s || !s.nodes[h.node] {
		return graph.SourceSite{}, fmt.Errorf("foreign source handle")
	}
	return s.site(h.node), nil
}
func (s *SourceRecoveryScope) SourceLexical(h ScopedSourceHandle) (graph.SourceLexicalID, error) {
	if _, e := s.SourceSite(h); e != nil {
		return 0, e
	}
	n := h.node
	if n.Kind != ast.KindIdentifier {
		n = n.Name()
	}
	if n == nil {
		return 0, fmt.Errorf("source declaration has no lexical name")
	}
	b, e := s.LexicalBinding(n)
	if e != nil {
		return 0, e
	}
	return s.BindingID(b)
}
func (s *SourceRecoveryScope) ValidateSourceCoverage() error {
	if s == nil || s.snapshot == nil || s.snapshot.Program == nil {
		return fmt.Errorf("actual source snapshot required")
	}
	if e := s.validateSourceSyntax(); e != nil {
		return e
	}
	p := s.snapshot.Program
	if len(p.Compiler.GetSourceFiles()) != len(s.files) {
		return fmt.Errorf("compiler source inventory changed")
	}
	for _, file := range p.Compiler.GetSourceFiles() {
		if s.files[file] == 0 {
			return fmt.Errorf("foreign compiler source added")
		}
	}
	expected := 0
	for _, record := range s.view.Files {
		file := p.Compiler.GetSourceFile(record.Path)
		if file == nil || !s.nodes[file.AsNode()] || fmt.Sprintf("%x", sha256.Sum256([]byte(file.Text()))) != record.SHA256 {
			return fmt.Errorf("source snapshot changed")
		}
		if record.OwnedSchema {
			expected++
			if p.Files[file] != record.Path || file.IsDeclarationFile {
				return fmt.Errorf("owned schema source coverage changed")
			}
		} else if _, ok := p.Files[file]; ok {
			return fmt.Errorf("unowned declaration source added")
		}
	}
	if len(p.Files) != expected {
		return fmt.Errorf("foreign schema source added")
	}
	runtime := []string{}
	for _, m := range s.view.Modules {
		if m.Format != "builtin" {
			runtime = append(runtime, m.Identity)
		}
	}
	if len(runtime) != len(p.RuntimeFiles) {
		return fmt.Errorf("selected runtime coverage changed")
	}
	for i, file := range p.RuntimeFiles {
		if p.Compiler.GetSourceFile(runtime[i]) != file {
			return fmt.Errorf("selected runtime source order changed")
		}
	}
	return nil
}
