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
- `(*Dir).Close() error` blocks new leases, waits for active operations, and closes
  every owned descriptor once. Copies of a wrapper share that lifetime.

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
independent review remain required. This slice adds no user-lock creation API,
coordination, transaction runner, caller migration, fidelity, or artifact authority.
Future user-lock adversarial fixtures must never use the real OS-user lock anchor.

## Authority observations — 31a candidate, not accepted

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
01777. System ancestors are validation-only, never flock targets. This candidate
adds no file open/create, private directory creation, creation records, flock,
canonical coordinator, or production integration.

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
fresh reviewed 31a native gates remain pending.

Parent must obtain independent review and fresh native evidence through reviewed
clean launchers before 31b. The supplied host launcher's `fmt` action only checks
formatting; formatting changes require a reviewed clean formatting action. Do not
run a child-only inventory filter or claim compile-only results as native gates.
