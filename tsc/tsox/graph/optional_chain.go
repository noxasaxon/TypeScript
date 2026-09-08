package graph

// OptionalLink records source syntax, not a declaration-derived nullish proof.
// Grouping terminates ContinuesReceiverChain even when the receiver is itself
// the result of another optional chain.
type OptionalLink struct {
	CheckReceiver          bool
	ContinuesReceiverChain bool
}
