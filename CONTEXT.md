# Dotty

Dotty manages personal configuration files by storing them in a dotfiles repository and placing links from their expected locations back into that repository.

## Language

**Dotfiles Repository**:
The directory that contains the user’s managed configuration packages and Dotty’s manifest.
_Avoid_: store, vault, dotfiles folder

**Default Repository**:
The **Dotfiles Repository** path Dotty uses when a command does not provide an override.
_Avoid_: active repo, selected repo

**Package**:
A named unit of configuration stored in the **Dotfiles Repository** and managed as one installable group.
_Avoid_: module, app, bundle

**Package Source**:
The file or directory inside a **Package** that is the source of truth for managed configuration content, addressed by a package-relative path that cannot escape the **Package Root**.
_Avoid_: source path, repo path

**Package Root**:
The top-level directory of a **Package** inside the **Dotfiles Repository**.
_Avoid_: package directory, root source

**Target Path**:
The filesystem path where a **Package Source** should appear for programs to consume it, stored home-relative with `~` when it is under the user’s home directory.
_Avoid_: destination, install path

**Link**:
An absolute symbolic link at a **Target Path** pointing to a **Package Source**.
_Avoid_: mount, install

**Conflict**:
A filesystem state that prevents Dotty from safely creating a **Link** or adopting content into a **Package**.
_Avoid_: collision, mismatch, error

**Link Mapping**:
A manifest record that connects one **Package Source** to one **Target Path**.
_Avoid_: rule, entry, mapping

**Manifest**:
The versioned `dotty.toml` file in the **Dotfiles Repository** that records package-keyed **Link Mappings** and named **Collections**.
_Avoid_: config, database, registry

**Collection**:
A user-defined named group of **Packages** that can be linked or unlinked together.
_Avoid_: profile, bundle, package group

**Init**:
A non-destructive operation that prepares a **Dotfiles Repository**, creates the **Manifest** if missing, validates an existing **Manifest**, and records the **Default Repository**.
_Avoid_: setup, bootstrap

**Add**:
An operation that adopts existing configuration into the **Dotfiles Repository**, replaces the original location with a **Link**, and records it in the **Manifest**.
_Avoid_: import, track

**Map**:
An operation that records a **Link Mapping** from an existing **Package Source** to a **Target Path** without copying, moving, linking, or otherwise changing filesystem content outside the **Manifest**.
_Avoid_: import, install, discover

**Unmap**:
An operation that removes a **Link Mapping** from the **Manifest** without unlinking, deleting, copying, or otherwise changing target-side filesystem content or **Package Sources**.
_Avoid_: delete, uninstall, unlink

**Symlink Adoption**:
An **Add** case where the target path is already a symlink and Dotty adopts the symlink’s resolved content instead of the symlink object.
_Avoid_: symlink import, link copying

**In-Place Adoption**:
A **Symlink Adoption** case where the symlink already resolves to the intended **Package Source** inside the **Dotfiles Repository**, so Dotty records the mapping without moving or copying repo content.
_Avoid_: no-op add, stow import

**Source Name**:
The name a newly adopted file or directory receives inside its **Package**.
_Avoid_: alias, local name

**Unlink**:
An operation that removes a **Link** from a **Target Path** while keeping the **Package Source** in the **Dotfiles Repository** and the package’s **Link Mappings** in the **Manifest**.
_Avoid_: remove, delete, uninstall

**Linked Package**:
A **Package** whose target paths all contain the expected **Links**.
_Avoid_: installed package, active package

**Unlinked Package**:
A **Package** whose **Link Mappings** remain in the **Manifest** and whose target paths are absent rather than linked.
_Avoid_: disabled package, inactive package

**Partial Package**:
A multi-mapping **Package** whose mappings are in mixed non-error states.
_Avoid_: mixed package, half-linked package

**Missing Source**:
A status state where the **Manifest** references a **Package Source** that does not exist.
_Avoid_: broken, missing entry

**Empty Package**:
A **Package** defined in the **Manifest** with no **Link Mappings**.
_Avoid_: blank package, no-op package

**Untracked Repository Content**:
A file or directory inside the **Dotfiles Repository** that is not represented by any **Link Mapping** in the **Manifest**.
_Avoid_: unmanaged home file, ignored file

**List**:
An inventory operation that shows packages and collections defined by the **Manifest** without checking target-side filesystem state.
_Avoid_: status, inspect

**Hard Unlink**:
An **Unlink** variant that removes expected Dotty **Links** without leaving a copy at the **Target Path**.
_Avoid_: delete package, purge

**Force Link**:
A link operation that intentionally replaces **Conflicts** at target paths before creating expected **Links**.
_Avoid_: overwrite mode, clobber

**Atomic Operation**:
A Dotty command that either completes all planned changes for every selected package or rolls back the changes it made after a normal error.
_Avoid_: transaction, all-or-nothing filesystem write

## Relationships

- A **Default Repository** is stored in Dotty’s user configuration and can be overridden per command.
- A **Dotfiles Repository** contains exactly one **Manifest**.
- A **Dotfiles Repository** contains zero or more **Packages**.
- A **Manifest** contains zero or more **Collections**.
- A **Collection** references one or more **Packages**.
- Collections are explicit lists and do not apply automatic OS detection or conditional filtering.
- A **Package** has one or more **Package Sources**.
- Each **Package Source** has one or more **Target Paths**.
- A **Package** has one or more **Link Mappings**.
- Each **Link Mapping** connects exactly one **Package Source** to exactly one **Target Path**.
- Multiple **Link Mappings** may share the same **Package Source**.
- Additional **Link Mappings** for an existing **Package Source** are created with **Map** or by editing the **Manifest**.
- Existing **Link Mappings** are removed from the **Manifest** with **Unmap** or by editing the **Manifest**.
- A **Link** exists at a **Target Path** and points to a **Package Source**.
- Whether a **Package** is a **Linked Package**, **Unlinked Package**, **Partial Package**, **Empty Package**, or has **Missing Source** is inferred from the filesystem and manifest, not stored as mutable state.
- Any non-symlink content at a **Target Path** is a **Conflict**, including a copy left by **Unlink**.
- Linking creates missing target parent directories and refuses **Conflicts** by default; **Force Link** replaces conflicts destructively.
- Unlinking removes only target-side links or content at mapped **Target Paths** and does not remove parent directories.
- Status reports package summaries by default and per-mapping details in verbose output.
- A single-package status request reports that Package's per-mapping details by default.
- Status reports **Untracked Repository Content** by scanning the **Dotfiles Repository**, not by scanning arbitrary target-side directories.
- Package-scoped status requests do not include repository-wide **Untracked Repository Content**.
- **List** reports manifest inventory; status reports filesystem state.
- **Init** does not overwrite existing package content or an existing **Manifest**.
- **Map** and **Unmap** operate on **Packages** and **Link Mappings**; link creation, **Unlink**, and **Hard Unlink** operate on **Packages** and target-side **Links**.
- Link and unlink commands operate on explicitly selected **Packages**, on all **Packages** when the user provides an explicit all option, and on **Collections** when the user provides an explicit collection option.
- Link and unlink commands may be narrowed to explicit **Target Paths** while preserving their selected **Package** or **Collection** scope.
- **Add**, **Unlink**, **Hard Unlink**, and link creation are **Atomic Operations**.
- **Map** and **Unmap** are Manifest-only **Atomic Operations** and do not create, remove, copy, or replace target-side content.
- **Unmap** leaves **Package Sources**, **Collections**, and an **Empty Package** unchanged when it removes the last **Link Mapping** from a **Package**.
- **Hard Unlink** does not delete target-side files or directories that are not the expected Dotty **Links**.
- Adding a directory as a new **Package** uses the **Package Root** as the **Package Source**.
- Adding a file as a new **Package** creates a **Package Source** whose default **Source Name** is the basename of the **Target Path**.
- Adding to an existing **Package** creates a new **Package Source** whose default **Source Name** is the basename of the **Target Path**.
- **Symlink Adoption** keeps the symlink path as the **Target Path** and brings the resolved content under the **Dotfiles Repository**.
- **Symlink Adoption** copies resolved content when it points outside the **Dotfiles Repository**.
- **Symlink Adoption** refuses symlinks that point inside the **Dotfiles Repository** but not to the intended **Package Source**.
- **In-Place Adoption** records the mapping and normalizes the target-side link without moving or copying repo content.
- **Add** refuses to overwrite existing repo-side package sources unless they are the same resolved content being adopted.

## Example dialogue

> **Dev:** "If I run `dotty add ~/.config/tmux tmux`, what becomes the package?"
> **Domain expert:** "The `tmux` **Package** is created in the **Dotfiles Repository**, `~/.config/tmux` becomes its **Target Path**, and Dotty leaves a **Link** there pointing at the package’s **Package Source**. If I later run `dotty add ~/secrets/.zshrc_secrets zsh`, Dotty adopts that file into the existing `zsh` **Package** using `.zshrc_secrets` as its **Source Name**. If one source must appear in two places, the package has two **Link Mappings** with the same **Package Source** and different **Target Paths**."

## Flagged ambiguities

- None currently.
