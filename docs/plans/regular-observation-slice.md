# Regular-file observation — slice 33a contract

**NONNORMATIVE · Planned · Production-unused.** Parent-adopted scope from scout
`3279d132` and one-shot oracle `9cfa0a1b`, following the [accepted coordinator32](lifecycle-implementation-plan.md#milestone-1-canonical-coordinator-32-acceptance-receipt).
[CONTEXT.md](../../CONTEXT.md) owns product semantics; [ADR 0007](../adr/0007-use-anchored-filesystem-mutations.md)
and [ADR 0008](../adr/0008-preserve-relocation-fidelity.md) own anchored mutation
and fidelity mechanisms. The [lifecycle plan](lifecycle-implementation-plan.md)
owns sequencing and acceptance. This document is a contract, not execution evidence.

## Scope and API

Add only `(*Dir).ObserveRegular(context.Context, Component) (RegularObservation, error)`
and its immutable data/error vocabulary inside `internal/nativefs`. Preserve
existing identity, authority, creation, flock and coordinator behavior. Do not add
reads to the existing `File` lifetime, return a reader or descriptor, expose a
callback, or adopt descriptors supplied by callers.

`RegularObservation` has private fields and value getters; its zero value is
invalid. Successful results contain device/inode/kind identity and validity,
UID, GID, permission/special mode bits (`07777`), link count, signed nonnegative
size, exact mtime/ctime seconds-and-nanoseconds pairs, and a fixed-array SHA-256
digest. Normalize without truncation, rounding or time-zone/monotonic semantics;
reject malformed or unrepresentable values. Every listed metadata field,
including identity validity, participates in exact metadata equality. Digest
bytes are returned by value. Every failure returns the wholly zero result.

These values describe bounded observations and bytes read, not an atomic or
durable snapshot. Exclude atime, birthtime, allocation data, file flags, xattrs
and ACL values. Do not imply complete metadata fidelity, symlink/tree inventory,
transaction provenance, capture, restoration or cleanup authority. A new public
metadata abstraction is not required merely to format an error; use the smallest
maintainable immutable representation and retain exact expected/observed values.

## Geometry, not security approval

This is the same data-only policy layer as `Dir.Observe`, with additional repeated
physical checks. Require regular type, usable metadata and successful native
reads. Do not impose effective-UID ownership, exact permissions, special-bit
restrictions, single-link policy, filesystem allowlists, ACL validation or an
authority role on the parent or payload. In particular, `RoleLockFile` is not a
general-file policy.

Readable files with other owners, arbitrary permissions/special bits and multiple
hardlinks are eligible; their observed metadata must remain equal. Permission or
native-capability failures refuse normally. Eligibility is not ownership or
security approval. Future consumers must independently obtain and revalidate
all authority required by their operation. This does not widen authority31a,
remove its cgo requirement or authorize callers to bypass its policy.

Linux and Darwin may implement this geometry/data-only operation without cgo.
Foreign targets must retain fail-closed unsupported stubs; successful compilation
is not foreign runtime evidence. Positive Darwin/no-cgo observation must never be
reported as authority, flock, coordinator or production-release support.

## Acquisition, observation and publication

1. Validate component and context, including nil context. Acquire the parent
   exactly once. Check context/parent closing and guard the complete physical
   ancestry before proceeding.
2. No-follow stat the selected entry; require regular type and normalize a
   complete metadata baseline before opening it.
3. Open a fresh independent description with fixed
   `O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_NOCTTY|O_CLOEXEC`, without create, truncate,
   following fallback, ownership repair or access-method retry.
4. Guard full ancestry and require held-FD and parent/name metadata to equal the
   baseline before reading. A pre-open regular-to-special/symlink swap must not
   reach hashing after the opened descriptor is found to have the wrong type.
5. Stream exactly the initial size through a fixed-size buffer, then perform a
   one-byte EOF probe. Premature EOF or extra data refuses. Do not compute
   `size+1`; handle maximum signed sizes without overflow. Handle short reads,
   EINTR and native read failures without losing progress, spinning on invalid
   results or skipping cancellation checks.
6. Reobserve FD/name metadata and every physical ancestry edge. Include context
   and closing checkpoints between synchronous calls and chunks. Same-identity
   metadata drift must be distinguished from identity replacement.
7. Close the owned raw FD exactly once on every owned-FD exit, joining close
   failures with prior causes. Never retry close or assume failure retains the
   descriptor number. A close failure cannot yield a valid observation. Retain
   the parent lease until all owned-FD cleanup and publication decisions finish.
8. Serialize successful publication against parent `Close` using the existing
   lifecycle mutex, checking cancellation immediately before publication. No
   syscall or blocking cleanup runs under that mutex. Release the parent lease
   afterward. Cancellation after publication does not revoke a returned value.

Use per-call private fault seams, not mutable global hooks. Preserve exact
quoted affected paths, reasons, expected/observed changed fields and native or
context causes through `errors.Is`/`errors.As`, including joined close errors.
Diagnostics must not invent mutation or restoration outcomes. Give a safe
inspection/stop-concurrent-edits remedy for drift; inability to observe is not
permission to retry through a weaker path.

## Atime, resources and limits

Read-only means **no intentional filesystem writes**. Reading may update atime,
including when observation later fails or is cancelled. Do not compare, return
or restore atime in this slice; do not add `O_NOATIME`, capabilities, mount changes
or a Linux-only timestamp-neutrality promise.

Public documentation consulted:

- [Linux open(2)](https://man7.org/linux/man-pages/man2/open.2.html): `O_NOATIME` is
  Linux-specific and requires matching ownership or `CAP_FOWNER`; `O_NONBLOCK`
  does not make regular-file I/O nonblocking.
- [Linux read(2)](https://man7.org/linux/man-pages/man2/read.2.html): short reads are
  valid, zero means EOF, and EINTR can occur before data is read.
- [Apple archived stat(2)](https://developer.apple.com/library/archive/documentation/System/Conceptual/ManPages_iPhoneOS/man2/stat.2.html):
  lists `read` among atime-changing calls. Archived API documentation is not
  empirical proof of current APFS timestamp behavior.

Fixed buffer memory and the initial-size limit provide a finite payload-byte
budget, not a small total-work ceiling or a syscall/wall-clock deadline. Context
and parent closing are observed between synchronous operations; regular-file
I/O can still block. No public byte-cap knob is needed for this unused primitive.

Mixed reads, timestamp granularity, inode reuse/ABA, and changes after the final
observation remain possible. A hostile regular-to-device swap can affect `open`
before post-open rejection; the flags do not eliminate every device-open race.
No security sandbox or universal filesystem guarantee is asserted.

**Required later decision, before planning/dry-run or C5a/caller integration:**
must pre-read atime be preserved exactly, or are ordinary read-induced updates
permitted? This unused primitive does not settle that product-facing question.
An exact-preservation requirement must revisit the design, not add hidden
restoration writes or silently relax fidelity. Likewise, complete fidelity
metadata must be established before these observations can participate in
capture comparisons or owned-artifact cleanup authority.

## Required tests before production edits

Declare `TestRegularObservationBoundary` with an independent required-name list
and implementation map, failing on missing, duplicate, skipped, filtered or
incomplete required roots:

1. `EmptyBinaryAndStreamingDigest`
2. `MetadataNormalizationAndExactEquality`
3. `GeometryOnlyPayloadPolicy`
4. `InvalidComponentMissingAndWrongKind`
5. `SpecialEntryAndSymlinkSwapRefusal`
6. `PreOpenFDNameBinding`
7. `FullAncestryAndLeafReplacement`
8. `SameInodeContentAndMetadataDrift`
9. `ShrinkGrowthAndEOFProbe`
10. `ShortReadEINTRAndReadFailures`
11. `StatGuardAndOpenFailures`
12. `ContextAndParentCloseCheckpoints`
13. `CopiedParentConcurrentObservationAndPublication`
14. `CloseOnceJoinedErrorsAndZeroResults`
15. `AtimeExclusionImmutableAPIAndUnavailableTargets`

Assert intended hook reachability and exact cleanup ownership, not merely an
error that could have arisen earlier. Observe real private native files for
positive digest, metadata and hardlink cases; keep counterfactual field/policy
and failure injections explicitly separate. Do not claim native foreign-owner
or device-race coverage from synthesized stat results. Simulated FD-zero cases
must never operate on real stdin. Atime exclusion can be tested with controlled
observations without asserting every filesystem updates it.

All test paths stay private with HOME/XDG/DOTTY_REPO isolated. Do not access live
dotfiles or the host canonical UID anchor. Preserve complete physical ancestry
guards and all previous required inventories; do not weaken earlier private
security-root tests or synthesize positive authority facts. Change only the
necessary API allowlist assertion in the earlier native boundary tests.

## Serial acceptance gates

Tests/inventory first, bounded implementation second, independent integrity and
test review, parent-reviewed clean launchers, formatting through mise, complete
non-skipped native Darwin/Linux inventories, full uncached `mise run verify`,
security-sensitive `mise run vuln`, and separate no-cgo/foreign compile evidence.
Workers do not execute Git, project checks or launchers; parent pauses all source
writers before execution. Native verification needs fresh exact-candidate
bindings; prior kit approvals do not transfer. No new VM/isolation infrastructure
is authorized. A `before_anchor_INT` recurrence still requires investigation
before another attempt, with no automatic retry or waiver.

Acceptance of 33a would not complete the anchored-observation/artifact foundation.
Separately gate symlink and immediate directory inventory, complete recursive
observations, creator provenance and narrow cleanup before downstream classifier,
fidelity, EXDEV and caller work. Creation records, matching root identity, locks,
paths and Dotty-looking names remain insufficient to mint deletion authority.
Legacy operations and production release policy remain unchanged.
