package extract

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/microsoft/typescript-go/internal/tsox/checked"
	"github.com/microsoft/typescript-go/tsox/graph"
	"sort"
)

type ScopedTypedBodies struct {
	scope    *checked.SourceRecoveryScope
	entries  []*scopedTypedEntry
	revision uint64
}
type scopedTypedEntry struct {
	integrity          *typedGraphIntegrity
	sourceSites        map[*graph.Expression]graph.SourceSite
	statementSites     map[*graph.Statement]graph.SourceSite
	body               *graph.TypedSourceBody
	source             checked.ScopedSourceHandle
	digest             [32]byte
	sources            map[*graph.Expression]checked.ScopedSourceHandle
	bindings           map[graph.BindingID][]checked.ScopedSourceHandle
	members            map[*graph.Expression]bool
	statementMembers   map[*graph.Statement]bool
	statements         map[*graph.Statement]checked.ScopedSourceHandle
	programStatements  map[*graph.Statement]bool
	programExpressions map[*graph.Expression]bool
	program            *graph.Program
	function           *graph.Statement
	async              *graph.AsyncProgram
}
type ScopedTypedBodyHandle struct {
	owner *ScopedTypedBodies
	index int
}

func (r *ScopedTypedBodies) Handles() []ScopedTypedBodyHandle {
	out := make([]ScopedTypedBodyHandle, len(r.entries))
	for i := range out {
		out[i] = ScopedTypedBodyHandle{r, i}
	}
	return out
}
func (h ScopedTypedBodyHandle) ResolveSourceBody() (*graph.TypedSourceBody, error) {
	if h.owner == nil {
		return nil, fmt.Errorf("registered source body required")
	}
	return h.owner.resolve(h)
}
func (r *ScopedTypedBodies) resolve(h ScopedTypedBodyHandle) (*graph.TypedSourceBody, error) {
	if r == nil || h.owner != r || h.index < 0 || h.index >= len(r.entries) {
		return nil, fmt.Errorf("foreign body owner")
	}
	if e := r.scope.ValidateSourceCoverage(); e != nil {
		return nil, e
	}
	if r.scope.View().Revision != r.revision {
		return nil, fmt.Errorf("source registry revision changed")
	}
	entry := r.entries[h.index]
	if e := entry.integrity.validate(entry.body); e != nil {
		return nil, e
	}
	if entry.body.Program != entry.program || entry.body.Function != entry.function || entry.body.Async != entry.async {
		return nil, fmt.Errorf("body graph identity changed")
	}
	ps, px := programMembers(entry.body.Program)
	if len(ps) != len(entry.programStatements) || len(px) != len(entry.programExpressions) {
		return nil, fmt.Errorf("complete Program membership changed")
	}
	for statement := range ps {
		if !entry.programStatements[statement] {
			return nil, fmt.Errorf("foreign Program statement")
		}
	}
	for x := range px {
		if !entry.programExpressions[x] {
			return nil, fmt.Errorf("foreign Program expression")
		}
	}
	members := typedBodyExpressions(entry.body)
	if len(entry.body.GraphExpressions) != len(entry.members) {
		return nil, fmt.Errorf("graph member list changed")
	}
	listed := map[*graph.Expression]bool{}
	for _, x := range entry.body.GraphExpressions {
		if !entry.members[x] || listed[x] {
			return nil, fmt.Errorf("foreign graph member list")
		}
		listed[x] = true
	}
	if len(members) != len(entry.members) {
		return nil, fmt.Errorf("expression membership changed")
	}
	for x := range members {
		if !entry.members[x] {
			return nil, fmt.Errorf("foreign graph expression")
		}
	}
	if len(entry.body.Expressions) != len(entry.sources) {
		return nil, fmt.Errorf("source expression mapping changed")
	}
	for x := range entry.sources {
		if entry.body.Expressions[x] != entry.sourceSites[x] {
			return nil, fmt.Errorf("source expression mapping changed")
		}
	}
	stmts := typedBodyStatements(entry.body)
	if len(stmts) != len(entry.statementMembers) {
		return nil, fmt.Errorf("statement membership changed")
	}
	for s := range stmts {
		if !entry.statementMembers[s] {
			return nil, fmt.Errorf("foreign graph statement")
		}
	}
	if len(entry.body.Statements) != len(entry.statements) {
		return nil, fmt.Errorf("statement source mapping changed")
	}
	for s := range entry.statements {
		if entry.body.Statements[s] != entry.statementSites[s] {
			return nil, fmt.Errorf("statement source mapping changed")
		}
	}
	digest, e := typedBodyDigest(entry.body)
	if e != nil || digest != entry.digest {
		return nil, fmt.Errorf("registered source graph changed")
	}
	return entry.body, nil
}
func typedBodyDigest(b *graph.TypedSourceBody) ([32]byte, error) {
	data, e := json.Marshal(b)
	if e != nil {
		return [32]byte{}, e
	}
	return sha256.Sum256(data), nil
}
func (r *ScopedTypedBodies) SourceOf(h ScopedTypedBodyHandle, x *graph.Expression) (checked.ScopedSourceHandle, error) {
	if _, e := r.resolve(h); e != nil {
		return checked.ScopedSourceHandle{}, e
	}
	source, ok := r.entries[h.index].sources[x]
	if !ok {
		return checked.ScopedSourceHandle{}, fmt.Errorf("expression is not an original mapped member of this body")
	}
	return source, nil
}
func (r *ScopedTypedBodies) BindingSources(h ScopedTypedBodyHandle, id graph.BindingID) ([]checked.ScopedSourceHandle, error) {
	if _, e := r.resolve(h); e != nil {
		return nil, e
	}
	sources := r.entries[h.index].bindings[id]
	if len(sources) == 0 {
		return nil, fmt.Errorf("graph-local binding has no registered original declaration")
	}
	return append([]checked.ScopedSourceHandle(nil), sources...), nil
}

func RegisterScopedTypedBodies(scope *checked.SourceRecoveryScope, export string) (*ScopedTypedBodies, error) {
	if scope == nil {
		return nil, fmt.Errorf("actual scope required")
	}
	if e := scope.ValidateSourceCoverage(); e != nil {
		return nil, e
	}
	p := scope.ActualProgram()
	helpers := sourceHelperBodies(p)
	if len(helpers.Diagnostics) > 0 {
		return nil, fmt.Errorf("typed helper source: %v", helpers.Diagnostics)
	}
	registry := &ScopedTypedBodies{scope: scope}
	add := func(body *graph.TypedSourceBody, source SourceBodyNode, sources map[*graph.Expression]SourceBodyNode, statementSources map[*graph.Statement]SourceBodyNode, bindings map[graph.BindingID][]SourceBodyNode) error {
		template, e := scope.ActualTemplate(source.Node)
		if e != nil {
			return e
		}
		h, e := scope.SourceHandle(source.Node)
		if e != nil {
			return e
		}
		site, e := scope.SourceSite(h)
		if e != nil {
			return e
		}
		body.Template = template
		body.Source = site
		body.Bindings = map[graph.BindingID][]graph.SourceLexicalID{}
		body.Expressions = map[*graph.Expression]graph.SourceSite{}
		body.Statements = map[*graph.Statement]graph.SourceSite{}
		body.Obligations = []string{"selected startup and effects remain pending", "each evaluated invocation/input/result/retention remains pending", "source layouts do not establish native carrier or referent identity"}
		entry := &scopedTypedEntry{sourceSites: map[*graph.Expression]graph.SourceSite{}, statementSites: map[*graph.Statement]graph.SourceSite{}, body: body, source: h, program: body.Program, function: body.Function, async: body.Async, statements: map[*graph.Statement]checked.ScopedSourceHandle{}, sources: map[*graph.Expression]checked.ScopedSourceHandle{}, bindings: map[graph.BindingID][]checked.ScopedSourceHandle{}}
		// Traverse actual graph membership. A helper bundle may contain source maps
		// for other bodies; those are never accepted by this body's query methods.
		members := typedBodyExpressions(body)
		entry.members = members
		entry.statementMembers = typedBodyStatements(body)
		for statement := range entry.statementMembers {
			if source, ok := statementSources[statement]; ok {
				h, e := scope.SourceHandle(source.Node)
				if e != nil {
					return e
				}
				site, e := scope.SourceSite(h)
				if e != nil {
					return e
				}
				entry.statements[statement] = h
				body.Statements[statement] = site
				entry.statementSites[statement] = site
			}
		}
		used := map[graph.BindingID]bool{}
		for _, p := range body.Parameters {
			used[p.Binding] = true
		}
		for x := range members {
			body.GraphExpressions = append(body.GraphExpressions, x)
			if x.Binding != 0 {
				used[x.Binding] = true
			}
			if source, ok := sources[x]; ok {
				h, e := scope.SourceHandle(source.Node)
				if e != nil {
					return e
				}
				s, e := scope.SourceSite(h)
				if e != nil {
					return e
				}
				entry.sources[x] = h
				body.Expressions[x] = s
				entry.sourceSites[x] = s
			} else {
				body.UnmappedExpressions++
			}
			// Durable refusal only. Actual membership is checked by this registry.
			if x.Pending == nil {
				x.Pending = &graph.SourcePending{Kind: "registered-typed-source", Obligations: []string{"source scope startup and evaluated invocation unresolved"}}
			}
		}
		for id := range used {
			for _, source := range bindings[id] {
				h, e := scope.SourceHandle(source.Node)
				if e != nil {
					return e
				}
				lexical, e := scope.SourceLexical(h)
				if e != nil {
					return fmt.Errorf("binding %d: %w", id, e)
				}
				entry.bindings[id] = append(entry.bindings[id], h)
				body.Bindings[id] = append(body.Bindings[id], lexical)
			}
		}
		registry.entries = append(registry.entries, entry)
		return nil
	}
	for _, fn := range helpers.Program.Statements {
		source, ok := helpers.Functions[fn.Binding]
		if !ok {
			return nil, fmt.Errorf("actual helper declaration absent")
		}
		body := &graph.TypedSourceBody{Program: helpers.Program, Function: fn, Parameters: fn.Parameters}
		if e := add(body, source, helpers.Expressions, helpers.Statements, helpers.BindingSources); e != nil {
			return nil, e
		}
	}
	inputs, e := checked.CallableBodyInputs(p, export)
	if e != nil {
		return nil, e
	}
	for _, input := range inputs {
		if input.Program != p {
			return nil, fmt.Errorf("foreign callable Program")
		}
		out := ExtractCallableBody(input)
		if len(out.Diagnostics) > 0 {
			return nil, fmt.Errorf("actual async body: %v", out.Diagnostics)
		}
		// Stages/Flow/Body and captured instance obligations are retained together.
		async := &graph.AsyncProgram{Flow: out.Flow, Stages: out.Stages, Result: out.Result}
		body := &graph.TypedSourceBody{Program: &graph.Program{SourcePath: out.SourcePath, Shapes: out.Shapes, Statements: out.Body}, Async: async, Parameters: out.Parameters}
		source := SourceBodyNode{Node: input.Node}
		if e := add(body, source, out.Expressions, out.Statements, out.BindingSources); e != nil {
			return nil, e
		}
		body.SourceModelInstance = out.Instance
		body.Captures = out.Captures
		body.Calls = out.Calls
		for _, target := range input.Targets {
			template, e := scope.ActualTemplate(target.Node)
			if e != nil {
				return nil, e
			}
			body.Targets = append(body.Targets, graph.TypedSourceTarget{Template: template, SourceModelInstance: target.Instance, SourceModelCell: target.Cell})
		}
		sort.Slice(body.Targets, func(i, j int) bool { return body.Targets[i].Template < body.Targets[j].Template })
		body.Obligations = append(body.Obligations, out.Obligations...)
		for _, pending := range out.SourceObligations {
			body.Obligations = append(body.Obligations, pending.Diagnostic.Message)
		}
	}
	view := scope.View()
	registry.revision = view.Revision
	for _, entry := range registry.entries {
		entry.body.Registry = view
		stamp := &graph.SourceRecoveryStamp{Scope: view.Scope, Revision: view.Revision, SnapshotFingerprint: view.SnapshotFingerprint}
		entry.body.Program.SourceRecovery = stamp
		entry.body.Program.MarkSourceOnly()
		if entry.body.Async != nil {
			entry.body.Async.Module = entry.body.Program
		}
		entry.programStatements, entry.programExpressions = programMembers(entry.body.Program)
		for x := range entry.programExpressions {
			x.MarkSourceOnly()
		}
		for s := range entry.programStatements {
			s.MarkSourceOnly()
		}
		for x := range typedBodyExpressions(entry.body) {
			x.MarkSourceOnly()
		}
		for s := range typedBodyStatements(entry.body) {
			s.MarkSourceOnly()
		}
		entry.digest, e = typedBodyDigest(entry.body)
		if e != nil {
			return nil, e
		}
	}
	for _, entry := range registry.entries {
		entry.integrity, e = captureTypedGraphIntegrity(entry.body)
		if e != nil {
			return nil, e
		}
	}
	return registry, nil
}

func typedBodyExpressions(body *graph.TypedSourceBody) map[*graph.Expression]bool {
	seen := map[*graph.Expression]bool{}
	seenStatements := map[*graph.Statement]bool{}
	var expr func(*graph.Expression)
	var stmts func([]*graph.Statement)
	params := func(pp []graph.Parameter) {
		for _, p := range pp {
			expr(p.Default)
		}
	}
	expr = func(x *graph.Expression) {
		if x == nil || seen[x] {
			return
		}
		seen[x] = true
		for _, c := range []*graph.Expression{x.Left, x.Right, x.Operand, x.Callee, x.Receiver, x.Index} {
			expr(c)
		}
		for _, c := range x.Arguments {
			expr(c)
		}
		for _, c := range x.Expressions {
			expr(c)
		}
		for _, p := range x.Properties {
			expr(p.Value)
		}
		params(x.Parameters)
		stmts(x.Body)
	}
	stmts = func(ss []*graph.Statement) {
		for _, s := range ss {
			if s == nil || seenStatements[s] {
				continue
			}
			seenStatements[s] = true
			expr(s.Value)
			expr(s.Condition)
			expr(s.Increment)
			params(s.Parameters)
			for _, x := range s.Arguments {
				expr(x)
			}
			if s.Producer != nil {
				expr(s.Producer.Receiver)
				for _, x := range s.Producer.Arguments {
					expr(x)
				}
			}
			stmts(s.Init)
			stmts(s.Then)
			stmts(s.Else)
			stmts(s.Body)
			if s.Protected != nil {
				stmts(s.Protected.Try)
				stmts(s.Protected.Catch)
				stmts(s.Protected.Finally)
			}
		}
	}
	for _, call := range body.Calls {
		expr(call.Expression)
	}
	params(body.Parameters)
	if body.Function != nil {
		stmts(body.Function.Body)
	}
	if body.Async != nil {
		for _, s := range body.Async.Stages {
			stmts(s.Before)
			if s.Await.Producer != nil {
				expr(s.Await.Producer.Receiver)
				for _, x := range s.Await.Producer.Arguments {
					expr(x)
				}
			}
			expr(s.Await.Argument)
			expr(s.Await.Promise)
		}
		if body.Async.Flow != nil {
			stmts(body.Async.Flow.Body)
			for _, b := range body.Async.Flow.Blocks {
				stmts(b.Before)
				expr(b.Condition)
			}
		}
	}
	return seen
}

func (r *ScopedTypedBodies) StatementSource(h ScopedTypedBodyHandle, s *graph.Statement) (checked.ScopedSourceHandle, error) {
	if _, e := r.resolve(h); e != nil {
		return checked.ScopedSourceHandle{}, e
	}
	source, ok := r.entries[h.index].statements[s]
	if !ok {
		return checked.ScopedSourceHandle{}, fmt.Errorf("statement lacks original source membership")
	}
	return source, nil
}
func typedBodyStatements(body *graph.TypedSourceBody) map[*graph.Statement]bool {
	seen := map[*graph.Statement]bool{}
	var visit func([]*graph.Statement)
	visit = func(ss []*graph.Statement) {
		for _, s := range ss {
			if s == nil || seen[s] {
				continue
			}
			seen[s] = true
			visit(s.Init)
			visit(s.Then)
			visit(s.Else)
			visit(s.Body)
			if s.Protected != nil {
				visit(s.Protected.Try)
				visit(s.Protected.Catch)
				visit(s.Protected.Finally)
			}
		}
	}
	if body.Function != nil {
		visit([]*graph.Statement{body.Function})
	}
	if body.Async != nil {
		visit(body.Program.Statements)
		for _, s := range body.Async.Stages {
			visit(s.Before)
		}
		if body.Async.Flow != nil {
			visit(body.Async.Flow.Body)
			for _, b := range body.Async.Flow.Blocks {
				visit(b.Before)
			}
		}
	}
	for x := range typedBodyExpressions(body) {
		visit(x.Body)
	}
	return seen
}

// ScopedGraphBinding requires both the owning body and its local graph slot.
// The same integer in another body/scope is not this binding capability.
type ScopedGraphBinding struct {
	owner   *ScopedTypedBodies
	body    int
	binding graph.BindingID
}

func (r *ScopedTypedBodies) BindingHandle(h ScopedTypedBodyHandle, id graph.BindingID) (ScopedGraphBinding, error) {
	if _, e := r.BindingSources(h, id); e != nil {
		return ScopedGraphBinding{}, e
	}
	return ScopedGraphBinding{r, h.index, id}, nil
}
func (r *ScopedTypedBodies) BindingOrigins(h ScopedTypedBodyHandle, b ScopedGraphBinding) ([]checked.ScopedSourceHandle, error) {
	if b.owner != r || h.owner != r || b.body != h.index {
		return nil, fmt.Errorf("foreign graph-local binding handle")
	}
	return r.BindingSources(h, b.binding)
}
func programMembers(p *graph.Program) (map[*graph.Statement]bool, map[*graph.Expression]bool) {
	root := &graph.Statement{Body: p.Statements}
	body := &graph.TypedSourceBody{Function: root}
	statements := typedBodyStatements(body)
	delete(statements, root)
	return statements, typedBodyExpressions(body)
}

// BodySource returns the original function-like source node capability.
func (r *ScopedTypedBodies) BodySource(h ScopedTypedBodyHandle) (checked.ScopedSourceHandle, error) {
	if _, e := r.resolve(h); e != nil {
		return checked.ScopedSourceHandle{}, e
	}
	return r.entries[h.index].source, nil
}
