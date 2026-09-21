# Preserve relocation fidelity or fail closed

Dotty prefers same-filesystem rename. Every copy path preserves and verifies bytes, filesystem type, symlink text, permission and special bits, access and modification times, supported platform file flags, ACLs, xattrs, and hardlink topology within the copied tree. Creation/change time and birth time are excluded because ordinary copies cannot recreate them.

On Linux, Dotty enumerates and preserves every accessible xattr namespace plus POSIX access/default ACL state; on Darwin it enumerates and preserves every accessible extended attribute, ACL entry, and supported file flag. Ownership is preserved when the operation requires a destructive copy followed by deletion; an owner or namespace that cannot be read, recreated, or verified causes refusal before deletion. Unsupported metadata or native capability is detected and refused rather than silently dropped.

Internal hardlink topology is recreated from recorded file identities. For destructive copy-and-delete, any alias outside the captured tree is a refusal because deleting the source would split or destroy external topology. **Leave Copy Unlink** also preserves copied-tree metadata and internal topology, but the Package Source remains, so an external source alias is not itself a deletion blocker.

Cross-filesystem relocation captures the source to identity-bound same-filesystem staging, copies to a destination-filesystem temporary tree while staging remains intact, verifies fidelity, installs no-replace, establishes every requested Target Path and Manifest/config change, and only then commits. Pre-commit failure removes only transaction-owned destination objects and restores staging no-replace; collision or unverifiable restoration retains recoverable paths and reports uncertainty. Only cleanup after the complete requested state commits may produce a committed cleanup warning.

Relative-symlink equivalence is evaluated by mapping every anchored pre-relocation hop to the corresponding tree-relative post-relocation node. This mechanism supports the user-visible acceptance and refusal rules in `CONTEXT.md` without relying on content equality or basenames.

## Considered Options

- Preserve and verify supported metadata, links, and topology or refuse
- Preserve file bytes and basic permission bits only
- Copy then delete with best-effort cleanup
- Disable all cross-filesystem operations
