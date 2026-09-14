# Canonical coordinator — slice 32 contract

**NONNORMATIVE · Planned · Production-unused.** This freezes the parent-adopted
scope of scout `81b29a6c` and oracle `a7782100` after the accepted native authority,
creation and flock slices. [CONTEXT.md](../../CONTEXT.md) owns product semantics;
[ADR 0006](../adr/0006-use-a-user-scoped-mutation-lock.md) and
[ADR 0007](../adr/0007-use-anchored-filesystem-mutations.md) own mechanisms. The
[lifecycle plan](lifecycle-implementation-plan.md) owns sequencing and acceptance.
This document is not implementation or execution evidence.

## Ownership and scope

Place the coordinator in `internal/nativefs`: native descriptor ownership,
authority policy and user/repository lock orchestration belong together here.
Amend package documentation explicitly: only coordinator batch resolution accepts
logical filesystem aliases. Existing primitive APIs remain physical-only.
Config/default selection, Manifest interpretation, CLI behavior, transaction
outcomes and caller migration remain outside this package/slice.

Use two stages: `AcquireUser(ctx)` returns an opaque shared `UserLease`, then
`LockRepositories(ctx, absoluteLogicalPaths)` accepts exactly one complete batch,
including empty. Callers resolve config/default selection between stages while
holding the user lease; this is not type-enforced caller migration. Provide
immutable creation-record retrieval and published logical/physical/identity
bindings without exposing owned descriptors or durable mutation authority.

Concurrent batch attempts have one winner. Losers refuse without cancelling or
corrupting the winner. A winning batch's failure is terminal and unwinds all
owned resources. Release serializes with publication, cancels pending work and
waits for its cleanup; do not hold a mutex through I/O or a wait needed by that
work. Cancellation after successful publication does not revoke ownership.
Release repository leases in reverse acquisition order, then repository pins,
then the user lease and anchor/root handles. Attempt every owned release/close
once and preserve every failure without reacquiring a closing parent.

## Canonical bootstrap

Identity is fixed by the effective UID, independent of HOME, XDG, repository,
flags or config: `/tmp/dotty-<decimal-euid>/mutation.lock` on Linux and
`/private/tmp/dotty-<decimal-euid>/mutation.lock` on Darwin. Validate full native
root authority before creating anything; unsupported capabilities fail closed.

Exclusive create may be followed by a separate existing-only open **only** for
the exact expected create-syscall EEXIST diagnostic, a wholly zero creation
record and no additional cause. Broad `errors.Is(err, EEXIST)` is insufficient.
Collision grants no authority; the separate open must independently validate it.
Do not retry disappearance/replacement loops, repair existing objects, or remove
anchors. Preserve all creation records through later acquisition/release errors,
including when acquisition cannot return a lease.

Keep the bootstrap lock-file descriptor open until an independent `LockFile`
acquisition has succeeded and strict descriptor/name/ancestry/authority continuity
has been verified against the bootstrap observation. This prevents freed-inode
reuse and rejects a different otherwise-valid rendezvous inode. It does not lock
or duplicate the bootstrap description. Close bootstrap once after comparison;
comparison or close failure unwinds everything, retaining causes and records.

Establish full-ancestry post-bootstrap baselines after permitted creation
transitions. Do not carry the 31b mkdir/APFS count exceptions into waiting or
publication. Revalidate the user lock, anchor/root and every repository after the
last repository wait, including empty-batch publication.

## Repository batch

Only existing directories are supported here. Missing, dangling, non-directory,
unsupported or unprovable selections terminate the batch; never substitute a
nearest existing parent. Missing-repository/Init behavior requires later explicit
caller design and is not inherited from legacy locking.

Reject nonabsolute or NUL-containing inputs. Do not lexically clean logical paths
before resolving symlink-sensitive `..` semantics. Resolve inside the user-locked
batch, pin normalized physical directories and retain complete authority
baselines and logical-to-physical identity associations. Sort physical paths
bytewise. Deduplicate only identical physical path/identity pairs; refuse one
identity reached through different physical topologies rather than inventing an
equivalence proof or acquiring the same advisory lock twice.

Re-resolve aliases around acquisition and final publication without switching to
replacement targets. Revalidate every retained physical pin and full authority
baseline after the last wait. Diagnostics identify logical selection, recorded
physical path, conflicting observations and safe remediation. Published bindings
are evidence only; future callers must not resolve aliases again as mutation
authority. Observations are not atomic snapshots; external ABA and uncooperative
writers remain outside exclusion guarantees.

## Required cases and gates

Declare `TestCoordinatorBoundary` with these exact fourteen required roots before
production source edits:

1. `CanonicalEUIDIgnoresEnvironment`
2. `UserBeforeResolution`
3. `OneCompleteBatch`
4. `AliasDedupStableOrder`
5. `AliasRetargetRefused`
6. `PhysicalAuthorityDrift`
7. `ExplicitEEXISTOpen`
8. `CreationRecordsSurviveFailure`
9. `CancellationUnwindsReverse`
10. `ReleaseConcurrentOnceAllErrors`
11. `NoSharedDescription`
12. `NoAnchorRemoval`
13. `BootstrapIdentityContinuity`
14. `MissingRepositoryRefused`

Include explicit subcases for final user-anchor drift, conflicting physical
topologies, concurrent batch attempts, bootstrap-close failure, and native child
serialization across differing repository/config-resolution choices. Assert
intended injection/transition reachability, exact paths/causes and once-only
cleanup; an earlier unrelated refusal must not satisfy a later-phase test.

All adverse fixtures use private paths. Native positive cases use real metadata
through package-private `_test.go` boundaries, never a public root override,
environment knob, build-tag escape, fake positive authority, or the host's
canonical UID anchor. Read-only system-temporary-root metadata may be observed;
canonical anchor writes are not authorized. Bounded private integration is not
unmodified canonical CLI proof. No new VM, chroot/capability grant, dependency,
release change or C4 producer belongs here.

Require independent source/test review, reviewed clean parent-owned mise
formatting, full verification and vulnerability checks, non-skipped Linux and
Darwin inventories, and separate unavailable/no-cgo/foreign compile evidence.
Preserve previous native inventories. Production Darwin support remains blocked
by the unchanged cgo-disabled release policy. The unresolved focused-fixture
`before_anchor_INT` timeout is recorded in the 31c receipt: its one authorized
unchanged rerun passed, but recurrence requires investigation before another
attempt. No automatic retry or test waiver follows from this contract.
