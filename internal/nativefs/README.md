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
