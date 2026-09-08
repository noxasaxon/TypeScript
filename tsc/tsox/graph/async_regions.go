package graph

import "fmt"

// AsyncProtectedRegion retains one lexical try statement. Each body executes in
// its original scope; preceding statements do not acquire this region's handlers.
type AsyncProtectedRegion struct {
	Position             Position
	Try, Catch, Finally  []*Statement
	HasCatch, HasFinally bool
	CatchBinding         *Parameter
}

const StatementAsyncProtected StatementKind = "async-protected"

type AsyncRegionPhase string

const (
	AsyncRegionBody    AsyncRegionPhase = "body"
	AsyncRegionTry     AsyncRegionPhase = "try"
	AsyncRegionCatch   AsyncRegionPhase = "catch"
	AsyncRegionFinally AsyncRegionPhase = "finally"
)

type AsyncCompletionKind string

const (
	AsyncNormal AsyncCompletionKind = "normal"
	AsyncReturn AsyncCompletionKind = "return"
	AsyncThrow  AsyncCompletionKind = "throw"
	// AsyncResume is a control action, never a source completion value.
	AsyncResume AsyncCompletionKind = "resume-pending"
)

// AsyncRegion describes handler entries and the normal suffix of one lexical
// region. Index zero is the callable's unprotected body. A finally entry owns the
// pending completion until its normal resume or replacement by an abrupt exit.
type AsyncRegion struct {
	Parent                                   int
	ParentPhase                              AsyncRegionPhase
	TryEntry, CatchEntry, FinallyEntry, Next int
	CatchBinding                             *Parameter
}
type AsyncRouteKind string

const (
	AsyncRouteExit    AsyncRouteKind = "exit"
	AsyncRouteNext    AsyncRouteKind = "next"
	AsyncRouteCatch   AsyncRouteKind = "catch"
	AsyncRouteFinally AsyncRouteKind = "finally"
)

type AsyncRoute struct {
	Kind   AsyncRouteKind
	Target int
	Region int
}

// RouteCompletion selects one immediate control transfer. Native emission uses
// it with an owning completion value; reachability and liveness use the same
// targets. Catch handles only a throw from its try. A finally cannot catch itself.
func (f *AsyncFlow) RouteCompletion(region int, phase AsyncRegionPhase, kind AsyncCompletionKind, next int) AsyncRoute {
	if region == 0 {
		if kind == AsyncNormal && next >= 0 {
			return AsyncRoute{Kind: AsyncRouteNext, Target: next}
		}
		return AsyncRoute{Kind: AsyncRouteExit, Target: -1}
	}
	r := f.Regions[region]
	if phase == AsyncRegionFinally {
		return f.AfterFinally(region, kind, next)
	}
	if phase == AsyncRegionTry && kind == AsyncThrow && r.CatchEntry >= 0 {
		return AsyncRoute{Kind: AsyncRouteCatch, Target: r.CatchEntry, Region: region}
	}
	if r.FinallyEntry >= 0 {
		return AsyncRoute{Kind: AsyncRouteFinally, Target: r.FinallyEntry, Region: region}
	}
	return f.AfterFinally(region, kind, next)
}

// AfterFinally consumes the saved completion after this region's cleanup. Normal
// completion enters this try statement's suffix; it does not exit the parent.
func (f *AsyncFlow) AfterFinally(region int, kind AsyncCompletionKind, next int) AsyncRoute {
	if kind == AsyncNormal {
		if next >= 0 {
			return AsyncRoute{Kind: AsyncRouteNext, Target: next}
		}
		return AsyncRoute{Kind: AsyncRouteExit, Target: -1}
	}
	if region == 0 {
		return AsyncRoute{Kind: AsyncRouteExit, Target: -1}
	}
	r := f.Regions[region]
	return f.RouteCompletion(r.Parent, r.ParentPhase, kind, next)
}

// Successors includes normal, exception and pending-completion routes. It is a
// conservative reachability set, not a claim that all completions occur. One
// lexical finally site remains shared instead of cloning its source expressions.
func (f *AsyncFlow) Successors(id int) []int {
	b := f.Blocks[id]
	out := []int{}
	add := func(n int) {
		if n < 0 {
			return
		}
		for _, old := range out {
			if old == n {
				return
			}
		}
		out = append(out, n)
	}
	if b.Completion == AsyncResume {
		r := f.Regions[b.Region]
		for _, kind := range []AsyncCompletionKind{AsyncNormal, AsyncReturn, AsyncThrow} {
			add(f.AfterFinally(b.Region, kind, r.Next).Target)
		}
	} else if b.Completion == AsyncNormal {
		add(f.RouteCompletion(b.Region, b.Phase, AsyncNormal, b.Next).Target)
	} else if b.Condition != nil {
		add(b.Then)
		add(b.Else)
	} else {
		add(b.Next)
	}
	if len(f.Regions) > 0 {
		add(f.RouteCompletion(b.Region, b.Phase, AsyncReturn, -1).Target)
		add(f.RouteCompletion(b.Region, b.Phase, AsyncThrow, -1).Target)
	}
	return out
}

// ValidateRegions rejects malformed control targets before shared route queries.
func (f *AsyncFlow) ValidateRegions() error {
	if f == nil {
		return fmt.Errorf("missing async flow")
	}
	valid := func(id int) bool { return id >= -1 && id < len(f.Blocks) }
	for id, r := range f.Regions {
		if id == 0 {
			if r.Parent != -1 {
				return fmt.Errorf("root protected region has a parent")
			}
			continue
		}
		if r.Parent < 0 || r.Parent >= id {
			return fmt.Errorf("protected region %d has invalid parent", id)
		}
		if r.Parent == 0 && r.ParentPhase != AsyncRegionBody || r.Parent != 0 && r.ParentPhase != AsyncRegionTry && r.ParentPhase != AsyncRegionCatch && r.ParentPhase != AsyncRegionFinally {
			return fmt.Errorf("protected region %d has invalid parent phase", id)
		}
		for _, target := range []int{r.TryEntry, r.CatchEntry, r.FinallyEntry, r.Next} {
			if !valid(target) {
				return fmt.Errorf("protected region %d has invalid target", id)
			}
		}
	}
	for id, b := range f.Blocks {
		if b.Region < 0 || b.Region >= len(f.Regions) && b.Region != 0 {
			return fmt.Errorf("async block %d has invalid protected region", id)
		}
		if len(f.Regions) > 0 {
			if b.Region == 0 && b.Phase != AsyncRegionBody || b.Region != 0 && b.Phase != AsyncRegionTry && b.Phase != AsyncRegionCatch && b.Phase != AsyncRegionFinally {
				return fmt.Errorf("async block %d has invalid protected phase", id)
			}
		}
		switch b.Completion {
		case "":
		case AsyncNormal:
			if b.Region == 0 || b.Phase == AsyncRegionFinally {
				return fmt.Errorf("async block %d has invalid normal region completion", id)
			}
		case AsyncResume:
			if b.Region == 0 || b.Phase != AsyncRegionFinally {
				return fmt.Errorf("async block %d has invalid pending completion resume", id)
			}
		default:
			return fmt.Errorf("async block %d has unknown completion", id)
		}
		if b.Completion != "" && (len(b.Before) > 0 || b.Condition != nil || b.Await >= 0) {
			return fmt.Errorf("async block %d combines completion dispatch with source work", id)
		}
	}
	return nil
}
