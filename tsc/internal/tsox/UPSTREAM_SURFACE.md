# Upstream dependency surface

Every import of an internal upstream symbol by TSOX code gets an entry here:
the package/symbol, the fact we extract from it, and why. An internal import
without an entry is a defect. This file is the checklist every version-bump
review diffs against.

| Package / symbol | What we extract | Why |
|---|---|---|
| `internal/core` — `Version()` | The embedded TypeScript version string | Proof-of-wiring smoke test (`tsc/tsox.Marker`) |
