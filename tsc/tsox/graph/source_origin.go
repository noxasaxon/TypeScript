package graph

// Source-only restrictions are one-way through the public API. They are carried
// by value copies, separate from removable diagnostics and serialized metadata.
// A restriction never certifies a native value, module startup or callable body.
func (p *Program) MarkSourceOnly() {
	if p != nil {
		p.sourceOnly = true
	}
}
func (p *Program) IsSourceOnly() bool { return p != nil && p.sourceOnly }
func (s *Statement) MarkSourceOnly() {
	if s != nil {
		s.sourceOnly = true
	}
}
func (s *Statement) IsSourceOnly() bool { return s != nil && s.sourceOnly }
func (x *Expression) MarkSourceOnly() {
	if x != nil {
		x.sourceOnly = true
	}
}
func (x *Expression) IsSourceOnly() bool { return x != nil && x.sourceOnly }
