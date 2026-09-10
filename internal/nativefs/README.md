# Native handle boundary

This standalone package is not used by production commands. It does not change
`ResolveRepo` or logical repository-alias semantics. `OpenPhysicalDir` accepts
only explicit physical absolute paths (`/` or slash-separated valid components),
refusing symlink directories, relative paths, repeated/trailing separators, and
`.`/`..`. Future alias validation/resolution belongs above this boundary.

API:

- `ParseComponent(string) (Component, error)` validates one opaque Unix name.
- `OpenPhysicalDir(string) (*Dir, error)` owns read-only, no-follow, close-on-exec
  directory descriptors for the complete ancestry from `/`.
- `(*Dir).Identity() (Identity, error)` revalidates the pinned directory chain.
- `(*Dir).Observe(Component) (Identity, error)` observes the entry, not a symlink
  referent.
- `(*Dir).Authority(AuthorityRole) (AuthorityFacts, error)` validates immutable
  descriptor authority observations and full security ancestry under a lease.
  This is not durable authorization for a later action.
- `RenameNoReplace(*Dir, Component, Identity, *Dir, Component) error` revalidates
  both chains and expected source identity, then performs native exclusive rename.
- `(*Dir).OpenPrivateDir(Component) (*Dir, error)` opens an existing exact-0700
  directory; `CreatePrivateDir(Component) (*Dir, CreationRecord, error)` creates
  exclusively. Both validate system-ancestor parent policy internally.
- `(*Dir).OpenLockFile(Component) (*File, error)` opens an existing lock file;
  `CreateLockFile(Component) (*File, CreationRecord, error)` creates exclusively.
  Both require an exact-0700 private parent and native lock-file authority.
- `(*File).Close() error` closes its independent read-only descriptor once and
  releases its retained parent lease, even on error. Copies share lifetime.
- `(*Dir).LockFile(context.Context, Component) (*Lease, error)` locks a fresh
  existing-only read-only File; `LockDirectory(context.Context) (*Lease, error)`
  locks an independently repinned repository directory. Neither creates content.
- `(*Lease).Release() error` attempts unlock and close once, joins path-wrapped
  failures, and drops retained ancestry even on error. Copies share lifetime.
- `(*Dir).Close() error` signals pending acquisition cancellation, blocks new
  acquisition/publication, then waits for active operations, dependent Files,
  **and returned file/directory Leases** before closing descriptors once. Release
  Leases and close Files before awaiting parent Close. Copies share lifetime.

Identity means device/inode/kind, not content, metadata, a durable snapshot, or
atomic inode comparison-and-swap. Private leases protect descriptor lifetime
through the syscall, not against uncooperative filesystem writers. All observable
pre-guard swaps refuse; the final observation/syscall interval remains best effort.
The private errno-injection seam is not a supported callback API or a security
boundary against malicious code inside the package.

Linux uses `renameat2(RENAME_NOREPLACE)`; Darwin uses
`renameatx_np(RENAME_EXCL)`. Other build targets refuse before filesystem work.
There is no replacing rename, copy, delete, or cross-device fallback. `EINVAL`
is retained as an invalid native operation, not proof of an unsupported flag;
known directory-into-self/descendant topology is separately refused. Errors are
primitive evidence, never final transaction outcome reports.

## Required native cases

`native_test.go` defines `requiredNativeCases` independently of the implementation
map. The aggregator fails if a required case is missing, duplicated, filtered out,
skipped, or fails to complete. Run the entire aggregator, not a selected child:

```sh
mise run test:focused ./internal/nativefs '^TestNativeHandleBoundary$'
```

Fixtures create content only under private `t.TempDir` roots, resolve the fixture
root physically before opening it, and isolate `HOME`, `XDG_CONFIG_HOME`, and
`DOTTY_REPO`. The root case only pins `/` read-only. Descriptor-zero coverage uses
private simulated ownership; it never closes process stdin. Interference happens
before the final guard, not in the unobservable guard/syscall interval. Capability
and EXDEV cases inject errno without touching another filesystem. They are not
claims of native cross-device execution or missing-capability filesystem coverage.

Parent-managed clean-environment commands, not run by the implementation worker:

```sh
mise run fmt
mise run test:focused ./internal/nativefs '^TestNativeHandleBoundary$'
mise run verify
mise run vuln
```

`TestUnsupportedBuildTargets` runs in ordinary `mise run test` / `mise run verify`
on Go compiler host platforms (not Android, iOS, or WebAssembly). It uses the host
Go driver resolved from trusted `PATH` to an absolute path to compile this package's
tests for FreeBSD/amd64 and Windows/amd64, requires nonempty regular artifacts under
private `t.TempDir` paths, and never executes them. Each compilation has a two-minute
context timeout and a five-second output-drain wait. Combined stdout/stderr capture
is capped at 64 KiB; further bytes are drained and discarded, with an explicit
truncation notice and discarded-byte count in failure diagnostics. Failures retain
the compiler error, context state, and captured output; artifact failures also
include captured output. Go config/workspace/toolchain switching, inherited flags,
and `GOCACHEPROG` helpers are disabled before any Go subprocess, as are cgo and
network module lookup. `GOCACHE`, `GOMODCACHE`, and `GOPATH` are always overwritten
with private test-owned paths, never reused from the parent or linked to host data;
HOME/config/repository and compiler temporary paths are also isolated. Unsupported
stubs and common tests use only the standard library; any unexpected dependency
requiring network access fails rather than enabling a network fallback. Small
helper regressions cover output truncation/draining and inherited-environment
overrides. These compile-only safeguards leave the 40-case native inventory intact.

Both native Darwin and native Linux runs must record every required case and
OS/architecture/revision provenance. Unsupported cross-builds are compile-only,
not native acceptance evidence. Full verification, vulnerability checks, and fresh
independent review remain required. The private create/open and planned 31c flock
primitives do not select canonical user coordination, implement a transaction
runner, migrate callers, or supply fidelity or artifact authority.
User-lock adversarial fixtures must never use the real OS-user lock anchor.

## Authority observations — 31a

`AuthorityFacts` separates UID/GID, permission and special bits, link count,
filesystem type/ID/mount flags, and security evidence from unchanged `Identity`.
Facts expose value getters only; zero evidence is invalid. `AuthorityError`
retains exact paths, observed facts, primitive reasons, native causes, and safe
remediation. No C4 outcomes are assigned. Read-only observation validates the
complete pinned ancestry before, between, and after two authority observations;
observable changes refuse. Saved facts never bypass a later operation's policy.

The supported security model is deliberately narrow:

- Linux: descriptor `Fstatfs` must identify ext or tmpfs. Descriptor `Fgetxattr`
  reads access ACL presence, and directory default ACL presence. Only `ENODATA`
  establishes absence; a present value (even empty), unsupported retrieval, or
  read failure refuses. Unknown filesystems, including read-only overlay, refuse.
- Darwin/cgo: descriptor `Fstatfs` must identify local APFS with ownership
  enforcement and without union semantics. A small public-libc bridge retrieves
  `ACL_TYPE_EXTENDED`, validates it, checks `ACL_FLAG_DEFER_INHERIT`, inspects
  entries, captures errno immediately, and always frees allocated ACL storage.
  Darwin entry success is **0**, not POSIX's usual 1. Only `-1/EINVAL` for the
  first entry of a validated ACL establishes an empty list; other errors refuse.
  Only a NULL/ENOENT getter result triggers independent `fgetattrlist` on the
  same held descriptor: required `ATTR_CMN_EXTENDED_SECURITY`, only
  `FSOPT_REPORT_FULLSIZE`, fixed aligned 4096-byte buffer. Only a successful,
  complete length-12 / signed-offset-8 / reference-length-0 result confirms
  absence. Errors, nonempty, malformed, oversized, or truncated results refuse;
  no reference is followed and no mode/xattr fallback exists. Its mechanism is
  `acl_get_fd_np:extended+fgetattrlist:required-extended-security`. Security-read
  ENOENT retains its cause but does not diagnose a missing pathname.
- Darwin without cgo and other platforms: capability refusal before authority
  filesystem work. Release builds remain cgo-disabled: **production Darwin
  integration is blocked**, regardless of native cgo test results.

Private anchors require effective UID and exact 0700; lock-file policy requires
regular type, effective UID, exact 0600 and one link. Repository directories
require effective UID and no group/other write or special bits. System ancestors
require root/effective UID ownership and no group/other write or special bits;
`/` requires root, and only the fixed platform temporary root permits root-owned
01777. System ancestors are validation-only, never flock targets. The private create/open
and flock operations below use these policies internally; no canonical coordinator
or production integration is included.

The [slice contract](../../docs/plans/coordination-authority-slice.md) owns serial
gates and full inventories. New required aggregators are `TestAuthorityBoundary`
(12 required cases), `TestLinuxAuthority` (eight), and `TestDarwinAuthority` (14).
`TestDarwinNoCgoUnsupported` is a separate capability-limit lane. Inventories and
tests were written before production edits; no worker checks were executed.

Positive native fixtures use a test-only private security root and real native
readers. Physical identity/topology guards still cover **all** ancestors.
Production has no alternate root entrypoint, flag, environment variable, or
config setting. Unsupported production ancestry is tested separately and labeled
as injected versus observed. Darwin allow/deny/inherited ACL fixtures are real;
deferred-inheritance branch coverage is injected around a real retrieved and
validated ACL, **not native deferred-flag creation/detection evidence**. Failure
injection supplements real allocation/free and native positive checks. Natural
absent directory and regular-file cases use the production reader; allocated
validation/free cases use real ACL-bearing fixtures. A heap-only `acl_init(0)`
fixture separately preserves allocated-empty validation/enumeration coverage,
without a public filesystem-mutating fixture API.

The NULL/ENOENT confirmation decision follows oracle `86ffd4f1`, the public
required-attribute contract and pinned XNU `f6217f891ac0bb64f3d375211650a4c1ff8ca1ea`
(12377.1.9), plus supplied private APFS probe proc9579 PASS: eight fixtures and
four negatives. See the [probe notes](testdata/darwin-acl-probe/README.md).
This is not exact source mapping for runtime XNU 12377.91.3, universal filesystem
proof, or elimination of ACL races. The diagnostic probe is not sole acceptance;
the accepted 31a receipt in the slice contract owns that slice's evidence only.

## Private create/open — accepted 31b, production-unused

The [31b receipt](../../docs/plans/lifecycle-implementation-plan.md#milestone-1-private-createopen-31b-acceptance-receipt)
accepts `5735f31764c0a9bf6c818b5c2f1ad9337370ab41` within its bounded Darwin/Linux
native scope. It does not establish canonical CLI integration, flock or release
support. The following mechanism and lifetime limits remain applicable.

Open and create are separate: EEXIST refuses without fallback adoption. Existing
objects are never chmodded, repaired, truncated or removed. File flags are fixed
`O_RDONLY|O_NOFOLLOW|O_CLOEXEC|O_NONBLOCK|O_NOCTTY`, with `O_CREAT|O_EXCL` and
requested 0600 only for creation. Type is observed before an existing file open,
then FD/name/full ancestry are rebound and authority is observed repeatedly.
These flags do not eliminate every device-open race. The planned 31c native
process gates must verify read-only local flock support; 31b implies none.

Only an exclusively created, bound FD with native supported security, effective
UID, regular type, one link, and only umask-reduced 0600 bits may establish 0600
through fchmod. Exact mode and unchanged remaining facts are revalidated afterward.
Mkdir requests 0700 without chmod or process-global umask changes. A restrictive
umask result is retained and reported rather than repaired or deleted.

Across **successful mkdir**, the direct parent's directory nlink may differ.
Native APFS evidence (proc_e309: same parent identity/UID/GID/mode/filesystem/ACL,
recorded nlink 2→3 after exclusive regular-file creation) supersedes the earlier
mkdir-only assumption: successful exclusive **file creation** also permits this
one field difference, but only with `EvidencePresent` filesystem model
`darwin-local-apfs-ownership-enforced`, not GOOS or an unverified model string.
After recording creation and binding the held regular FD to its parent/name,
unchanged `privateParent(RolePrivateAnchor)` obtains two strictly equal guarded
postcreation observations. Identity, UID/GID, mode, filesystem/mount/security and
all higher-ancestor facts must match across creation before adopting that baseline.
Linux file creation, existing opens, failed creates, precreation and all later
comparisons remain strict; both pre-chmod file observations and post-chmod checks
remain unchanged. Actual observations are retained, never normalized or required
to differ by exactly one. The allowed APFS window is **not causal proof**: concurrent
entry changes cannot be attributed separately, and confer no child-inventory or
deletion authority. 31a readers/comparisons and `Dir.Authority` remain unchanged.
Independently repinned child directories must match both the leased parent's
entire chain and the first child observation. Mkdir success proves an event, not
which inode it created: an indistinguishable replacement before first observation
remains an observational limit.

`CreationRecord` and `CreationObservation` expose only immutable value getters.
They retain exact requested/observed paths and available identities/facts, never
creator-inode or deletion authority. Every failure within a create operation after
its successful creation syscall, including descriptor cleanup, preserves the
record in `CreationError` and joins all causes. No moved path or restoration is
inferred; retained/uncertain paths require inspection before retrying.

`TestPrivateCreateOpenBoundary` declares exactly the contract's 12 roots and uses
the maintained fail-on-missing/skip/filter inventory harness. Supporting tests
cover narrow mkdir and APFS exclusive-file-create transitions (native guarded
counts/records, injected model/field matrices, exact phase reachability and full
binding guards), copied/concurrent File.Close and parent leases, real native ACL
refusals, unavailable backends, and isolated-subprocess umasks. Strengthened
replacement and post-chmod assertions retain exact hook/chmod counts, paths,
reasons/causes and facts; hardlink aliases stay outside the direct parent.
Positive fixtures retain real native filesystem/security readers; only `_test.go`
cuts security ancestry above a private root, never physical guards. No canonical
anchor is accessed. Tests were written before production source; the worker ran
no tests, formatting, build, Git or validation commands, and claims no red run.

Parent red/green evidence and final platform checks are recorded in the receipt.
Changes must repeat independent review and fresh native evidence through reviewed
clean launchers, including formatting and `mise run verify` / `mise run vuln`.
Run whole required aggregators, not selected children. Preserve the 40 native,
12 common authority, 14 Darwin/cgo and eight Linux authority inventories. Unavailable
Darwin/no-cgo and foreign-target results are capability/compile limits only, not
native acceptance. Release configuration and the Darwin production blocker remain
unchanged.

## File/directory flock — planned 31c, production-unused

Each acquisition owns a new open file description, never a dup or the caller's
shared directory FD. File acquisition uses existing-only 31b open and retains
its original parent token. An immutable final File authority observation carries
its strict baseline into flock setup; File.Close semantics are unchanged.
Directory acquisition independently repins every original ancestry identity and
transfers its original operation token through Release.

Fresh guarded baselines survive opening/repinning and waiting. Full physical and
production security ancestry, leaf policy, and every identity/UID/GID/mode/ACL/
mount/nlink fact are compared before waiting and after acquisition. No creation
nlink exception applies. Selected `/` and the fixed temporary root refuse even
for UID 0; only the selected leaf is flocked.

`LOCK_EX|LOCK_NB` retries EINTR and waits on a context/Close-aware timer for
EWOULDBLOCK/EAGAIN. Unsupported errors refuse. Publication and Close share the
lifecycle mutex, without holding it through syscalls or blocking waits. Context
is checked immediately before publication; later cancellation does not revoke a
returned Lease. A losing publication race unlocks, closes and drops all tokens
once. Errors remain primitive evidence, never C4 or unchanged-state claims.

`TestFlockBoundary` defines the exact 15 slice-contract roots, with file/directory
variants and nested drift, failure, publication, and release regressions. Those
implementations, subprocess helper and initial unavailable/API assertions preceded
production edits; the final-observation cancellation regression was added during
source inspection. Source ordering is not a red-run claim. Real processes use bounded ready/go/
contended/acquired/release/released pipe handshakes, native read-only descriptors,
validated private fixture identities and native root authority before the test
security adapter. Contention proof is an observed native error, never a sleep.
Full physical guards still cover ancestors above the private security boundary.

The implementation worker ran no commands or checks. Independent review, parent
formatting, clean `mise run verify`/`mise run vuln`, both native OS inventories,
and unavailable/foreign-build limits remain required. Read-only flock support
is **unproven until those real-process cases execute**. Prior inventories and
Darwin's production cgo blocker remain unchanged; no 31c acceptance is recorded.
