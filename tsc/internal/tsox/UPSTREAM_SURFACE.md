# Upstream dependency surface

Every import of an internal upstream symbol by TSOX code gets an entry here:
the package/symbol, the fact we extract from it, and why. An internal import
without an entry is a defect. This file is the checklist every version-bump
review diffs against.

| Package / symbol | What we extract | Why |
|---|---|---|
| `internal/core` — `Version()` | The embedded TypeScript version string | Proof-of-wiring smoke test (`tsc/tsox.Marker`) |
| `internal/ast` — `Node`, `SourceFile`, `Kind`, `Node.ForEachChild`, node/source-file position accessors | Syntax-tree shape and source byte ranges | Phase-one compile-backed facts for recursive traversal and source provenance |
| `internal/bundled` — `WrapFS`, `LibPath` | The compiler's embedded default-library filesystem | Construct checked in-memory programs without host filesystem dependencies |
| `internal/compiler` — `NewCompilerHost`, `NewProgram`, `ProgramOptions`, program binding/source-file/semantic-diagnostic methods | A bound, semantically checked single-threaded program | The verified checker-driving recipe used by extractor tests and implementation |
| `internal/core` — compiler options/tristates and `UTF16Offset` | Strict/single-threaded configuration and diagnostic column units | Keep one checker identity domain and report TypeScript-compatible source columns |
| `internal/scanner` — `GetTokenPosOfNode`, `GetECMALineAndUTF16CharacterOfPosition` | A construct's trivia-free token start and zero-based line/UTF-16 column | Fence diagnostics name the source position users see |
| `internal/tsoptions` — `NewParsedCommandLine` | Compiler configuration for an explicit root file | Drive one source file without inventing tsconfig behavior |
| `internal/tspath` — `ComparePathsOptions` | Case-sensitive path normalization rooted at the in-memory source directory | Keep program and virtual-filesystem path identity consistent |
| `internal/vfs/vfstest` — `FromMap` | In-memory source files | Extractor input stays deterministic and does not touch the host filesystem |
