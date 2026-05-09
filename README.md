<!-- prettier-ignore -->
<div align="center">

# Dotty

_Manage dotfiles with explicit TOML link mappings._

[Overview](#overview) | [Features](#features) | [Getting Started](#getting-started) | [Workflow](#basic-workflow) | [Manifest](#manifest) | [Commands](#commands) | [Development](#development)

</div>

Dotty is a Go CLI for managing your configuration files. It stores managed content as **Packages** in a **Dotfiles Repository**, records each **Package Source** to **Target Path** relationship in `dotty.toml`, and creates absolute symlinks back to the repository.

It is intentionally explicit: Dotty does not infer state from directory layout, hide metadata in target folders, or guess whether local files are safe to replace.

## Overview

Dotty is built around a simple model:

- A **Dotfiles Repository** contains `dotty.toml` and one directory per Package.
- A **Package** is an installable group of one or more Link Mappings.
- A **Link Mapping** connects a package-relative source to an absolute or home-relative Target Path.
- A **Collection** is an explicit user-defined list of Packages that can be linked or unlinked together.

This makes status, conflict detection, unlink behavior, and migration from symlink-based setups deterministic.

## Features

- **Explicit manifest**: Every managed target is recorded in `dotty.toml` as a source-to-target Link Mapping.
- **Safe by default**: Non-symlink content at a Target Path is a Conflict unless you explicitly pass `--force`.
- **Dry runs**: Preview `add`, `link`, and `unlink` operations without changing files.
- **Collections**: Link or unlink named groups of Packages without adding OS-specific inference rules.
- **Status reporting**: See linked, unlinked, partial, conflict, missing-source, empty, and untracked states.
- **Soft and hard unlink**: Leave a target-side copy by default, or remove only expected Dotty Links with `--hard`.
- **Rollback-based writes**: Mutating commands roll back changes already made when a normal error occurs.
- **Symlink adoption**: Adopt existing symlinks, including common stow-style layouts, without moving unrelated repositories.

## Getting Started

### Prerequisites

- [Go 1.22+](https://go.dev/dl/)
- [mise](https://mise.jdx.dev/) for contributor tooling and local verification tasks

### Build From Source

```bash
go build -o dotty ./cmd/dotty
./dotty --help
```

The root `dotty` binary is ignored by git, so rebuilding locally will not dirty the repository.

## Basic Workflow

```bash
# Initialize a Dotfiles Repository and remember it as the default
dotty init ~/dotfiles

# Move ~/.config/tmux into ~/dotfiles/tmux and link it back
dotty add ~/.config/tmux tmux

# Preview the same add operation without writing files
dotty add --dry-run ~/.config/tmux tmux

# Link or unlink a Package
dotty link tmux
dotty unlink tmux

# Remove only expected Dotty Links without leaving target-side copies
dotty unlink --hard tmux

# Link or unlink every Package in the Manifest
dotty link --all
dotty unlink --all

# Inspect Manifest inventory and filesystem state
dotty list
dotty status
dotty status --verbose
```

> [!WARNING]
> `dotty link --force` destructively replaces Target Path Conflicts before creating Links. Use `dotty link --force --dry-run <package>` first when you are not certain what will be replaced.

## Manifest

Dotty stores repository state in `dotty.toml` at the root of the Dotfiles Repository.

```toml
version = 1

[packages.tmux]
links = [
  { source = ".", target = "~/.config/tmux" },
]

[packages.zsh]
links = [
  { source = ".zshrc", target = "~/.zshrc" },
  { source = ".zshrc_secrets", target = "~/secrets/.zshrc_secrets" },
]

[collections.terminal]
packages = ["tmux", "zsh"]
```

Collections are explicit package lists:

```bash
dotty link --collection terminal
dotty unlink --collection terminal
```

`--all` cannot be combined with package names or `--collection`.

> [!NOTE]
> Dotty normalizes `dotty.toml` when commands write the Manifest. Hand formatting and comments in the Manifest are not preserved.

## Commands

| Command                      | Purpose                                                              | Useful flags                                    |
| ---------------------------- | -------------------------------------------------------------------- | ----------------------------------------------- |
| `dotty init [path]`          | Initialize a Dotfiles Repository and remember it as the default.     | None                                            |
| `dotty add PATH PACKAGE`     | Adopt an existing file, directory, or symlink target into a Package. | `--dry-run`                                     |
| `dotty link [packages...]`   | Create Links for selected Packages.                                  | `--all`, `--collection`, `--force`, `--dry-run` |
| `dotty unlink [packages...]` | Remove Links for selected Packages.                                  | `--all`, `--collection`, `--hard`, `--dry-run`  |
| `dotty status [packages...]` | Show package state inferred from the Manifest and filesystem.        | `--verbose`                                     |
| `dotty list`                 | List Packages and Collections defined in the Manifest.               | None                                            |

All commands accept the global `--repo` flag when they need to operate on a specific Dotfiles Repository.

## Status States

- `LINKED`: the Target Path is a symlink to the expected Package Source.
- `UNLINKED`: the Package Source exists and the Target Path does not exist.
- `CONFLICT`: the Target Path exists as non-symlink content or points to another source.
- `MISSING SOURCE`: the Manifest references a Package Source that does not exist.
- `EMPTY`: the Package has no Link Mappings.
- `PARTIAL`: a multi-mapping Package has mixed linked/unlinked states.
- `UNTRACKED`: repository content is not represented in the Manifest.

## Repository Selection

Dotty resolves the Dotfiles Repository in this order:

1. The `--repo` command flag.
2. The `DOTTY_REPO` environment variable.
3. `~/.config/dotty/config.toml`, or `$XDG_CONFIG_HOME/dotty/config.toml` when `XDG_CONFIG_HOME` is set.

`dotty init PATH` creates the Manifest if needed, validates any existing Manifest, and records the Default Repository in the user config file.

## Stow-Style Migration

If an existing symlink already points to the intended Package Source inside the Dotfiles Repository, `dotty add` adopts it in place:

```bash
dotty init ~/Clone/dot-example
dotty add ~/.config/tmux tmux
```

If the symlink points to an external stow repository, Dotty copies the resolved content into the Dotfiles Repository and leaves the old repository intact.

## Development

Contributor tool versions and tasks are defined in `mise.toml`.

```bash
mise trust && mise install

mise run fmt
mise run verify
```

`mise run verify` runs formatting checks, linting, `go vet`, tests, and a CLI build. CI runs the same checks on pull requests and pushes to `main`.

Dotty is built with [Cobra](https://github.com/spf13/cobra) for CLI commands, [go-toml](https://github.com/pelletier/go-toml) for Manifest parsing, [Lip Gloss](https://github.com/charmbracelet/lipgloss) for terminal styling, and Go's standard library for filesystem operations plus Dotty's rollback layer.

For terminology and design rationale, see [`CONTEXT.md`](CONTEXT.md) and the ADRs in [`docs/adr`](docs/adr).
