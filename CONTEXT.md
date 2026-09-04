# Dotty

Dotty manages personal configuration files by storing them in a dotfiles repository and placing links from their expected locations back into that repository.

## Lifecycle delivery status

This document is Dotty's sole normative terminology and user-visible product contract. The lifecycle changes below—including integrity guarantees, strict decoding, deterministic Add, Remove, Prune, bounded Force Add, destructive confirmation, and Init dry-run—record previously approved target behavior, not claims that the current implementation already provides it. [ADRs](docs/adr/) own architectural mechanisms and rationale only; the [lifecycle implementation plan](docs/plans/lifecycle-implementation-plan.md) owns delivery order, acceptance gates, and evidence for this baseline. Current-baseline acceptance is tracked in that plan; historical review and implementation evidence does not transfer. `README.md` and CLI help describe only shipped behavior.

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
_Avoid_: delete link, uninstall

**Leave Copy Unlink**:
An **Unlink** variant that replaces each removed expected **Link** with a target-side copy of its **Package Source**.
_Avoid_: hard unlink, soft unlink

**Remove**:
A mapping-first operation that stops managing selected **Link Mappings**, restores ordinary target-side content by default, and deletes a **Package Source** only when no remaining dependency needs it.
_Avoid_: unlink, untrack, delete source

**Purge Remove**:
A destructive **Remove** variant that leaves selected **Target Paths** absent instead of restoring copies. It never widens the selected mappings and may retain a shared **Package Source**.
_Avoid_: force remove, hard remove

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

**Empty Collection**:
A **Collection** defined in the **Manifest** with no **Packages**. It remains valid but is reported as a successful warning by **Status** and **List**.
_Avoid_: invalid collection, missing collection

**Untracked Repository Content**:
A file or directory inside the **Dotfiles Repository** that is not represented by any **Link Mapping** in the **Manifest**.
_Avoid_: unmanaged home file, ignored file

**Prune**:
A destructive operation that deletes explicitly selected or explicitly scoped **Untracked Repository Content** without changing **Target Paths** or the **Manifest**.
_Avoid_: remove, untrack, clean package

**List**:
An inventory operation that shows packages, Package Sources, Link Mappings, and collections defined by the **Manifest** without checking target-side filesystem state.
_Avoid_: status, inspect

**Force Link**:
A link operation that intentionally replaces **Conflicts** at target paths before creating expected **Links**.
_Avoid_: overwrite mode, clobber

**Atomic Operation**:
A Dotty command that stages planned changes, commits them together after ordinary success, and attempts to reverse its changes after an ordinary failure, preserving external edits rather than reconstructing externally superseded state. It distinguishes successful rollback, uncertain rollback, and committed cleanup warnings.
_Avoid_: crash-proof transaction, all-or-nothing filesystem write

## Relationships

- A **Default Repository** is stored in Dotty's user configuration and can be overridden per command.
- A **Dotfiles Repository** contains exactly one **Manifest**.
- A **Dotfiles Repository** contains zero or more **Packages**.
- A **Manifest** contains zero or more **Collections**.
- A **Collection** references zero or more **Packages**; a zero-member Collection is an **Empty Collection**.
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
- Every mutating command or mode is an **Atomic Operation**: **Init**; **Add** and **Force Add**; **Track** and **Untrack**; Link and **Force Link**, including link creation with explicit tracking; **Unlink** and **Leave Copy Unlink**, including unlinking with explicit untracking; **Remove** and **Purge Remove**; and **Prune**.
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
- **Untrack** can remove all selected **Link Mappings** for one **Package Selector** or **Package Source Selector**, optionally narrowed by explicit **Target Paths**.
- **Unlink** does not delete target-side files or directories that are not the expected Dotty **Links**.
- **Add** grammar is `dotty add TARGET PACKAGE[/SOURCE]`; this is placement grammar, not a **Package Source Selector** accepted by other commands.
- Adding resolved directory content with a bare Package uses the **Package Root** (`source = "."`) for new and existing Packages.
- Adding resolved non-directory content with a bare Package uses the **Target Path** basename as the **Source Name**.
- Explicit nested `PACKAGE/SOURCE` placement wins over default placement; `PACKAGE/.`, an empty Source, traversal, and Package escape are invalid.
- **Symlink Adoption** keeps the symlink path as the **Target Path**, infers placement from the resolved content type, and brings the resolved content under the **Dotfiles Repository**.
- **Symlink Adoption** copies resolved content when it points outside the **Dotfiles Repository**.
- **Symlink Adoption** refuses symlinks that point inside the **Dotfiles Repository** but not to the intended **Package Source**.
- **In-Place Adoption** records the mapping and normalizes the target-side link without moving or copying repo content.
- Ordinary **Add** never overwrites an existing repository destination and suggests **Force Add** only when exact classification proves the requested source is eligible.
- **Force Add** may be invoked directly; it does not require a recorded prior failure or dry-run. The command itself always plans, renders, and confirms destructive work unless `--yes` is explicit.
- **Force Add** may replace an exact untracked or exact tracked **Package Source**, preserves exact-source **Link Mappings** and Collection membership, and adds the adopted **Target Path** only when it is not already mapped.
- The selected mapped **Target Path** may contain eligible edited input for **Force Add**. Every other dependent Target Path for the exact source must be an expected **Link** or absent.
- **Force Add** refuses unexpected dependent content, wrong Links, unequal ancestor/descendant Package Source overlap, stale identity, unsupported metadata/topology, and protected repository content.
- Replacing a non-empty directory requires `--force --recursive`.
- Package Root **Force Add** is allowed only for an absent, **Empty Package**, or exact-root-only Package; nested non-root Package Sources refuse. Existing exact-root mappings and Collections are preserved.

## Lifecycle command contracts

These contracts describe the approved lifecycle behavior. The numbered ADRs under `docs/adr/` own the implementation mechanisms; this document owns user-visible semantics.

### Planning, dry-run, and destructive confirmation

- Dry-run and execution use the same semantic plan and every pure or read-only validation.
- Dry-run never writes, prompts, or requires a writable destination. All dry-run plans are written to stdout.
- Native or filesystem capabilities that can be proven only through a write are reported as unverified during dry-run. Execution checks them before changing user state and may still refuse safely.
- `dotty init PATH --dry-run` previews repository-directory, Manifest, and Default Repository config changes without creating or rewriting them. Init continues to reject global `--repo`.
- Prompting is driven by the computed plan, not merely by a flag. A `--force` invocation with no destructive action does not prompt, and safe **Remove** never prompts.
- Destructive `add --force`, destructive `link --force`, a computed destructive `remove --purge` plan, and non-empty **Prune** plans print their complete preflight plan to stderr and use an inline Huh Yes/No selector that defaults to No and never uses a full-screen UI.
- `--yes` bypasses only the interactive selector. It does not suppress the plan, validation, revalidation, or path preconditions.
- Redundant `--yes`, including `--yes --dry-run`, is accepted. Dry-run still prevents prompting and mutation.
- Input and prompt output must be visible terminals; redirected stdout is allowed. A non-TTY destructive execution refuses unless `--yes` is explicit and prints an exact retry command with `--yes`.
- Selecting No prints `Cancelled. No changes made.` to stderr and exits zero. Ctrl-C or Escape exits 130. EOF, missing terminal capability, validation failure, confirmed-plan drift, and an ordinary rolled-back operation failure exit 1.
- Confirmed-plan drift detected before mutation reports every changed path and its planned and observed states, states that no changes were made, and requires a new plan and confirmation. Drift discovered after mutation follows the transaction outcomes below; it must not be described as a no-change refusal.
- Normal success writes result actions to stdout. Errors, Conflicts, warnings, drift, and recovery guidance use stderr. Untrack's orphan-Link note remains ordinary stdout result output.
- A committed cleanup failure exits zero because the requested state committed, warns on stderr, lists retained cleanup paths, and tells the user not to retry the operation.
- Completions never prompt, mutate, or emit normal runtime diagnostics. Inventory failure returns no selectable values, and protected repository content is never suggested.

Architecture: [ADR 0005](docs/adr/0005-use-immutable-operation-plans.md) defines immutable plans, structured diagnostics, and plan-driven confirmation.

### Remove selection and behavior

`Remove` accepts exactly one of these selection forms:

```text
dotty remove SELECTOR... [--purge] [--dry-run] [--yes]
dotty remove SELECTOR --target TARGET... [--purge] [--dry-run] [--yes]
dotty remove --collection COLLECTION... [--purge] [--dry-run] [--yes]
dotty remove --all [--purge] [--dry-run] [--yes]
```

- Positional Package/Package Source selectors, repeatable `-c, --collection`, and `--all` are mutually exclusive. No selection refuses.
- Multiple positional selectors and Collections are allowed and deduplicated.
- Repeatable `-t, --target` requires exactly one positional Package or Package Source selector. It rejects Collections, `--all`, and multiple selectors; an unmapped requested Target Path refuses.
- `--all`, `--purge`, `--dry-run`, and `--yes` have no shorthands.
- Safe **Remove** restores ordinary content at selected Target Paths, removes selected Link Mappings, and deletes only Package Sources no remaining equal or overlapping mapping needs.
- **Purge Remove** leaves selected Target Paths absent. It never expands to mappings outside the selection and may retain a shared Package Source. Explicitly authorized Purge may intentionally remove the last copy of a selected source when no remaining dependency needs it; dependency and protected-content refusals still apply.
- Unexpected regular content, wrong Links, Blocked targets, stale identity, or ambiguous Package Root state aborts the entire operation without removing mappings or repository content.
- Safe **Remove** restores a Package Source of `.` by copying it to selected Target Paths. Both safe **Remove** and **Purge Remove** retain the Package Root when remaining dependencies require it; **Purge Remove** leaves selected Target Paths absent.
- After the last selected Link Mapping is removed, Dotty removes the Empty Package only when the Package Root has no remaining content and no Collection references the Package. Otherwise it retains the Package Root and Manifest entry and reports the exact content or Collections preventing cleanup.

### Prune selection and behavior

`Prune` deletes only protected-classifier-approved **Untracked Repository Content** and never changes Target Paths or Manifest bytes/mode.

```text
dotty prune PACKAGE/PATH... [--recursive] [--dry-run] [--yes]
dotty prune PACKAGE --all [--recursive] [--dry-run] [--yes]
dotty prune --repo-wide --all [--recursive] [--dry-run] [--yes]
dotty --repo PATH prune --repo-wide --all --recursive
```

- Multiple explicit repository-relative paths may span Packages and enter one atomic plan.
- Explicit paths, one Package plus `--all`, and `--repo-wide --all` are mutually exclusive. No selection refuses.
- `--repo-wide` is a valueless Prune scope flag, requires `--all`, accepts no positional arguments, and is distinct from global `--repo PATH`.
- Explicit paths plus `--all`, Package plus `--repo-wide`, and `--repo-wide` without `--all` refuse.
- Equal paths are deduplicated. A descendant is collapsed only when an explicitly selected ancestor plus `--recursive` already covers it; Dotty never infers parents, siblings, or other content.
- Non-empty directories require `-r, --recursive`, including bulk plans. Redundant recursion on files is accepted.
- `-a, --all` and `-r, --recursive` are the only Prune shorthands.
- System entries, tracked sources, Dotty locks, transaction staging, backups, and temp artifacts are not user-selectable or prunable, even by explicit path. Parent traversal never follows a symlink outside the Dotfiles Repository.

Architecture: [ADR 0009](docs/adr/0009-share-protected-repository-inventory.md) defines the shared protected-inventory classifier and transaction-owned cleanup authority.

### Protected repository inventory

- Status omits Dotty system artifacts from user-facing tracked and untracked inventory.
- Completion never suggests a protected entry and returns no choices, without runtime diagnostics, when safe inventory cannot be resolved.
- Strict Manifest loading rejects a Link Mapping whose Package Source is protected or contains a protected descendant, with the exact mapping and protected path in the diagnostic.
- Track and link creation with explicit tracking reject a protected Package Source or a source with a protected descendant before recording a Link Mapping.
- Link refuses an existing mapping whose Package Source is protected or contains a protected descendant; Force Link does not override this refusal.
- Ordinary Add refuses a protected repository destination or any protected descendant of a selected directory or Package Root.
- Remove refuses a selected source with a protected descendant and never treats protected content as source-owned cleanup.
- Prune refuses protected content whether it is selected directly or encountered as an equal, ancestor, or descendant path.
- Force Add directory and Package Root replacement refuses every protected descendant; it never retains, merges, or replaces one silently.
- Locks, transaction staging, rollback backups, Dotty temporary paths, `dotty.toml`, and `.git` remain protected even when their names are selected explicitly. Only the transaction that created an exact recorded artifact may attempt its cleanup; changed or foreign content is retained and reported.

Architecture: [ADR 0009](docs/adr/0009-share-protected-repository-inventory.md) defines classification and transaction provenance.

### Transaction outcomes and concurrency boundary

Every Atomic Operation reports one of four outcomes:

1. **Success** — requested state committed and cleanup completed; exit 0.
2. **Rolled-back failure** — requested state did not commit; Dotty's changes were reversed and restoration was verified, preserving external edits rather than recreating the earlier planned state; exit 1.
3. **Rollback failure / uncertain state** — restoration could not be proven; exit 1 and report every uncertain or staged path plus manual recovery guidance.
4. **Committed cleanup warning** — requested state committed but an identity-bound backup or temp path could not be deleted; exit 0, list retained paths on stderr, and say not to retry the operation.

A refusal before mutation explicitly reports that no changes were made. After mutation, report verified restoration or the exact uncertain/staged paths rather than claiming unchanged state. Restoration reverses Dotty's changes; it does not promise to undo external edits or recreate an earlier planned version of externally changed content.

Cooperating Dotty mutations for the same effective OS user do not concurrently change shared Target Paths, Dotfiles Repositories, or user config. If secure user coordination or a required native filesystem capability cannot be established, the mutating command refuses before changing user state. Uncooperative external writers and open file descriptors remain a best-effort boundary; when identity or topology cannot be proven, Dotty fails closed and retains recoverable content.

Architecture: [ADR 0002](docs/adr/0002-use-rollback-based-atomicity.md) keeps crash-proof journaling out of scope; [ADR 0006](docs/adr/0006-use-a-user-scoped-mutation-lock.md) defines the lock domain; [ADR 0007](docs/adr/0007-use-anchored-filesystem-mutations.md) defines anchored mutation and rollback authority.

### Relocation, metadata, and hardlinks

- A relocated relative symlink is accepted only when every hop and final referent maps to the same contained tree-relative node before and after relocation.
- External, absolute-changing, escaping, cyclic, dangling, and otherwise unprovable embedded symlink chains refuse before relocation.
- Add, safe Remove restoration, Leave Copy Unlink, Force Add, and every cross-filesystem copy preserve and verify bytes, filesystem type, symlink text, permission and special bits, access and modification times, supported platform file flags, ACLs, xattrs, and internal hardlink topology.
- Change time and birth time are not promised copy-preservable timestamps.
- A destructive copy that may delete its source also preserves required ownership and refuses when a hardlink has an alias outside the captured tree. Leave Copy Unlink keeps the Package Source, so an external source alias is not itself a refusal, but copied-tree metadata and internal hardlinks must still be preserved.
- If required present metadata cannot be read, recreated, or verified, Dotty refuses before destructive change and retains the source. Missing native or filesystem support also refuses safely.

Architecture: [ADR 0008](docs/adr/0008-preserve-relocation-fidelity.md) defines cross-filesystem phases, metadata and hardlink mechanisms, and fail-closed capability handling.

### Manifest and config decoding

- Manifest and user config decoding is strict. Unknown top-level fields and unknown nested fields refuse before any mutation.
- The diagnostic identifies the exact file and TOML key and includes source location information when the decoder provides it.
- Dotty does not preserve or pass through unknown fields. Third-party Manifest or config extensions are unsupported and must be removed before Dotty mutates state.

### Empty metadata and deferred scope

- **Empty Collections** are valid. Status and List show a warning on stderr while preserving normal stdout data and exit zero.
- **Empty Packages** remain valid. Untrack continues to leave an Empty Package; Remove applies its conditional cleanup rule.
- Package or Package Source rename/move, Link Mapping retargeting, Collection CRUD, Default Repository unset, Manifest/repository teardown, Package Root replacement with nested non-root sources, general `replace-package`, full-screen UI, crash-resistant journaling, filesystem snapshots, and third-party Manifest extensions are not part of this lifecycle contract.
- Force never overrides unsupported content, unsafe topology, protected content, unavailable capabilities, or stale identity.

## Lifecycle examples

### Add and Track

```bash
dotty add ~/.zshrc zsh
dotty add ~/.config/nvim nvim
dotty add ~/.config/nvim/lua nvim/lua
```

The file uses Source `.zshrc`, the resolved directory uses Package Root Source `.`, and the explicit nested form uses Source `lua`. Add adopts content, writes one Link Mapping when absent, and creates the expected Link.

```bash
dotty track nvim/init.lua ~/.config/nvim/init.lua
dotty track nvim/init.lua --target ~/.config/nvim/init.lua --target ~/preview/init.lua
```

Track records the explicit Target Paths for the existing Package Source without inspecting or changing those targets. Positional targets and repeated `--target` values are merged and deduplicated.

### Link

```bash
dotty link nvim zsh/.zshrc
dotty link --collection workstation --collection shell
dotty link --all
dotty link nvim/init.lua --target ~/.config/nvim/init.lua
```

The forms select mixed Package/Package Source selectors, repeatable Collections, all Packages, or one selector narrowed to mapped Target Paths. `--target` with multiple selectors, Collections, or `--all` refuses.

### Remove

```bash
dotty remove nvim zsh/.zshrc
dotty remove --collection workstation --purge
dotty remove --all --purge --yes
dotty remove nvim/init.lua --target ~/.config/nvim/init.lua
```

Safe Remove restores ordinary target-side content and never prompts. Purge leaves selected targets absent, shows a destructive plan, and never expands beyond the same selection forms. Unexpected targets abort before mappings or repository content change.

### Unlink and Untrack

```bash
dotty unlink nvim zsh/.zshrc
dotty unlink --collection workstation --leave-copy
dotty unlink --all
dotty unlink nvim/init.lua --target ~/.config/nvim/init.lua --untrack
dotty untrack nvim/init.lua ~/.config/nvim/init.lua
dotty untrack nvim/init.lua --target ~/.config/nvim/init.lua
```

Unlink keeps Package Sources and, unless `--untrack` is explicit, keeps Link Mappings. Leave Copy Unlink writes target-side copies. Untrack changes only the Manifest and may leave orphaned target-side Links.

### Prune

```bash
dotty prune nvim/cache nvim/notes.txt --recursive
dotty prune nvim --all --recursive
dotty prune --repo-wide --all --recursive
dotty --repo ~/dots prune --repo-wide --all --recursive
```

The forms select explicit repository-relative paths, all safe untracked content in one Package, or all safe untracked content in the selected Dotfiles Repository. Prune never changes Target Paths or the Manifest, never selects protected content, and requires `--recursive` for non-empty directories.

## Flagged ambiguities

- None currently.
