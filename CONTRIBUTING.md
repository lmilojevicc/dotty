# Contributing to Dotty

Thanks for your interest in contributing to Dotty! This guide covers the
essentials for getting a change landed. Dotty is a CLI that syncs configuration
(dotfiles) across machines using an explicit `dotty.toml` manifest.

## Prerequisites

- [Go](https://go.dev/) 1.26+ (per `go.mod`).
- [mise](https://mise.jdx.dev/) — the single source of truth for tool versions
  and task commands.

After cloning, run:

```sh
mise trust && mise install
```

This installs the exact Go, linter, and tool versions pinned in `mise.toml`.
All checks below are run through `mise` tasks, never by invoking the underlying
tools directly, so contributors share one environment.

## Daily workflow

1. Create a branch from `main`.
2. Make your change, keeping it small and focused.
3. Run the full verification gate (see [Verification](#verification)).
4. Open a pull request against `main` and fill in the PR template.
5. Address review feedback.

`main` is protected: changes go through pull requests, the `verify` status check
must pass, and linear history is enforced.

## Code layout

This is a single Go module (`github.com/lmilojevicc/dotty`) with three packages:

- `cmd/dotty` — entrypoint; `main.go` only constructs `cli.NewRootCommand`.
- `internal/cli` — Cobra commands, flags, and rendering.
- `internal/dotty` — core behavior: manifest/config/path handling and the
  add/track/untrack/link/unlink/status/list and rollback helpers.

Keep concerns in the right package: Cobra flag wiring belongs in
`internal/cli`; behavior and state belong in `internal/dotty`.

## Style

- Format Go and YAML with `mise run fmt`; check with `mise run fmt:check`.
- Group imports as third-party first, then local imports using the module
  prefix `dotty` (e.g. `dotty/internal/dotty`), matching `.golangci.yml`.
- Match the surrounding code's conventions. Prefer boring, minimal changes.
- Keep terminology aligned with `CONTEXT.md` (Package, Package Source, Target
  Path, Link Mapping, Manifest, Collection, etc.).

## Verification

`mise run verify` is the local pre-merge gate and mirrors CI. Run it before
pushing:

```sh
mise run verify
```

Individual checks:

| Task | Command | What it does |
| --- | --- | --- |
| Build | `mise run build` | Builds the `dotty` binary |
| Format | `mise run fmt` | Formats Go and YAML |
| Format check | `mise run fmt:check` | Fails if formatting is stale |
| Lint | `mise run lint` | Runs `golangci-lint` |
| Vet | `mise run vet` | Runs `go vet` |
| Test | `mise run test` | Runs `go test ./...` |
| Focused test | `mise run test:focused ./internal/cli TestName` | Non-skipped executed tests matching the full selection |
| Vulnerabilities | `mise run vuln` | Runs `govulnncheck` |

Focused tests take exactly two positional arguments: one Go package argument
(including package patterns such as `./...`) and one Go `-run` pattern. Quote
patterns containing shell metacharacters. There are no additional forwarded
positional arguments or flags. Normal Go environment settings still apply;
explicit `-run`, `-json`, and `-count=1` ensure the requested selection is observed
without reusing cached execution evidence.

The runner prints `executed: PACKAGE TEST` for matching, non-skipped tests with
both start and completion events, including subtests. It fails when none satisfy
the full selection (a parent running for a missing child does not count), when
Go fails, or when execution output cannot be verified. Names aggregate across
the package argument; a package with no matches does not invalidate matches in
another completed package. Every started package must have a terminal event,
even when it contributes no matching tests. Go's hierarchical matching and first-matching alternation
semantics apply, not a regular expression against the entire test path.
Focused checks are iteration evidence only; their own real-Go regressions run
in the ordinary `mise run test` and `mise run verify` gates.

The focused runner currently supports Linux and macOS; other platforms refuse
before starting Go (this does not change Dotty's application platform support).
SIGINT/SIGTERM sent to the runner or task-wrapper PID cooperatively stops its
owned build/test process groups, escalates after a bounded grace period, and
returns 130/143 after waiting for its direct children. The wrapper retains a Bash
group leader from build through runner completion; it forwards only to that
reserved group, never to a potentially reaped runner PID. A private readiness
acknowledgment defers startup cancellation until the runner subscribes to signals.
Forwarding stops before the wrapper releases and waits for its anchor and
synchronously removes its private temporary directory. Signals after that finish
boundary do not change the completed result. This is not a process sandbox:
externally killed anchors, descendants that detach or escape their groups,
blocked output sinks, and uninterruptible kernel tasks are outside this
cooperative ownership and cancellation guarantee.

## Tests

- Filesystem tests must isolate `HOME`, `XDG_CONFIG_HOME`, and `DOTTY_REPO`.
  Follow the helpers in `internal/cli/root_test.go` and
  `internal/dotty/test_helpers_test.go` — never touch real user paths.
- CLI output tests assert exact strings. Most strip ANSI, but
  `TestStatusRenderingKeepsLipglossStylesWithoutBorders` expects lipgloss color
  codes and no table borders.
- When you fix a bug or add logic, add or extend a test that would fail without
  your change.

## Project conventions

- Mutating operations use the `RunAtomic`/`Tx` rollback helpers. Dry-run paths
  should plan and validate without writing files.
- Links are absolute symlinks. Non-symlink content at a Target Path is a
  Conflict unless `--force` is explicit.
- `track` and `untrack` are Manifest-only operations; `link --track` and
  `unlink --untrack` compose manifest updates with filesystem changes
  atomically.
- Repository resolution order is `--repo`, then `DOTTY_REPO`, then
  `~/.config/dotty/config.toml` via `XDG_CONFIG_HOME` when set.
- `status` accepts Package and Package Source selectors; `list` accepts Package
  selectors only.
- When adding or changing Cobra commands and flags, also consider shell
  completion behavior, including dynamic Package or Collection completions for
  manifest inventory.

## Reporting issues

Use the GitHub issue templates (bug report or feature request). For security
issues, do not open a public issue — see `SECURITY.md` if present or contact the
maintainer directly.

## License

By contributing, you agree that your contributions are licensed under the MIT
License (see `LICENSE`).
