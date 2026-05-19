# Dotty

Dotty manages personal configuration files by storing them in a dotfiles repository and placing links from their expected locations back into that repository.

## Language

**Dotfiles Repository**:
The directory that contains the user's managed configuration packages and Dotty's manifest.
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

**Package Source Selector**:
A command argument in `package/source` form that selects one **Package Source** inside one **Package**.
_Avoid_: file selector, path selector

**Package Selector**:
A command argument in `package` form that selects one **Package**.
_Avoid_: package name, package argument

**Package File**:
A **Package Source** that is a single file inside a **Package**.
_Avoid_: file, item, script

**Package Root**:
The top-level directory of a **Package** inside the **Dotfiles Repository**.
_Avoid_: package directory, root source

**Target Path**:
The filesystem path where a **Package Source** should appear for programs to consume it, stored home-relative with `~` when it is under the user's home directory.
_Avoid_: destination, install path

**Link**:
An absolute symbolic link at a **Target Path** pointing to a **Package Source**.
_Avoid_: mount, install

**Track**:
An operation that records a **Link Mapping** for an existing **Package Source** without adopting target-side content into the **Dotfiles Repository**.
_Avoid_: map, register, import

**Untrack**:
An operation that removes selected **Link Mappings** from the **Manifest** while keeping **Package Sources** in the **Dotfiles Repository**.
_Avoid_: unmap, deregister, delete mapping

**Conflict**:
A filesystem state that prevents Dotty from safely creating a **Link** or adopting content into a **Package**.
_Avoid_: collision, mismatch, error

**Blocked**:
A status state where a **Target Path** is currently linked to a different managed **Package Source**.
_Avoid_: conflict, collision

**Competing Link Mappings**:
Two or more **Link Mappings** whose **Target Paths** are equal or overlapping and cannot be linked in the same operation.
_Avoid_: duplicate targets, competing packages

**Overlapping Target Paths**:
A state where one **Target Path** is equal to or nested under another **Target Path** after path normalization.
_Avoid_: nested mapping, target collision

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
An operation that adopts existing target-side configuration into the **Dotfiles Repository**, replaces the original location with a **Link**, and records it in the **Manifest**.
_Avoid_: import, track

**Force Add**:
An **Add** variant that intentionally replaces an existing **Package Source** with target-side content before creating the **Link**.
_Avoid_: overwrite package, clobber source

**Symlink Adoption**:
An **Add** case where the target path is already a symlink and Dotty adopts the symlink's resolved content instead of the symlink object.
_Avoid_: symlink import, link copying

**In-Place Adoption**:
A **Symlink Adoption** case where the symlink already resolves to the intended **Package Source** inside the **Dotfiles Repository**, so Dotty records the mapping without moving or copying repo content.
_Avoid_: no-op add, stow import

**Source Name**:
The name a newly adopted file or directory receives inside its **Package**.
_Avoid_: alias, local name

**Unlink**:
An operation that removes expected **Links** from **Target Paths** while keeping **Package Sources** in the **Dotfiles Repository** and **Link Mappings** in the **Manifest**.
_Avoid_: remove, delete, uninstall

**Leave Copy Unlink**:
An **Unlink** variant that replaces each removed expected **Link** with a target-side copy of its **Package Source**.
_Avoid_: hard unlink, soft unlink

**Linked Package**:
A **Package** whose target paths all contain the expected **Links**.
_Avoid_: installed package, active package

**Unlinked Package**:
A **Package** whose **Link Mappings** remain in the **Manifest** and whose target paths are absent rather than linked.
_Avoid_: disabled package, inactive package

**Partial Package**:
A multi-mapping **Package** whose mappings are in mixed non-error states.
_Avoid_: mixed package, half-linked package

**Blocked Package**:
A **Package** with at least one selected **Target Path** already linked to a different managed **Package Source**.
_Avoid_: conflicting package, unavailable package

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
An inventory operation that shows packages, Package Sources, Link Mappings, and collections defined by the **Manifest** without checking target-side filesystem state.
_Avoid_: status, inspect

**Force Link**:
A link operation that intentionally replaces **Conflicts** at target paths before creating expected **Links**.
_Avoid_: overwrite mode, clobber

**Atomic Operation**:
A Dotty command that either completes all planned changes for every selected package or rolls back the changes it made after a normal error.
_Avoid_: transaction, all-or-nothing filesystem write

## Relationships

- A **Default Repository** is stored in Dotty's user configuration and can be overridden per command.
- A **Dotfiles Repository** contains exactly one **Manifest**.
- A **Dotfiles Repository** contains zero or more **Packages**.
- A **Manifest** contains zero or more **Collections**.
- A **Collection** references one or more **Packages**.
- Collections do not reference **Package Sources**.
- Collections are explicit lists and do not apply automatic OS detection or conditional filtering.
- Collections may contain **Packages** with **Competing Link Mappings**; link creation rejects a selected **Collection** when its packages compete.
- A **Package** can have zero or more **Package Sources** recorded by **Link Mappings**.
- A **Package** with zero **Link Mappings** is an **Empty Package**.
- A **Package Source** can be a **Package File** or a directory.
- A **Package Source Selector** names exactly one **Package** and exactly one **Package Source**.
- A tracked **Package Source** has one or more **Target Paths**.
- A tracked **Package File** has one or more **Target Paths**.
- A **Package** can have zero or more **Link Mappings**.
- Each **Link Mapping** connects exactly one **Package Source** to exactly one **Target Path**.
- Multiple **Link Mappings** may share the same **Package Source**.
- One **Package** must not contain **Competing Link Mappings**.
- Different **Packages** may contain **Competing Link Mappings** to support alternative packages for the same **Target Path**.
- Link creation rejects selected **Competing Link Mappings**, even when **Force Link** is requested.
- Additional **Link Mappings** for an existing **Package Source** are created by **Track**, by link creation with explicit tracking, or by editing the **Manifest**.
- **Track** requires explicit **Target Paths** and never infers them.
- **Track** accepts explicit **Target Paths** either as positional targets or as repeated target flags.
- **Untrack** without explicit **Target Paths** removes all selected **Link Mappings** in scope.
- **Untrack** rejects explicit **Target Paths** that are not mapped in the selected scope.
- **Untrack** accepts explicit **Target Paths** either as positional targets or as repeated target flags to remove only matching **Link Mappings**.
- When a command accepts positional targets and target flags, Dotty merges and deduplicates those **Target Paths**.
- Existing **Link Mappings** are removed from the **Manifest** with **Untrack**, by unlinking with explicit untracking, or by editing the **Manifest**.
- A **Link** exists at a **Target Path** and points to a **Package Source**.
- Whether a **Package** is a **Linked Package**, **Unlinked Package**, **Partial Package**, **Empty Package**, or has **Missing Source** is inferred from the filesystem and manifest, not stored as mutable state.
- Any non-symlink content at a **Target Path** is a **Conflict**, including a copy left by **Leave Copy Unlink**.
- A **Link** at a selected **Target Path** that points to a different managed **Package Source** is **Blocked**.
- **Force Link** can replace a **Blocked** target-side **Link** when the user selects only one of the competing alternatives.
- **Overlapping Target Paths** are unsafe because one **Link** could hide, replace, or interfere with another mapped **Target Path**.
- Manifest validation rejects **Competing Link Mappings** within one **Package**, rejects **Overlapping Target Paths** within one **Package**, and allows equal **Target Paths** across different **Packages** to support alternative configurations.
- Link creation rejects selected **Competing Link Mappings**, checks target-side filesystem topology, and can reject machine-specific symlink-parent overlap or other unsafe target layouts.
- Linking creates missing target parent directories and refuses **Conflicts** by default; **Force Link** replaces conflicts destructively.
- Unlinking removes only expected **Links** at mapped **Target Paths** and does not remove parent directories.
- **Leave Copy Unlink** removes expected **Links**, writes target-side copies, and intentionally leaves those **Target Paths** in **Conflict** until the copies are removed or replaced with **Force Link**.
- Status reports package summaries by default and per-mapping details in verbose output.
- A single-package status request reports that Package's per-mapping details by default, including untracked content inside that Package Root as verbose rows with `-` for the **Target Path**.
- A **Package Source Selector** status request reports per-mapping details for that **Package Source** by default.
- A **Package Source Selector** status request includes **Untracked Repository Content** under the selected **Package Source** when the selector names a directory.
- **List** accepts **Package Selectors** and does not accept **Package Source Selectors**.
- Multi-selector status requests remain aggregate-only by default; `--verbose` or `--state untracked` shows selected package-local untracked details.
- Status reports **Untracked Repository Content** by scanning the **Dotfiles Repository**, not by scanning arbitrary target-side directories.
- Package-scoped status requests exclude repository-wide **Untracked Repository Content** outside the selected **Package Roots**, including top-level repository entries and untracked content under unselected **Packages**.
- **List** reports manifest inventory; status reports filesystem state.
- A package-scoped **List** request reports that Package's **Package Sources** and **Target Paths**.
- **Init** does not overwrite existing package content or an existing **Manifest**.
- **Init** rejects the global `--repo` flag; it uses only its positional path argument.
- **Track** and **Untrack** are Manifest-only **Atomic Operations** and do not create, remove, copy, or replace target-side content.
- **Untrack** leaves existing target-side **Links** in place as orphaned symlinks.
- **Untrack** output warns when target-side **Links** still exist after untracking.
- Link and unlink commands operate on explicitly selected **Package Selectors**, explicitly selected **Package Source Selectors**, on all **Packages** when the user provides an explicit all option, and on **Collections** when the user provides an explicit collection option.
- **Package Selectors** and **Package Source Selectors** can be mixed in one link or unlink command.
- A **Package Selector** includes all of that Package's selected **Link Mappings**.
- A **Package Source Selector** includes only **Link Mappings** whose **Package Source** matches the selector.
- A selector ending in `/` is invalid because it has an empty **Package Source**.
- Dotty does not provide a special selector for the **Package Root**; root-only operations use explicit **Target Paths** when disambiguation is needed.
- Duplicate link and unlink selections collapse to one action per selected **Link Mapping**.
- Link and unlink commands may be narrowed to explicit **Target Paths** while preserving a single selected **Package** or **Package Source** scope.
- Link and unlink commands reject explicit **Target Paths** when the user selects multiple selectors, **Collections**, or all **Packages**.
- Composed tracking (`--track`) requires exactly one selector and cannot be combined with multiple selectors, **Collections**, or all **Packages**.
- Composed untracking (`--untrack`) requires exactly one selector and cannot be combined with multiple selectors, **Collections**, or all **Packages**.
- **Add**, **Unlink**, **Leave Copy Unlink**, **Track**, **Untrack**, and link creation are **Atomic Operations**.
- All mutating commands support dry-run mode that reports planned actions without writing changes.
- **Track** can be composed with link creation when the user selects an existing **Package Source**, explicit **Target Paths**, and explicit tracking.
- Composed tracking with link creation requires at least one explicit **Target Path**.
- Composed tracking with link creation only tracks and links the explicit **Target Paths**; it does not link other existing mappings in the selected scope.
- **Track** can create a **Package** in the **Manifest** when the selected **Package Root** already exists in the **Dotfiles Repository**.
- **Track** validates that the selected **Package Source** exists in the **Dotfiles Repository** before recording a **Link Mapping**.
- **Track** validates **Target Path** syntax but does not inspect or modify target-side filesystem state.
- **Track** uses a **Package Selector** plus explicit **Target Paths** to create package-root mappings whose **Package Source** is the **Package Root**.
- **Track** uses a **Package Source Selector** plus explicit **Target Paths** to create mappings for that selected **Package Source**.
- **Track** can create multiple **Link Mappings** for one selected **Package Source** when multiple explicit **Target Paths** are provided.
- Link creation output distinguishes existing mapped links from newly tracked and linked mappings, including in dry-run output.
- When **Track** is composed with link creation, the **Link Mapping** is recorded only if the whole link operation succeeds.
- Link creation is all-or-nothing across every selected **Link Mapping** and every newly tracked **Target Path**.
- A **Conflict** at a new **Target Path** prevents both link creation and the new **Link Mapping** unless the user explicitly chooses **Force Link**.
- **Force Link** can be combined with composed tracking to replace target-side conflicts while recording the new **Link Mapping**.
- **Untrack** can be composed with **Unlink** when the user explicitly requests both target-side unlinking and **Link Mapping** removal.
- **Untrack** composed with **Leave Copy Unlink** leaves target-side copies that Dotty no longer manages.
- **Untrack** leaves target-side content unchanged when a selected **Target Path** is not an expected Dotty **Link**.
- **Untrack** composed with **Unlink** on an absent target still removes the manifest entry.
- **Unlink** on an absent target is a no-op and does not error.
- **Leave Copy Unlink** on an absent target copies the **Package Source** to the **Target Path** and reports that no link existed.
- **Untrack** leaves an **Empty Package** in the **Manifest** when it removes the last **Link Mapping** from a **Package**.
- **Untrack** can remove all selected **Link Mappings** for a **Package Selector**, **Package Source Selector**, **Collection**, or explicit **Target Paths**.
- **Unlink** does not delete target-side files or directories that are not the expected Dotty **Links**.
- **Add** selects exactly one target-side **Target Path** and either one **Package Selector** or one **Package Source Selector**.
- Adding with a **Package Selector** uses Dotty's default source placement rules.
- Adding with a **Package Source Selector** uses the selector's package-relative source path as the new **Package Source**, including for directories.
- Adding a directory with a **Package Selector** always uses the **Package Root** as the **Package Source**.
- Adding a file as a new **Package** creates a **Package Source** whose default **Source Name** is the basename of the **Target Path**.
- Adding a file to an existing **Package** creates a new **Package Source** whose default **Source Name** is the basename of the **Target Path**.
- **Symlink Adoption** keeps the symlink path as the **Target Path** and brings the resolved content under the **Dotfiles Repository**.
- **Symlink Adoption** copies resolved content when it points outside the **Dotfiles Repository**.
- **Symlink Adoption** refuses symlinks that point inside the **Dotfiles Repository** but not to the intended **Package Source**.
- **In-Place Adoption** records the mapping and normalizes the target-side link without moving or copying repo content.
- **Add** refuses to replace existing repo-side **Package Sources** unless the user explicitly chooses **Force Add** or the existing source is the same resolved content being adopted.
- **Force Add** preserves existing **Link Mappings** for the replaced **Package Source** and adds the adopted **Target Path** only when it is not already mapped.

## Example dialogue

> **Dev:** "If I run `dotty add ~/.config/tmux tmux`, what becomes the package?"
> **Domain expert:** "The `tmux` **Package** is created in the **Dotfiles Repository**, `~/.config/tmux` becomes its **Target Path**, and Dotty leaves a **Link** there pointing at the package's **Package Source**. If I later run `dotty add ~/secrets/.zshrc_secrets zsh`, Dotty adopts that file into the existing `zsh` **Package** using `.zshrc_secrets` as its **Source Name**. If I want an explicit location, `dotty add ~/.config/foo/config.toml foo/config.toml` adopts the target-side file as the `config.toml` **Package Source** in the `foo` **Package**. If one source must appear in two places, the package has two **Link Mappings** with the same **Package Source** and different **Target Paths**."

## Flagged ambiguities

- None currently.
