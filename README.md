# Dotty

Dotty is a Go dotfiles manager. It stores packages in a dotfiles repository and creates absolute symlinks from target paths back to package sources recorded in `dotty.toml`.

## Install / run

```bash
go build -o dotty .
./dotty --help
```

## Libraries

- CLI commands and flags: [`spf13/cobra`](https://github.com/spf13/cobra)
- TOML manifest parsing: [`pelletier/go-toml/v2`](https://github.com/pelletier/go-toml)
- Colored output: [`charmbracelet/lipgloss`](https://github.com/charmbracelet/lipgloss)
- Filesystem changes: Go standard library plus Dotty's internal rollback layer

## Basic workflow

```bash
# Initialize a repository and remember it in ~/.config/dotty/config.toml
dotty init ~/dotfiles

# Move ~/.config/tmux into ~/dotfiles/tmux and link it back
dotty add ~/.config/tmux tmux

# Link or unlink packages
dotty link tmux
dotty unlink tmux          # leaves a copy at the target path
dotty unlink --hard tmux   # removes only expected Dotty symlinks

# Replace target conflicts explicitly
dotty link --force tmux

# Inspect manifest inventory vs filesystem state
dotty list
dotty status
dotty status --verbose
```

## Manifest shape

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

Collections are explicit user-defined package lists:

```bash
dotty link --collection terminal
dotty unlink --collection terminal
```

## Stow-style migration

If an existing symlink already points into the Dotty repository, `dotty add` adopts it in place:

```bash
dotty init ~/Clone/dot-example
dotty add ~/.config/tmux tmux
```

If the symlink points to an external stow repo, Dotty copies the resolved content into the Dotty repository and leaves the old repo intact.

## Status states

- `LINKED`: target is a symlink to the expected package source.
- `UNLINKED`: package source exists and target path does not exist.
- `CONFLICT`: target exists as non-symlink content or points to another source.
- `MISSING SOURCE`: manifest references a package source that does not exist.
- `EMPTY`: package has no link mappings.
- `PARTIAL`: package mappings are mixed linked/unlinked states.
- `UNTRACKED`: repository content is not represented in the manifest.
