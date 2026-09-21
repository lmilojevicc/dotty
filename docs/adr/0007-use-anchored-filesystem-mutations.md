# Use anchored identity-bound filesystem mutations

Dotty will perform destructive capture and installation relative to pinned, no-follow parent directory handles rather than by checking and later mutating an unbound pathname. Component identities are revalidated during traversal. Existing objects move to same-filesystem staging before inspection; Dotty compares captured type, identity, content fingerprint, symlink text, supported metadata, and directory inventory against the immutable plan. Planned-absent installation uses genuine no-replace primitives.

A capture mismatch is a pre-commit failure. Dotty first attempts identity-bound, no-replace restoration to the original anchored parent and name. It retains staging only when restoration cannot be proven, and reports rollback uncertainty with every recoverable path, consistent with ADR 0002. Successful restoration returns the captured object, including any external changes already present at capture; it does not recreate the plan's earlier version. Drift detected before capture instead refuses without mutation.

Rollback and cleanup authority are identity-bound. A transaction records every object it captures and an owned-tree inventory for every descendant it creates. Recursive cleanup performs an anchored no-follow rewalk and deletes only descendants whose relative path, type, identity, and relevant fingerprint still match that inventory. An unrecorded, missing, or changed descendant stops cleanup; foreign content is retained and the outcome is a committed cleanup warning or rollback uncertainty according to commit state. A path, root-directory identity, or Dotty-looking name alone never grants recursive deletion authority.

Platform or filesystem lack of anchored traversal, no-replace, or equivalent guarantees fails closed; check-then-rename is not an acceptable fallback. These mechanisms narrow deterministic races and protect cooperating Dotty operations, but cannot exclude in-place mutation or directory changes made through external open descriptors. Owned-tree cleanup rechecks creator identity and anchored path agreement immediately before each descriptor-relative `unlinkat`; the unavoidable syscall window between that observation and `unlinkat` remains best effort against an uncooperative external writer, while every observable pre-action swap fails closed. Creator-owned empty-directory rollback performs its final no-hooks `fstatat(AT_SYMLINK_NOFOLLOW)` identity observation immediately followed by immutable `unlinkat(AT_REMOVEDIR)`. Supported kernels provide no atomic fd-rmdir operation, so that final native-syscall boundary is trusted rather than described as atomic.

## Considered Options

- Descriptor-relative capture, identity validation, owned-tree cleanup, and no-replace install
- Pathname checks followed by rename
- Unconditional replacing rename with rollback backup
- Filesystem snapshots or crash journal
