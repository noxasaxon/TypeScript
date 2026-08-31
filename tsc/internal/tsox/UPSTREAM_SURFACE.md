# Upstream dependency surface

Every import of an internal upstream symbol by TSOX code gets an entry here:
the package/symbol, the fact we extract from it, and why. An internal import
without an entry is a defect. This file is the checklist every version-bump
review diffs against.

| Package / symbol | What we extract | Why |
|---|---|---|
| `internal/core` — `Version()` | The embedded TypeScript version string | Proof-of-wiring smoke test (`tsc/tsox.Marker`) |
| `internal/ast` — `Node`, `SourceFile`, `Symbol`, `ModifierList`, `Diagnostic`; `Node.ForEachChild`, `Node.Text`, `Node.Name`, `Node.Pos`; `Diagnostic.File`, `Diagnostic.Pos`, `Diagnostic.String`; `GetSourceFileOfNode`; `Symbol.Declarations`; the guarded `As*` accessors and fields for source files, blocks, variable/parameter/function declarations, expression/return/if/while/for statements, identifiers/literals, binary/prefix/postfix/call/property-access/template/arrow/parenthesized expressions; the corresponding `Kind*` constants; `NodeFlagsBlockScoped`, `NodeFlagsLet`, `NodeFlagsConst`; `SymbolFlagsValue` | Checked syntax shape, lexical symbol identity and declaration provenance, declaration mutability, literal/operator data, source byte ranges, and TypeScript diagnostic file/position/message data | Build the closed graph allow-list, admit calls only to source-owned functions, and preserve binding identity without retaining AST or symbol pointers; phase-one tests also pin the recursive traversal pattern |
| `internal/bundled` — `WrapFS`, `LibPath` | The compiler's embedded default-library filesystem | Construct checked in-memory programs without host filesystem dependencies |
| `internal/checker` — `Checker`, `Type`, `SignatureKindCall`, `TypeFlagsNumberLike`, `TypeFlagsStringLike`, `TypeFlagsBooleanLike`, `TypeFlagsVoidLike`; `GetTypeAtLocation`, `GetSymbolAtLocation`, `GetGlobalSymbol`, `GetSignaturesOfType`, `GetTypeOfSymbol`, `GetReturnTypeOfSignature`, `TypeToString`; `Type.Flags`; `Signature.Parameters` | Checked primitive/function types, resolved lexical bindings, the real global `console`, and callable parameter/result shapes | Make overloaded operations and calls semantic before they cross the fork seam, and never confuse a shadowed `console` binding with the supported builtin |
| `internal/compiler` — `NewCompilerHost`, `NewProgram`, `ProgramOptions`; `Program.BindSourceFiles`, `GetSourceFile`, `GetSourceFiles`, `GetSyntacticDiagnostics`, `GetSemanticDiagnostics`, `GetTypeChecker` | A bound, syntactically and semantically checked single-threaded program plus its one checker | Use the compile-verified embedding recipe and keep every type/symbol identity in one checker domain |
| `internal/core` — `CompilerOptions`, `TSTrue`, `ModuleDetectionKindForce`, `UTF16Offset` | Strict/single-threaded/module-scoped configuration and diagnostic column units | Keep one checker identity domain, prevent bundled DOM script globals from colliding with program bindings, and report TypeScript-compatible source columns |
| `internal/jsnum` — `FromString` | The exact IEEE-754 value of a scanner-normalized TypeScript numeric literal | Reuse the compiler's JavaScript numeric parser instead of inventing literal conversion |
| `internal/scanner` — `GetTokenPosOfNode`, `GetECMALineAndUTF16CharacterOfPosition` | A construct's trivia-free token start and zero-based line/UTF-16 column | Fence diagnostics name the source position users see |
| `internal/tsoptions` — `NewParsedCommandLine` | Compiler configuration for an explicit root file | Drive one source file without inventing tsconfig behavior |
| `internal/tspath` — `ComparePathsOptions` | Case-sensitive path normalization rooted at the in-memory source directory | Keep program and virtual-filesystem path identity consistent |
| `internal/vfs/vfstest` — `FromMap` | In-memory source files | Extractor input stays deterministic and does not touch the host filesystem |
