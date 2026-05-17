<!-- prettier-ignore -->
<div align="center">

# Dotty

_Sync configuration files across machines using a manifest._

[Features](#features) | [Installation](#installation) | [Workflow](#basic-workflow) | [Commands](#commands) | [Advanced](#advanced) | [Development](#development)

</div>

![banner-image](./docs/assets/dotty-cover.png)

## Features

- **Explicit manifest**: Every managed target is recorded in `dotty.toml` as a source-to-target Link Mapping.
- **Safe by default**: Non-symlink content at a Target Path is a Conflict unless you explicitly pass `--force`.
- **Dry runs**: Preview `add`, `map`, `link`, and `unlink` operations without changing files.
- **Collections**: Link or unlink named groups of Packages.
- **Status reporting**: See linked, unlinked, partial, conflict, missing-source, empty, and untracked states.
- **Soft and hard unlink**: Leave a target-side copy by default, or remove only expected Dotty Links with `--hard`.

## Installation

Install with Go:

```bash
go install github.com/lmilojevicc/dotty/cmd/dotty@latest
```

If `dotty` is not found after installing, make sure Go's binary directory is on your `PATH`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

To pin a specific release:

```bash
go install github.com/lmilojevicc/dotty/cmd/dotty@v0.1.2
```

Prebuilt archives are also available on [GitHub Releases](https://github.com/lmilojevicc/dotty/releases).

## Basic Workflow

```bash
# Initialize a Dotfiles Repository and remember it as the default
dotty init ~/dotfiles

# Move ~/.config/tmux into ~/dotfiles/tmux and link it back
dotty add ~/.config/tmux tmux

# Preview the same add operation without writing files
dotty add --dry-run ~/.config/tmux tmux

# Add another Link Mapping for an existing Package Source without touching files
dotty map tmux . ~/.tmux
dotty map --dry-run tmux . ~/.tmux

# Link or unlink a Package
dotty link --dry-run tmux
dotty link tmux
dotty unlink --dry-run tmux
dotty unlink tmux

# Remove only expected Dotty Links without leaving target-side copies
dotty unlink --hard --dry-run tmux
dotty unlink --hard tmux

# Link or unlink every Package in the Manifest
dotty link --all
dotty unlink --all

# Inspect Manifest inventory and filesystem state
dotty repo
dotty list
dotty status
dotty status --state conflict
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

Use `dotty map <package> <source> <target>` to add a Link Mapping from an existing Package Source to an additional Target Path without copying, moving, or linking filesystem content. The Package and Package Source must already exist; `map` only validates and writes the Manifest. Run `dotty link` later to create the Link, using `--force` only if that Target Path is a Conflict you intentionally want to replace.

## Commands

| Command                                                           | Purpose                                                              | Useful flags                                    |
| ----------------------------------------------------------------- | -------------------------------------------------------------------- | ----------------------------------------------- |
| `dotty init [<path>]`                                             | Initialize a Dotfiles Repository and remember it as the default.     | None                                            |
| `dotty add <path> <package>`                                      | Adopt an existing file, directory, or symlink target into a Package. | `--dry-run`                                     |
| `dotty map <package> <source> <target>`                           | Add a Manifest Link Mapping without changing files.                  | `--dry-run`                                     |
| `dotty link <package>... \| --all \| --collection <collection>`   | Create Links for selected Packages.                                  | `--all`, `--collection`, `--force`, `--dry-run` |
| `dotty unlink <package>... \| --all \| --collection <collection>` | Remove Links for selected Packages.                                  | `--all`, `--collection`, `--hard`, `--dry-run`  |
| `dotty status [<package>...]`                                     | Show package state inferred from the Manifest and filesystem.        | `--state`, `--verbose`, `-v`                    |
| `dotty list`                                                      | List Packages and Collections defined in the Manifest.               | None                                            |
| `dotty repo`                                                      | Show the resolved Dotfiles Repository and config file path.          | None                                            |
| `dotty completion <shell>`                                        | Generate shell completion scripts.                                   | `bash`, `zsh`, `fish`, `powershell`             |

All commands accept the global `--repo` flag when they need to operate on a specific Dotfiles Repository.

`dotty status` prints the resolved Dotfiles Repository, package states, untracked repository content, and a summary count. Use `--state <state>` to keep aggregate Package rows and untracked rows that match a state. Supported values are `linked`, `unlinked`, `partial`, `conflict`, `missing-source`, `empty`, and `untracked`. Use `dotty status --verbose` or `dotty status -v` for per-Link Mapping status. A single package argument, such as `dotty status tmux`, implies verbose per-Link Mapping output for that Package. Package-scoped status does not include repository-wide Untracked Repository Content.

## Status States

Dotty renders status labels in uppercase, while `--state` accepts lowercase or kebab-case filter values:

- `LINKED` (`--state linked`): the Target Path is a symlink to the expected Package Source.
- `UNLINKED` (`--state unlinked`): the Package Source exists and the Target Path does not exist.
- `PARTIAL` (`--state partial`): a Package has mixed Link Mapping states.
- `CONFLICT` (`--state conflict`): the Target Path exists as non-symlink content or points to another source.
- `MISSING SOURCE` (`--state missing-source`): the Manifest references a Package Source that does not exist.
- `EMPTY` (`--state empty`): the Package has no Link Mappings.
- `UNTRACKED` (`--state untracked`): untracked repository content is not represented in the Manifest.

## Advanced

### Shell Completions

Dotty can generate completion scripts for common shells:

```bash
dotty completion bash
dotty completion zsh
dotty completion fish
dotty completion powershell
```

Example local installs:

```bash
# Bash
mkdir -p ~/.local/share/bash-completion/completions
dotty completion bash > ~/.local/share/bash-completion/completions/dotty

# Zsh
mkdir -p ~/.zsh/completions
dotty completion zsh > ~/.zsh/completions/_dotty

# Fish
mkdir -p ~/.config/fish/completions
dotty completion fish > ~/.config/fish/completions/dotty.fish
```

### Repository Selection

Dotty resolves the Dotfiles Repository in this order:

1. The `--repo` command flag.
2. The `DOTTY_REPO` environment variable.
3. `~/.config/dotty/config.toml`, or `$XDG_CONFIG_HOME/dotty/config.toml` when `XDG_CONFIG_HOME` is set.

`dotty init <path>` records the Default Repository in the user config file.

## Development

Contributor tool versions and tasks are defined in `mise.toml`.

```bash
mise trust && mise install
mise run fmt
mise run verify
```

To build from source:

```bash
go build -o dotty ./cmd/dotty
./dotty --help
```

For terminology and design rationale, see [`CONTEXT.md`](CONTEXT.md) and the ADRs in [`docs/adr`](docs/adr).
