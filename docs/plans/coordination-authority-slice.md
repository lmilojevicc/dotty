# Coordination authority — slice 31 implementation/test contract

**NONNORMATIVE · Planned · Production-unused.** This records the accepted scope and limits of oracle `9ab9a37a` and follow-up `71110014`, within the continuing full-plan authorization. It is not implementation evidence, Milestone 1 acceptance, or a product-semantics change. [CONTEXT.md](../../CONTEXT.md) owns product semantics; [ADR 0006](../adr/0006-use-a-user-scoped-mutation-lock.md) and [ADR 0007](../adr/0007-use-anchored-filesystem-mutations.md) own coordination and anchored authority. The [lifecycle plan](lifecycle-implementation-plan.md#first-serial-slices) owns delivery order and wider gates.

Slice 31 extends the accepted native-handle boundary with authority facts, private create/open, and file/directory flock primitives. Slice 32, the canonical coordinator, follows only after these serial gates and remains production-unused initially. No caller, persistence, transaction, CLI, or release migration belongs here.

## Facts and policy boundary

- `Identity` remains device/inode/kind identity, not permission or deletion authority. Separate `AuthorityFacts` carry descriptor-observed UID, GID, permission and special bits, link count, filesystem/mount facts, and explicit security-evidence state. Zero/uninitialized evidence is invalid, never verified absence.
- Only descriptor-bound **verified ACL absence** is supported in this slice. Any present ACL, including deny-only or inheritance-bearing ACLs, refuses; unknown, unreadable, malformed, or unsupported evidence refuses. This is a bounded capability, not general ACL evaluation or relocation fidelity.
- Linux uses `Fgetxattr` for POSIX access ACLs and directory default ACLs. Only `ENODATA` on the approved descriptor-verified ext/tmpfs model establishes absence. `EOPNOTSUPP` is unsupported, not absence. NFS, CIFS, FUSE, overlay, and unknown filesystems refuse; read-only overlay gets no exception.
- Darwin supports local APFS with ownership enforcement proven. A small `darwin && cgo` libc adapter uses `acl_get_fd_np`, entry inspection, validation, deferred-inheritance inspection, and `acl_free`; all failures fail closed. `darwin && !cgo` is unsupported. No new dependency, private linkname, handwritten raw ABI, or command-output parsing supplies security evidence.
- Release builds remain `CGO_ENABLED=0`. **Production Darwin integration is BLOCKED until build policy is resolved.** Native cgo evidence cannot establish production release support.

| Role | Required authority |
| --- | --- |
| Private anchor directory | Effective UID; exact `0700`, no special bits; supported filesystem/mount/security evidence |
| Lock file | Effective UID; regular file; exact `0600`, no special bits; `nlink == 1`; supported evidence |
| Dotfiles Repository directory | Effective UID; no group/other write or special bits; supported evidence |
| System ancestors | Validation only under the full ancestry policy, including ADR 0006's expected root-owned sticky temporary root; never impose private-anchor mode on system ancestors, repair them, or flock `/` or the temporary root |

Role policy and identity/topology guards belong **inside operations**, not in an optional caller preflight. Revalidate before lock waiting and after acquisition, before returning authority; observable ownership, mode, ACL, mount, pathname, or ancestry drift refuses.

## Creation and lease limits

Existing objects are never repaired, truncated, or removed. Opens use no-follow, non-truncating flags; creation uses genuine exclusivity. An exclusively created file descriptor may establish its mode only after security binding. This exception never authorizes chmod of an existing object or a newly observed race winner.

Successful `mkdir` records a creation event, not proof of ownership of a subsequently opened inode. Under a restrictive umask, refuse an unusable directory and retain/report the creation when identity/authority cannot be proven; do not chmod it, change process-global umask, or delete it speculatively. Creation records retain exact paths and available observations for recovery, **not deletion authority**. Errors distinguish pre-creation refusal from retained/uncertain creation and give safe recovery guidance rather than claiming no changes.

Each lock acquisition uses an independent open file description, not a duplicated/shared description masquerading as another lease. Waiting uses context-aware nonblocking flock, handles contention and `EINTR`, and stops on cancellation. Closing cancels waiters; an already returned lease pins its descriptor and ancestry until concurrency-safe release. Unlock and close are each attempted once, with errors joined and preserved; this layer does not classify them into C4 outcomes.

## Accepted test boundary

Use only a **test-only private authority root**: no CLI flag, environment/config override, or exported alternate production entrypoint. Linux positive fixtures verify the actual root descriptor's ext/tmpfs filesystem and real UID/mode/ACL evidence at that root and below; Darwin positive fixtures analogously require native ownership-enforced local APFS evidence. Do not synthesize positive filesystem or security facts.

Only the test security-ancestry boundary is cut above that root. Every native identity and topology guard still covers full ancestry. Production always checks full security ancestry. Keep a separate unbounded-production-policy refusal track, including unsupported overlay, without touching the host canonical anchor. Negative injected-reader cases supplement, not replace, native evidence.

Report these results as **bounded integration**, not canonical CLI coverage. Record Darwin native cgo ACL evidence separately; `!cgo` and foreign-target compile results establish limits only. Later positive unmodified CLI coverage needs an appropriate environment and its own gate. No host canonical-anchor adversarial fixtures or VM execution are approved.

## Serial slices and named required cases

The names below are planned maintained required-case identifiers, not existing test claims. Common cases must execute on **each native OS**; platform-specific cases must execute on the named native OS. Declare exact leaf inventories before source edits, and fail the gate on missing/skipped required cases. Fixtures stay private; umask variations run in isolated subprocesses, never by changing the test runner's global umask. Inspect raw open/creation flags and pre/post state; do not repair fixtures after the operation to make assertions pass.

| Gate | Common required cases on Linux and Darwin |
| --- | --- |
| 31a — facts/security readers | `ZeroEvidenceInvalid`; `IdentityNotAuthority`; `DescriptorFactsUIDGIDModeSpecialNlink`; `RolePolicyMatrix`; `ACLAbsentVerified`; `ACLPresentRefused`; `ACLReadFailureRefused`; `FilesystemMountUnknownRefused`; `SecurityDriftSameInode`; `FullIdentityTopologyAncestry`; `PrivateSecurityBoundaryOnly`; `UnboundedProductionPolicyRefusal` |
| 31b — private create/open, after 31a | `ExclusiveFileModeAfterSecurityBinding`; `ExistingObjectNeverRepairedTruncatedRemoved`; `RawOpenFlags`; `ExistingSymlinkWrongTypeOwnerModeSpecialRefused`; `LockHardlinkRefused`; `CreateCollisionWinnerUntouched`; `MkdirCreationNotInodeOwnership`; `PostCreateReplacementRefused`; `RestrictiveUmaskDirectoryRetained`; `CreationRecordNoDeleteAuthority`; `ExactRecoveryPaths`; `SystemAncestorsValidationOnly` |
| 31c — file/directory flock, after 31b | `FileLockSerializesProcesses`; `DirectoryLockSerializesProcesses`; `IndependentDescriptionPerLease`; `ContentionCancellation`; `EINTRRetryAndCancellation`; `CloseCancelsWaiters`; `ReturnedLeasePinsAncestry`; `ConcurrentReleaseOnce`; `UnlockCloseErrorsJoined`; `FailedAcquisitionReleasesResources`; `PreWaitPolicyDrift`; `PostWaitPolicyIdentityTopologyDrift`; `UnsupportedFlockRefused`; `NoSystemAncestorFlock`; `NoC4Classification` |

Platform-specific 31a inventory:

- **Linux:** `PosixAccessENODATA`; `DirectoryDefaultENODATA`; `AccessACLPresentRefused`; `DefaultACLPresentRefused`; `EOPNOTSUPPNotAbsence`; `DescriptorExtTmpfsVerified`; `NfsCifsFuseOverlayUnknownRefused`; `ReadonlyOverlayStillRefused`.
- **Darwin/cgo:** `LocalAPFSOwnershipEnforced`; `OwnershipDisabledRefused`; `DescriptorACLNoEntriesVerified`; `AllowACLRefused`; `DenyOnlyACLRefused`; `InheritedACLRefused`; `DeferredInheritanceRefused`; `ACLValidationFailureRefused`; `ACLAllocationFreedOnAllPaths`.
- **Capability-limit lane:** `DarwinNoCgoUnsupported` and foreign-target unsupported compilation. These never substitute for Darwin/cgo native cases.

Each gate requires independent source/integrity and test review, clean-environment `mise run verify` and `mise run vuln`, and non-skipped native required-case evidence with exact candidate, OS/architecture, filesystem/mount/security facts, build mode, executed leaves, and outcomes. Distinguish real native cases from injected failures and compile-only evidence. An unavailable required environment blocks acceptance; do not silently skip, weaken policy, or reuse old evidence. Only after 31c acceptance may unused coordinator slice 32 begin; production integration still requires the lifecycle plan's release-outcome and isolation readiness.

## This documentation task

Only this contract is added. Static comparison against the supplied decisions, CONTEXT, ADRs, and lifecycle sequencing is not runtime validation. No source edits, Git operations, project checks, Go/Docker/live Dotty execution, host-anchor fixtures, old-worktree access, or VM activity occur in this task. Implementation and all gates above remain pending.
