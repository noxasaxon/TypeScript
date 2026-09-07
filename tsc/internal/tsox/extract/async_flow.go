package extract

import "github.com/microsoft/typescript-go/tsox/graph"

func asyncContainsAwait(ss []*graph.Statement) bool {
	for _, s := range ss {
		if s.Kind == graph.StatementAsyncAwait || asyncContainsAwait(s.Then) || asyncContainsAwait(s.Else) {
			return true
		}
	}
	return false
}
func asyncConditionalAwait(ss []*graph.Statement) bool {
	for _, s := range ss {
		if s.Kind == graph.StatementIf && (asyncContainsAwait(s.Then) || asyncContainsAwait(s.Else)) {
			return true
		}
	}
	return false
}

func buildAsyncFlow(body []*graph.Statement, stages []graph.AsyncStage) *graph.AsyncFlow {
	flow := &graph.AsyncFlow{Body: body}
	operations := map[graph.BindingID]int{}
	for i, stage := range stages {
		operations[stage.Await.Binding] = i
	}
	add := func(block graph.AsyncBlock) int {
		id := len(flow.Blocks)
		flow.Blocks = append(flow.Blocks, block)
		return id
	}
	// Construct each suffix once, then point both branch arms at its entry.
	var block func([]*graph.Statement, int) int
	block = func(ss []*graph.Statement, next int) int {
		var before []*graph.Statement
		for i, s := range ss {
			if s.Kind == graph.StatementAsyncAwait {
				successor := block(ss[i+1:], next)
				return add(graph.AsyncBlock{Before: before, Await: operations[s.Binding], Next: successor, Then: -1, Else: -1})
			}
			if s.Kind == graph.StatementIf && (asyncContainsAwait(s.Then) || asyncContainsAwait(s.Else)) {
				successor := block(ss[i+1:], next)
				left, right := block(s.Then, successor), block(s.Else, successor)
				return add(graph.AsyncBlock{Before: before, Await: -1, Condition: s.Condition, Next: -1, Then: left, Else: right})
			}
			before = append(before, s)
			if s.Kind == graph.StatementReturn || s.Kind == graph.StatementThrow {
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
		b := a.Flow.Blocks[id]
		if b.Condition != nil {
			visit(b.Then)
			visit(b.Else)
		} else {
			visit(b.Next)
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
	a.Flow.Entry = ids[a.Flow.Entry]
	a.Flow.Blocks = blocks
	a.Stages = stages
}
