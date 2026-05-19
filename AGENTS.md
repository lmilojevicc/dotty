# Agent Notes

## Commands

- Build the CLI with `mise run build`; the root `dotty` binary is gitignored.
- `mise.toml` is the single source of truth for contributor tool versions and task commands; run `mise trust && mise install` before local checks.
- If `mise` is unavailable, inspect `mise.toml` and run the underlying commands defined there.
- Agents must use mise tasks for formatting, linting, vetting, and testing checks instead of invoking formatter, linter, vet, or test tools directly.
- Format Go and YAML files with `mise run fmt`; check formatting with `mise run fmt:check`.
- Run all tests with `mise run test`.
- Run focused tests with `mise run test:focused ./internal/dotty TestName` or `mise run test:focused ./internal/cli TestName`.
- Keep editor Go format-on-save aligned with `.golangci.yml`: local imports use module path/prefix `dotty` and are grouped after third-party imports.
- Agents must use `mise run verify` for final verification; this runs format check, lint, vet, tests, and build.
- Run focused checks with `mise run lint`, `mise run vet`, `mise run test`, `mise run test:focused`, or `mise run build`.
- Run vulnerability checks with `mise run vuln` when dependency or security-sensitive code changes.
- CI installs tools through mise and runs the same mise tasks on pushes to `main` and pull requests; there is no Makefile.

## Code Layout

- This is a single Go module (`module github.com/lmilojevicc/dotty`, Go 1.26) with packages `cmd/dotty`, `internal/cli`, and `internal/dotty`.
- `cmd/dotty/main.go` only constructs `cli.NewRootCommand`; Cobra flags and rendering live in `internal/cli`.
- When adding or changing Cobra commands and flags, also decide and test shell completion behavior, including dynamic Package or Collection completions when arguments reference Manifest inventory.
- Core behavior lives in `internal/dotty`: manifest/config/path handling plus add/track/untrack/link/unlink/status/list and rollback helpers.
- Repository resolution order is `--repo`, then `DOTTY_REPO`, then `~/.config/dotty/config.toml` via `XDG_CONFIG_HOME` when set.

## Dotty Semantics

- Keep terminology aligned with `CONTEXT.md`; use terms like Dotfiles Repository, Package, Package Source, Target Path, Link Mapping, Manifest, and Collection.
- Manifest state is explicit `dotty.toml` Link Mappings; command writes normalize TOML through `FormatManifest` and do not preserve comments or hand formatting.
- Mutating operations should use `RunAtomic`/`Tx` rollback helpers; dry-run paths should plan and validate without writing files.
- Link creation uses absolute symlinks; non-symlink content at a Target Path is a Conflict unless `--force` is explicit.
- Default `unlink` removes only expected Dotty Links and leaves target paths absent; `unlink --leave-copy` writes target-side copies, so a later `link` sees a Conflict and needs `--force` unless the mapping is untracked.
- `track` and `untrack` are Manifest-only operations; `link --track` and `unlink --untrack` compose Manifest updates with filesystem link/unlink changes atomically.
- `status` accepts Package and Package Source selectors, infers state from filesystem plus Manifest, and scans the Dotfiles Repository for Untracked Repository Content; `list` reports Manifest inventory only and accepts Package selectors only.

## Tests

- Filesystem tests must isolate `HOME`, `XDG_CONFIG_HOME`, and `DOTTY_REPO`; follow helpers in `internal/cli/root_test.go` and `internal/dotty/test_helpers_test.go` instead of touching real user paths.
- CLI output tests assert exact strings; most strip ANSI, but `TestStatusRenderingKeepsLipglossStylesWithoutBorders` expects lipgloss color codes and no table borders.
