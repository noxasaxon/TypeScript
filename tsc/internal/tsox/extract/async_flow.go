package extract

import "github.com/microsoft/typescript-go/tsox/graph"

func asyncContainsAwait(ss []*graph.Statement) bool {
	for _, s := range ss {
		if s.Kind == graph.StatementAsyncProtected || s.Kind == graph.StatementAsyncAwait || asyncContainsAwait(s.Then) || asyncContainsAwait(s.Else) || asyncContainsAwait(s.Body) {
			return true
		}
	}
	return false
}
func asyncStructuredFlow(ss []*graph.Statement) bool {
	for _, s := range ss {
		if s.Kind == graph.StatementAsyncProtected || s.Kind == graph.StatementFor || s.Kind == graph.StatementWhile || s.Kind == graph.StatementForOf {
			return true
		}
		if s.Kind == graph.StatementIf && (asyncNeedsFlow(s.Then) || asyncNeedsFlow(s.Else)) {
			return true
		}
	}
	return false
}

// A synchronous loop inside an async body needs the same control edges as a
// suspending one: it may return early and its carried locals still need liveness.
func asyncNeedsFlow(ss []*graph.Statement) bool {
	return asyncContainsAwait(ss) || asyncStructuredFlow(ss)
}

func buildAsyncFlow(body []*graph.Statement, stages []graph.AsyncStage) *graph.AsyncFlow {
	return buildAsyncFlowMode(body, stages, false)
}

func buildAsyncFlowMode(body []*graph.Statement, stages []graph.AsyncStage, allBranches bool) *graph.AsyncFlow {
	flow := &graph.AsyncFlow{Body: body}
	region, phase := 0, graph.AsyncRegionBody
	operations := map[graph.BindingID]int{}
	for i, stage := range stages {
		operations[stage.Await.Binding] = i
	}
	add := func(block graph.AsyncBlock) int {
		block.Region, block.Phase = region, phase
		id := len(flow.Blocks)
		flow.Blocks = append(flow.Blocks, block)
		return id
	}
	// Construct each suffix once, then point both branch arms at its entry.
	var block func([]*graph.Statement, int) int
	block = func(ss []*graph.Statement, next int) int {
		var before []*graph.Statement
		for i, s := range ss {
			if s.Kind == graph.StatementAsyncProtected {
				successor := block(ss[i+1:], next)
				if len(flow.Regions) == 0 {
					flow.Regions = append(flow.Regions, graph.AsyncRegion{Parent: -1, TryEntry: -1, CatchEntry: -1, FinallyEntry: -1, Next: -1})
				}
				outer, outerPhase := region, phase
				id := len(flow.Regions)
				r := s.Protected
				flow.Regions = append(flow.Regions, graph.AsyncRegion{Parent: outer, ParentPhase: outerPhase, TryEntry: -1, CatchEntry: -1, FinallyEntry: -1, Next: successor, CatchBinding: r.CatchBinding})
				region, phase = id, graph.AsyncRegionFinally
				finallyEntry := -1
				if r.HasFinally {
					resume := add(graph.AsyncBlock{Await: -1, Next: -1, Then: -1, Else: -1, Completion: graph.AsyncResume})
					finallyEntry = block(r.Finally, resume)
				}
				region, phase = id, graph.AsyncRegionCatch
				catchEntry := -1
				if r.HasCatch {
					normal := add(graph.AsyncBlock{Await: -1, Next: successor, Then: -1, Else: -1, Completion: graph.AsyncNormal})
					catchEntry = block(r.Catch, normal)
				}
				region, phase = id, graph.AsyncRegionTry
				normal := add(graph.AsyncBlock{Await: -1, Next: successor, Then: -1, Else: -1, Completion: graph.AsyncNormal})
				tryEntry := block(r.Try, normal)
				flow.Regions[id].TryEntry, flow.Regions[id].CatchEntry, flow.Regions[id].FinallyEntry = tryEntry, catchEntry, finallyEntry
				region, phase = outer, outerPhase
				if len(before) == 0 {
					return tryEntry
				}
				return add(graph.AsyncBlock{Before: before, Await: -1, Next: tryEntry, Then: -1, Else: -1})
			}
			if s.Kind == graph.StatementFor || s.Kind == graph.StatementWhile {
				successor := block(ss[i+1:], next)
				condition := s.Condition
				if condition == nil {
					condition = &graph.Expression{Kind: graph.ExpressionBoolean, Boolean: true, Type: graph.Type{Kind: graph.TypeBoolean}, Position: s.Position}
				}
				// Allocate the condition before its body so the update/backedge
				// reuses this entry; synchronous edges remain native dispatch.
				header := add(graph.AsyncBlock{Await: -1, Condition: condition, Next: -1, Then: -1, Else: successor})
				update := header
				if s.Increment != nil {
					update = add(graph.AsyncBlock{Before: []*graph.Statement{{Kind: graph.StatementExpression, Value: s.Increment, Position: s.Increment.Position}}, Await: -1, Next: header, Then: -1, Else: -1})
				}
				bodyEntry := block(s.Body, update)
				flow.Blocks[header].Then = bodyEntry
				entry := block(s.Init, header)
				if len(before) == 0 {
					return entry
				}
				return add(graph.AsyncBlock{Before: before, Await: -1, Next: entry, Then: -1, Else: -1})
			}
			if s.Kind == graph.StatementAsyncAwait {
				successor := block(ss[i+1:], next)
				return add(graph.AsyncBlock{Before: before, Await: operations[s.Binding], Next: successor, Then: -1, Else: -1})
			}
			if s.Kind == graph.StatementIf && (allBranches || asyncNeedsFlow(s.Then) || asyncNeedsFlow(s.Else)) {
				successor := block(ss[i+1:], next)
				left, right := block(s.Then, successor), block(s.Else, successor)
				return add(graph.AsyncBlock{Before: before, Await: -1, Condition: s.Condition, Next: -1, Then: left, Else: right})
			}
			before = append(before, s)
			if s.Kind == graph.StatementReturn || s.Kind == graph.StatementReturnPromise || s.Kind == graph.StatementUnresolvedReturn || s.Kind == graph.StatementThrow {
				next = -1
				break
			}
		}
		if len(before) == 0 {
			return next
		}
		return add(graph.AsyncBlock{Before: before, Await: -1, Next: next, Then: -1, Else: -1})
	}
	flow.Entry = block(body, -1)
	return flow
}

// Drop syntactically present but unreachable suffixes and operations together.
// Their source was still checked; no impossible continuation enters emission.
func pruneAsyncFlow(a *graph.AsyncProgram) {
	reachable := map[int]bool{}
	var visit func(int)
	visit = func(id int) {
		if id < 0 || reachable[id] {
			return
		}
		reachable[id] = true
		for _, next := range a.Flow.Successors(id) {
			visit(next)
		}
	}
	visit(a.Flow.Entry)
	ids := map[int]int{-1: -1}
	var blocks []graph.AsyncBlock
	used := map[int]bool{}
	for id, b := range a.Flow.Blocks {
		if reachable[id] {
			ids[id] = len(blocks)
			blocks = append(blocks, b)
			if b.Await >= 0 {
				used[b.Await] = true
			}
		}
	}
	stages := []graph.AsyncStage{}
	stageIDs := map[int]int{}
	for id, s := range a.Stages {
		if used[id] {
			stageIDs[id] = len(stages)
			stages = append(stages, s)
		}
	}
	for i := range blocks {
		b := &blocks[i]
		b.Next = ids[b.Next]
		b.Then = ids[b.Then]
		b.Else = ids[b.Else]
		if b.Await >= 0 {
			b.Await = stageIDs[b.Await]
		}
	}
	remap := func(id int) int {
		if next, ok := ids[id]; ok {
			return next
		}
		return -1
	}
	for i := range a.Flow.Regions {
		r := &a.Flow.Regions[i]
		r.TryEntry, r.CatchEntry, r.FinallyEntry, r.Next = remap(r.TryEntry), remap(r.CatchEntry), remap(r.FinallyEntry), remap(r.Next)
	}
	a.Flow.Entry = ids[a.Flow.Entry]
	a.Flow.Blocks = blocks
	a.Stages = stages
}
