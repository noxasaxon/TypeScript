# TSOX TypeScript tracking fork

This checkout is `noxasaxon/TypeScript`, branch `tsox`, used by TSOX.
The outer repository's `AGENTS.md`, `CONTEXT.md`, and current task define
the development workflow.

- Authorized implementation changes belong in `tsc/tsox/` and
  `tsc/internal/tsox/`. This root `AGENTS.md` is also maintained by TSOX.
- Keep all other upstream files unchanged.
- Commit on `tsox`, push to `noxasaxon/TypeScript`, and update the outer
  repository's gitlink at an integrated checkpoint.
- Run focused workspace checks and the integration gate from the outer
  repository using its `mise.toml`.
