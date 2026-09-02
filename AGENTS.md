# TSOX tracking fork of microsoft/TypeScript

This checkout is `noxasaxon/TypeScript`, branch `tsox`: TSOX's permanent
tracking fork, pinned as a submodule of the tsox repository. Upstream's
maintenance-mode notice, which this file replaced, concerns pull requests to
microsoft/TypeScript. Nothing in this tree is ever proposed upstream, so no
acknowledgement is needed and none may be requested.

Rules for coding agents working in this tree:

- Add and change files only under `tsc/tsox/` and `tsc/internal/tsox/`. This
  is authorized whenever a ratified plan in the tsox repository
  (`docs/plans/NNNN-*.md`) calls for it. Do not stop to ask.
- Never modify any other file. Upstream files stay byte-identical so the
  `tsox` branch rebases cleanly onto each upstream release tag. This file is
  the single documented exception (tsox `docs/adr/0001`).
- Commit on the `tsox` branch, push to `noxasaxon/TypeScript`, and bump the
  submodule pointer in the tsox repository once per slice.
- Build and test from the tsox repository root with `mise run ci`. The build
  notes in `.github/copilot-instructions.md` are upstream's and describe
  upstream's workflow, not ours.
