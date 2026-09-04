# Use immutable operation plans and structured confirmation

Dotty will represent each mutation as an immutable semantic plan produced by the same planner used for dry-run and execution. Actions use canonical ordering and include the selected scope, dependencies, destructive classification, expected transaction outcome, and the filesystem state relevant to execution: anchored paths, object type and identity, content fingerprints, symlink text, supported metadata snapshots, and complete relative directory inventory. Locked replanning compares every field that can change the selected action, authorization, or result, including same-inode content changes and descendant drift.

Dry-run performs the same semantic planning and every pure or read-only validation. Capability checks that require an actual write or native mutation are recorded as unverified; execution performs them under the mutation boundary and may still refuse before changing user state.

The CLI renders structured diagnostics rather than parsing hints from error strings. Interactive confirmation is plan-driven: only plans containing destructive actions use an inline confirmation boundary, and confirmation never bypasses validation or locked replanning. Input, output, prompt writer, terminal capability, and exit status remain injectable so TTY and non-TTY behavior can be tested without embedding interaction in `internal/dotty`.

This design provides one semantic validation seam for dry-run, automation, and interactive execution without holding locks while a user decides. It does not make plans durable across process restarts and does not add crash-proof journaling.

## Considered Options

- Immutable plan, locked replan, and exact comparison
- Separate dry-run and execution branches
- Hold filesystem locks while prompting
- Persist authorization tokens between invocations
