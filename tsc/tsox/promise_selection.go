package tsox

import "fmt"

func (p *Project) SelectSourcePromiseAnalysis(export string) (bool, error) {
	if p == nil || p.snapshot == nil || p.Entry != p.snapshot.Entry || p.ConfigPath != p.snapshot.ConfigPath {
		return false, fmt.Errorf("captured project selection required")
	}
	return p.snapshot.SelectSourcePromiseAnalysis(export)
}
