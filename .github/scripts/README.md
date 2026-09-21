# Bounded native fixture CI — SOURCE PREPARATION ONLY

**Task34/33a bounded fixtures only. No production hosted-ancestry, canonical CLI,
or Milestone 1 compatibility claim. All changed/new regressions are UNRUN.**
Production `internal/nativefs` is untouched. Its full `/`-to-leaf authority still
refuses the historically observed `/home` default ACL, assuming earlier checks pass.
`fixtureAuthority` and `privateFixtureCalls` cut security observations only in Go
`_test.go` fixtures; complete physical topology checks remain. These cutoffs are
not imported into the CI harness, and production refusal cases remain required.

The earlier CI attempts 35548268496 and diagnostic 35551465637 are consumed.
The latter saved six directories: `/home` was root-owned 0755 with absent access
ACL and present default ACL; lower existing directories through `_temp` and
`_runner_file_commands` had both attributes absent. Those are historical facts,
not current authority, admission by bytes/UID/path, or evidence of native success.
A default ACL governs creation in its immediate parent, not existing lower
objects. The helper therefore requires the actual creation parent to be strict;
it never creates directly in an admitted higher default-bearing directory.

## Collector follow-up — source prepared, not performance evidence

Run **35603980436** subsequently passed the native command step and routine 110
helper regressions, but collection exited silently after about 120.084 seconds.
Publication refused missing readiness; no artifacts were available. Expiry is
strongly consistent with this observation, but the phase/hotspot remains unknown.
Command-step success is not independently audited native acceptance.

This follow-up computes the pure private-directory set once per
`directory_endpoint` call instead of four times. The closed operation map, strict
set (including intermediates), errors and all live ACL, descriptor/name, identity,
nofollow and privacy checks are unchanged. No filesystem authority is cached.
There is **no measured performance improvement or claim that this fixes expiry**.

Captured collection now emits console-only `collector-progress` records through
an owned duplicate of original FD2, established before private stream redirection.
Capture payloads still go only to their original private destinations. Fixed phase
labels are `binding`, `logparse`, `metadata`, `journalcapture`, `strictconsumers`,
`captureclosure`, `curation`, `finalinventory`, and `readiness`. Each has at most
one `start` and one `done` record: **18 records total, 256 bytes per record, 4608
bytes total**, including attempted short writes. Records contain only the fixed
labels/events and bounded numeric elapsed milliseconds, units and bytes; no paths,
root names, payloads or exception text. Elapsed milliseconds are bounded to
0–119999, units to 0–20000, and bytes to 0–96 MiB. There is no per-entry logging,
background timer/thread, retry or fallback file.

A `start` reports phase entry, not completion. `done` follows that phase's work:
binding counts one completed bind; logparse counts successfully read/decoded logs;
metadata counts observed nodes after traversal and revalidation (including refused
observations); journalcapture counts entries whose capture and post-write binding
check finished, with payload bytes for those entries (including hardlink aliases).
Strict-consumer units count completed command-receipt bundles, transcript checks,
and the root comparison/export/FatalOwner checks. Captureclosure counts one closed
capture plus exit receipt; curation counts closed copies after rereads and manifest
creation; finalinventory counts compared files including the manifest; readiness
counts one completed readiness write. These are work observations, **not PASS,
acceptance, publication authority or proof of complete native evidence**. A failed
phase has no earned `done` record. No final summary is emitted during unwinding.

The original shared **120-second** collection/curation/publication allowance starts
before sink setup and is unchanged. Deadline guards precede each emission;
`CollectionDeadline` still unwinds silently and nonzero, without post-expiry output,
footer, receipt or new write. Only the owned sink descriptor is closed, once, also
on failure. Duplication, write, short-write or close errors disable diagnostics and
force an otherwise successful collection nonzero; existing primary failures remain
primary. No diagnostic retry occurs. Captured collector failure suppresses further
progress while existing safe curation behavior is retained. Direct uncaptured
`collect()` remains non-diagnostic. Blocking OS operations and scheduling remain
outside a hard Python wall-clock guarantee.

Current prepared inventory: **125 definitions = 110 unchanged purposes + 15 new
portable fake regressions** (114 in `test_native_ci.py`, 11 unchanged parser tests;
68 Linux-dependent, 57 portable overall). Existing silent-expiry assertions needed
no adaptation: pre-expiry progress bypasses redirected private streams, while all
post-expiry no-output/no-write purposes remain unchanged. New cases cover label and
byte/record/counter bounds, original-sink routing during real capture redirection,
expiry before setup/emission and silent captured unwind, diagnostic errors and FD
ownership, primary-status preservation, actual collector/curation phase wiring,
consumer/readiness noncompletion, and once-per-call pure construction with the same
closed roles. The coverage follow-up adds a specified 18-byte journal payload and
24 curated copies through actual `collect(capture=True)`, capture, collection and
curation, retaining real JSON writes, tar construction and final upload inventory.
It asserts all 18 phase records in exact order with operation-driven elapsed values,
nonzero journal/copy counts and readiness completion only after its writer closes.
A failed copy reread earns no curation/final-inventory/readiness completion. Sink
writes that reach expiry then return short, raise OSError or raise CollectionDeadline
must unwind silently without new private completion, curation or another emission;
only the owned diagnostic FD is closed once. In-memory writer capabilities retain
the real deadline guards and abort-versus-close contract. The helper itself needed
no change for these coverage additions. All new assertions are **UNRUN**; AST
preparation is not execution or native evidence.
Parent owns authorized checks, Git, publication and the one new hosted attempt;
independent review and exact-candidate evidence audit remain required. Production,
consumers, workflow, mise tasks, native cases and ESRCH supervision are untouched.

## Prepared directory-role policy

Every opened ancestor retains directory, root-or-effective-UID, no group/other
write, nofollow descriptor, current-name, retained-identity and repeated checks.
Access ACL absence is mandatory everywhere. Both attributes are queried even when
one fails. Only ENODATA means absence; unsupported, unreadable or unknown refuses.
Readable higher default ACL bytes are not interpreted or whitelisted.

| Closed operation | Valid endpoint (both ACLs absent) |
| --- | --- |
| `create-root` | ROOT.parent; actual mkdir parent |
| `create-private` | ROOT; only fixed child names may be created |
| `bind`, `output`, `read` | ROOT or fixed PRIVATE/evidence/compile/upload control directories |
| `source-inventory` | WORK, with retained ancestry across source checkpoints |
| `preflight`, `collector` | ROOT/tmp |
| `export` | ROOT/evidence or ROOT/upload |
| `upload` | ROOT/upload |
| `publish` | ROOT |
| `github-env` | ROOT.parent/_runner_file_commands, after existing filename-role validation |

ROOT.parent, ROOT, every fixed PRIVATE/evidence/compile/upload directory and the
runner file-command parent remain strict even when another operation makes them
intermediate ancestors. Every endpoint is conservatively strict too. Roles are
hardcoded at operation calls, not CLI/environment/binding-JSON waivers or arbitrary
permission booleans. Unknown roles, wrong endpoints, incomplete/relinked chains
and unknown private child names refuse before creation/write. Chain extension
uses the validated bind role without demoting its actual creation parent.

Recursive source traversal keeps its existing metadata/name checks. Collector
journal traversal keeps its existing source-bound negative-input and metadata-only
rules: test-created adverse objects are not blanket-classified as harness control
directories. Regular-leaf guards are unchanged. Restrictive modes, metadata/name
checks and default-free creation parents are protections, **not a claim that every
regular leaf has an explicitly verified absent ACL**. Repeated observations are
not atomic exclusion of concurrent same-UID/privileged changes.

## Workflow source restoration and remaining gates

The unpublished workflow source is restored byte-for-byte from the parent-verified
baseline (319d76c), SHA256
`b2c8be0ec4f1c4d417e1ede090f23fb636d7c50673aa6d949aadcf27932b3c18`.
It re-enables ordinary verification and the prepared native route in source only;
it grants no commit, push, run, rerun or merge permission. Triggers, action pins,
permissions, operational budgets and native gate counts match that baseline.
The read-only, always-nonzero `diagnose-acl` CLI remains available but is not selected
by the restored workflow. Its observation/byte budgets and no-progression behavior
are unchanged. The diagnostic routing regression now asserts the restored route
and exact workflow hash, rather than claiming the diagnostic is still selected.

Prior role-policy inventory: **110 definitions = 98 retained purposes + 12 new portable
role-policy definitions** (99 in test_native_ci.py, 11 unchanged parser definitions).
The Linux test-only host-ancestry cutoff only adapts to the policy signature; it
never becomes CI policy. New fake-descriptor tests cover higher default-only
admission; access ACL refusal at every node; both ACLs on fixed strict roles and
intermediate roles; unknown/mismatched endpoints; creation refusal before mkdir;
chain extension/demotion; ACL errors; symlink/owner/mode refusal; identity/ACL drift;
output/read/GITHUB_ENV refusal before leaf open; and all ancestry caller wiring,
including source checkpoints, preflight, collector and upload. Source wiring is
not dynamic coverage of every end-to-end caller. No prepared test was imported or
executed. Counts describe definitions, not passing tests or native gates.

Remaining gates for this follow-up: independent source/control review; parent-owned
local mise checks, portable/Linux helper tests and real verification; parent
publication and the authorized exact-candidate attempt with fresh target checks;
then complete downloaded evidence and independent bounded-fixture audit. Neither
local success nor a green future workflow alone accepts Task34/33a or completes M1.
No readiness claim, new attempt, ACL mutation, sudo, signal, resource or cleanup
permission follows. Historical notes below are design/history, not current receipts.

## Route and triggers

`ci.yml` retains the ordinary `ubuntu-latest` verification job. One additional
non-container `ubuntu-24.04-arm` job handles ordinary **same-repository PRs**, checking
out `pull_request.head.sha`, not the synthetic merge SHA. Fork PRs and pushes to
`main` do not execute the native job. The existing PR event defaults remain:
opened, synchronize and reopened. There is no new dispatch, label, remote trigger,
service, image, registry or API controller.

This avoids a second native job on the subsequent main push; it does **not** promise
one platform execution forever. Further PR updates/reopens and separately approved
operator reruns can create additional attempts. No retries, fallback runners,
concurrency cancellation or automatic reruns are configured. Job timeout is 90
minutes, native work 75 minutes, collection/publication/upload five minutes each.
Collection and publication share the original 120-second internal window; the
publication step receives only its remaining time, never a new allowance. The job timeout
is an outer bound, not a guarantee that collection gets its entire allowance.

## Trust, isolation and acquisition

The new trust boundary is a GitHub-hosted ephemeral VM. It is **not** equivalent to
image a9, UID10001, Docker init, capability dropping, or network-none. Network and
passwordless sudo availability are platform properties; the helpers never use
sudo, privileged namespaces, Docker, an installed Dotty, or adverse canonical UID
anchor fixtures. Ordinary source-bound tests retain their existing private-anchor
boundary. The production CLI/nativefs code and CGO-disabled release policy do not
change. Linux acceptance explicitly records and requires `CGO_ENABLED=0`.

Preparation creates exactly one previously absent 0700 root:
`$RUNNER_TEMP/dotty-native-$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT`.
HOME, all XDG roots, TMPDIR, fake DOTTY_REPO, Go caches/modules, mise directories and
compile outputs are private children. Root/directory device, inode, UID/GID and mode
bindings are recorded and checked. Before any root creation, every ancestor must be
a nofollow-bound directory owned by root or the effective UID, with no group/other
write bits and **ENODATA** for its access ACL. Both ACL attributes are queried;
only ENODATA establishes absence. Readable default-ACL presence is admitted only
on existing higher traversal-only directories. ROOT.parent, ROOT, fixed private
control directories, the runner file-command parent and every traversal endpoint
require both ACLs absent, including when intermediate. Missing ACL support,
unreadable/unknown ACL observations and unsafe metadata refuse. Creation is exclusive and
descriptor-relative; current names/FDs are checked before each mkdir. There is no
chmod-parent, sudo, alternate root or adoption fallback. Hosted `RUNNER_TEMP`
ancestry may be unsupported: this preparation does not assume it passes.
Evidence writes retain nofollow ancestor descriptors and their original bindings,
create leaves exclusively relative to those descriptors, and check name, ownership,
single-link status, size and metadata around bounded writes. No evidence path is
opened with truncation. `GITHUB_ENV` is only the runner's
`$RUNNER_TEMP/_runner_file_commands/set_env_<UUID>` regular, owned, single-link
file capability under trusted current ancestry: nofollow bounded append (64 KiB
append, 1 MiB resulting file), never creation or truncation. Unsupported ancestry
or unsafe early-bootstrap output means platform logs and explicitly missing evidence,
not permission to write another path.
Neither cached Go test results nor restored Actions caches supply native evidence.
`GOFLAGS` remains empty.

Later preflight refuses a non-hosted runner, non-Linux/ARM64, root/effective-ID
mismatch, unknown image identity, installed Dotty on PATH, unsupported actual
source/test filesystem (only ext/tmpfs), unexpected/unknown ACL evidence, inadequate
capacity, or failed reaping. It probes private file ownership/mode, hardlinks,
symlinks and FIFO creation; probes direct-child and orphan reaping with **ESRCH
only**, never treating a zombie, reused PID, EPERM or UNKNOWN as absence. The native
families remain the actual capability tests, not a waiver based on preflight.

The 8 GiB / 100,000 available-inode floor is conservative working headroom for
ordinary tool/module acquisition, Go builds and bounded evidence; it is **not** a
measured reservation or whole-run capacity guarantee. Each `capacity-N.json` saves
the actual path, filesystem and available bytes/inodes **before** its threshold is
checked. Refusal states that path, both measurements and both floors. A full
`capacity.json` requires all three paths to pass. No capacity bootstrap or cleanup
predecessor is introduced.

Tool acquisition is ordinary future network-enabled CI work. The pinned mise
bootstrap action does not install project tools or restore/save caches in the native
job. Captured `mise install` then acquires the versions in `mise.toml`. Bootstrap
checkout/mise action diagnostics live in the platform action logs; command-level
raw capture begins in the prepared runner. Bootstrap failure cannot be acceptance.
No custom secrets are passed; workflow permission is `contents: read` and checkout
uses `persist-credentials: false`.

| Pin | Value |
| --- | --- |
| checkout v6 | `d23441a48e516b6c34aea4fa41551a30e30af803` |
| mise-action v4 | `c2a87611a18de5b3828c5652fe268e992400cb5c` |
| upload-artifact v4 | `ea165f8d65b6e75b540449e92b4886f43607fa02` |
| mise bootstrap | `2026.9.1` |
| Go (`mise.toml`, sole version authority) | `1.26.6` |
| Existing project tools (`mise.toml`) | yamlfmt `0.21.0`, golangci-lint `v2.12.2`, GoReleaser `v2.15.0`, govulncheck `v1.3.0` |

setup-go is omitted rather than duplicating Go's version authority. Ordinary CI
still installs project tools through mise and runs its same checks. The test task's
optional argument change is `DOTTY_NATIVE_ACCEPTANCE=1`: fixed `-v -count=1` flags apply
**only to go test**. Unset/empty preserves `go test ./...`; unexpected nonempty values
refuse. Build/vet never inherit invalid test flags. Native execution requires value1.

### Test fixture creation prerequisite (current repair)

Both `mise run test` (also called by `verify`) and `mise run test:focused` now
establish process-local `umask 077 || exit "$?"` before executing their commands.
Failure to set the mask stops execution. This intentionally changes the creation
policy, not the default `go test ./...` arguments, native opt-in branches, or focused
argument forwarding/completion semantics; it does not change the invoking shell's
mask. Native CI already sets its own private mask.

Go 1.26.6 creates the numbered `testing.T.TempDir` child with requested mode 0777;
under Unix masking, 077 yields the exact 0700 required by private-anchor fixtures.
A private parent alone is insufficient. Ownership, ACL and filesystem authority
checks still apply. No fixture chmod, production policy relaxation, Go-global mask
or CGO change is introduced. Isolated private-umask subprocess tests still override
the inherited mask intentionally **after** their fixture setup.

Prior local verification passed format/lint/vet with CGO enabled and explicit
`/usr/bin/clang`, then reported nativefs private anchors at 0755 instead of 0700.
The capture was truncated; the actual inherited mask was not recorded. The prior
host wrapper set 077, whereas the failing wrapper omitted it: this explains a
creation-policy mismatch, not proof of that process's historical mask. The parent
must separately correct its local wrapper before any authorized post-review check;
this repair neither reruns verification nor modifies retained failed fixtures.
Local/non-Linux checks cannot establish native Linux acceptance or waive its
capability gates; this prerequisite alone does not establish filesystem support.

Pre-diagnostic source inventory: **81 prepared test definitions** (68 Linux-dependent,
11 portable parser, two portable routing/source definitions). All prior 80 purposes
are retained. The new portable source regression asserts fail-closed mask ordering,
exact default/native/refusal branches, and unchanged focused usage/forwarding; its
assertions are **UNRUN**. The historical checkpoint notes and hashes below are not
current validation receipts. Current repair hashes are in the tool-bound report.

## Provenance and ordered gate map

Context records distinguish PR candidate SHA, event SHA/ref, head/base/merge event
fields, workflow SHA/ref, run/attempt, actual image/architecture/effective IDs and
tool versions. Event input is hashed, with only selected fields retained (not the
entire environment). Source records include the commit object, tree identity, every
tracked blob hash and Git/executable mode, plus actual mode and SHA256, without
source exclusions. Candidate HEAD/worktree are checked before acquisition, after
workflow acquisition, and after successful verification. Each checkpoint also
walks the actual worktree including ignored/untracked names; only the actual
root `.git` administration directory (bound by Git's absolute-git-dir observation)
is excluded. Unsupported Git-administration layouts refuse. Unexpected files or
directories, including Go and Go-test sources, refuse. Only the final checkpoint
permits the exact root `/dotty` build output: regular, effective-UID-owned, single
link, executable, no group/other write or special bits, bounded and hashed. This
is not an arbitrary ignored-source allowance. Tracked bytes/executable modes stay
Git-bound; actual tracked modes must also equal the first checkpoint. Inventory
bounds: 20,000 names, depth32, 8 MiB/source file, 64 MiB/build output, 128 MiB total.
Helper imports disable bytecode writes before loading sibling code; workflow and
test-discovery commands also use `-B`.

The workflow commit can differ from candidate HEAD. Exactly one noninteractive,
credential-free fetch with a 60-second command deadline from the fixed public
`https://github.com/lmilojevicc/dotty.git` acquires the exact recorded workflow SHA
into the disposable checkout's object database. This **writes Git objects**, not
source files. Replacement objects, hooks, tags, submodules and FETCH_HEAD writes
are disabled for acquisition; no alternate URL/ref or fallback exists. Raw commit,
three tree-link objects and ci.yml blob are hash-authenticated and recorded with
modes, separately from candidate source. Fetched workflow content is never executed.
Failure to obtain/authenticate it refuses acceptance and retains available evidence.
The same owned-command mechanism below supplies finite termination/drain; no
external `timeout` wrapper, retry or alternate-ref acquisition is used.

| Order | Gate / preserved obligation |
| --- | --- |
| 1 | Source/workflow binding; actual native environment/capacity/reaping preflight; captured tools/modules acquisition; formatting check |
| 2 | All 24 `PROTOCOL_ORDER` prerequisites, in the inherited order, individually through `mise run test:focused` |
| 3 | Exact 42-case `TestFocusedTaskProtocol` matrix |
| 4 | Required cancellation descendants and child-invocation/error/signal cases |
| 5 | `TestRegularObservationBoundary` and all required drift/cancellation descendants |
| 6 | `TestCoordinatorBoundary` plus correlated private native child proof |
| 7 | `TestFlockBoundary` plus four correlated child proofs |
| 8 | `TestPrivateCreateOpenBoundary`, nested cases and five isolated umask proofs |
| 9 | `TestAuthorityBoundary` |
| 10 | `TestLinuxAuthority` |
| 11 | `TestNativeHandleBoundary` |
| 12 | Real `mise run verify`: format/lint/vet/uncached verbose Go tests, stdlib helper regressions, build; exact protocol/native/supporting cases rechecked |
| 13 | FreeBSD/arm64 and Windows/arm64 nativefs **compile only**, never execute foreign binaries |
| 14 | Actual new `mise run vuln`, source post-binding, unconditional diagnostic collection/upload |
| 15 | Later download and independent exact-candidate evidence audit; scoped acceptance/integration decision |

No skips, blanket FAIL suppression, selected-case waivers, historical before_anchor
allowance, or ESRCH weakening. The sole expected FatalOwner child failure retains
its strict transcript and conditional FD9 witness checks. Darwin vulnerability
results are not relabeled as a Linux scan; this preparation ran no scan.

## Evidence and failure semantics

Each launched command has argv/cwd/explicit override data, separate raw stdout and
stderr, an arrival-order combined transcript, EOF/drain facts, original exit and
hash/size receipts. Eight MiB per command is the inherited transcript bound. Overflow
immediately becomes failure and enters bounded termination/drain; it cannot drain
forever or become complete evidence. Known original nonzero exits survive store,
interruption, teardown and receipt errors. Missing/unwritable receipts never prove
an unobserved exit; retained logs and the primary failure remain the recovery data.

The local command supervisor starts one private session/group. Linux
`waitid(WEXITED|WNOHANG|WNOWAIT)` observes but reserves the direct leader through
**all** mutating signals. TERM then KILL target only that still-owned group, with
fresh child/session/group checks. No `Popen` context manager, `poll()` or early
reap releases its identity before signalling. A final nonblocking wait releases
the leader; after that, only signal0 absence observations are allowed, and only
ESRCH proves this group's absence. Present/reused/EPERM/unknown refuses and retains;
there is no stale-PGID cleanup. `/proc` session-peer observations (65,536 entries,
250 ms within the existing five-second shutdown envelope) are
metadata, never authority to signal those peer IDs or non-owned groups. An escaped
new-session descendant cannot be proved absent by this mechanism; receipts state
that limit, and retained pipes or observed remaining session peers refuse. Scan
budget/errors mark diagnostics incomplete but do not bypass bounded owned shutdown:
fresh signal-ownership checks still precede TERM/KILL. Failed ownership still forbids
signals. Absence-loop requested sleeps are clamped to the remaining deadline.

Proposed per-command limits (not measured/approved operational budgets):

| Command | Deadline |
| --- | --- |
| workflow fetch | 60 s |
| mise install | 900 s |
| module download | 600 s |
| real mise verify | 1,800 s |
| each focused test lane | 120 s |
| remaining commands, including vuln and foreign compile | 300 s |

Poll/select waits are at most 50 ms. An exited leader gets at most 2 s of pipe drain
within the command deadline; termination adds TERM grace2s, KILL/drain grace2s and
absence observation1s. Fetch therefore has a nominal total ceiling65s, not an
unbounded TERM-only timeout. Commands also share the existing 75-minute run window
with eight seconds withheld for termination/receipts. The 90-minute job and 75-minute
step budgets are **not increased**. Scheduling delay and hard-blocked filesystem,
exec, kernel or final evidence writes remain outside a Python wall-clock guarantee;
platform timeout/cancellation may still lose evidence. These are not clean-state or
all-descendant-absence claims.

The independent always-run collector preserves available failed/unfinished lanes
before strict semantic checking. It inventories declared **and undeclared** protocol
roots only inside the bound private TMPDIR, retains ambiguous attribution as unknown,
and reports missing records/lanes. It does not require PASS to save journals. Nested
RUN/NAME/outcome attribution, positive-removal proof and source-required journal
rules are inherited. Real captured paths are never rewritten to `/state`.

Reads use descriptor-relative nofollow opens, nonblocking regular-file reads,
current name/FD binding and metadata checks, bounded enumeration and before/after
observations. The curated `journals.tar` preserves original journal names, modes,
numeric ownership, FIFO metadata, symlink text (bounded metadata only, never followed),
and internal hardlink relationships. Metadata is inventoried first, with expected
UID and permitted-role checks. Every regular inode's complete permitted-name count
must equal `st_nlink` **before any payload read/export**. Outside/unapproved or
incomplete alias sets retain metadata/refusal only: their bytes and base64 payloads
never enter the upload directory. All approved alias names remain in the inventory
and archive. Original root/ancestor bindings must survive reopening; they are not reset to a
replacement. Complete parent/name/alias authority is checked before and after
payload reads, before JSON/tar commitment. Symlink identity/ancestry is checked
again after readlink and before its text is emitted. Final inventory checks remain; strict exported-record checking independently checks UID and
alias counts before decoding. Evidence logs/receipts require single-link ownership
before reading. `journal-inventory.jsonl` preserves original absolute paths,
identities, nanosecond timestamps, approved payload hashes/bytes and refusals. Negative
fixture directories are metadata-only, not arbitrary traversal roots. Unexpected
special objects are inventoried and refused, never opened or executed. The inherited
512 roots / 20,000 entries / depth8 / 2 MiB file / 64 MiB payload / 96 MiB inventory
bounds remain. Observed regular sizes reserve the remaining 64 MiB aggregate
allowance before open; reads cannot exceed that size, and aggregate exhaustion
stops subsequent payload reads while bounded metadata/refusals may continue.
Collection has a fatal 120-second internal deadline covering binding,
log reads, metadata inventory, payload capture, semantic checks, final writes and
CLI upload curation,
with cancellation only in the outer `finally`. `CollectionDeadline` propagates
through both capture handlers and the production collection/curation wrapper.
The original absolute clock also guards output creation and every unbuffered data
write, including archive footers and publication writes; it is never renewed.
Deadline unwinding closes owned descriptors without flushing buffered text or
adding archive completion data. No new private diagnostic, exit receipt, curation
or readiness write starts after expiry. Already written partial artifacts (including
an interrupted receipt) remain incomplete, never a collection-complete proof.

Do not extract the archive over live paths: audit tar members as data. No whole
HOME, credentials, cache, tool installations, foreign binaries or ordinary temp
workspace is uploaded. Working `evidence/` is retained but **never uploaded**.
The conditional always-run upload route targets only fresh, exclusively created
`upload/`, and only after the separate publication step succeeds. Curation
accepts exact names/roles from `upload_roles()`, never recursive content:

| Upload role | Exact namespace and byte bound |
| --- | --- |
| Command streams | `COMMAND_LANES` + `.stdout`, `.stderr`, `.log`: 8 MiB each |
| Command metadata | Same lanes + `.command.json`: 16 KiB; `.result.json`: 4 KiB |
| Helper capture | `main`/`collector` + `.stdout`/`.stderr`: 8 MiB; `.exit`: 16 bytes |
| Fixed receipts | Explicit binding/context/source/workflow/capacity/preflight/main-status/collection-checks names: 8 MiB each |
| Collector artifacts | `collection.json`, `journal-inventory.jsonl`, `journals.tar`: 96 MiB each |

The archive's wire cap does not increase the journal payload cap of 64 MiB.
Input enumeration remains bounded by 20,000 names; output enumeration is bounded
by the finite allowlist plus its manifest. Each input must be owned, regular,
single-link and currently ancestry/name/FD-bound. Its entire bounded read and
post-read checks finish **before** an upload output is opened. Unexpected names,
symlinks and outside hardlinks retain refusal metadata, never their payloads;
there are no hidden raw staging copies in `upload/`. Validated partial artifacts
survive failed semantic collection. `upload-manifest.json` lists the exact exported
names, roles, sizes and SHA256 hashes (excluding itself), refusals and missing
allowed names. Curation completeness is not test/collection completeness.

Each successfully closed output writer retains its full non-atime leaf metadata
(including device/inode), byte count and streamed SHA256. Copied payloads must match
the approved input size/hash. The generated manifest retains its own writer record.
After manifest creation, the complete observed inventory must equal those original
writer records, not just their names; observations never replace the baseline.
Same-name different-content or same-bytes/new-inode replacement, including manifest
replacement, refuses before readiness. After that comparison, curation exclusively
writes `upload-ready.json` outside the upload namespace using the verified writer
baseline. Readiness binds candidate SHA,
run/attempt, namespace identity, every file's metadata/hash and the original
120-second clock origin/deadline. A separate `publish` step revalidates that same
namespace without re-curation, rejects stale/mismatched/expired/missing state,
exclusively writes one-use `upload-publication.json`, then appends only the fixed
`DOTTY_NATIVE_UPLOAD_PATH` marker through its current runner environment capability.
That publication environment file must initially be empty. Preexisting markers or
consumption records refuse; failed namespaces/readiness are retained, never deleted
or retried. The upload action requires **both** publication-step success and the
exact run/attempt path. A partial marker append, post-append failure or detected
output drift cannot authorize upload. There is no fallback upload path. Missing
readiness/publication yields an explicit step failure and skipped upload; failed
bootstrap never grants collection/publication authority over a preexisting root.

Rejected input leaves can still yield safely curated partial/refusal artifacts.
Original native/collector failure exits remain failures even if publication and
upload subsequently succeed. The publication marker is eligibility only, not native
acceptance. Readiness and consumption records are retained working state, not
additional raw upload content.

Curation/publication are observed boundaries, not a permanent atomic snapshot against
later same-UID external mutation. Races, unavailable inputs, failed writes or a deadline refuse;
partial exports without a complete matching manifest are incomplete. If authority
cannot be established, only platform diagnostics may survive. No unsafe path is
adopted to create a refusal receipt. Raw main/collector capture now uses the same
bound writer rather than shell redirection. Only `upload/` is uploaded, with
`if-no-files-found: error` and requested retention **90 days**, subject to platform
limits. That input catches zero files, not individual missing records; the collector
and independent audit must check the latter. Collection failure after main exit0
fails the job; collector/upload failures never rewrite `main-status.json` or turn a
known original failure into success. Raw main/collector streams remain separate.

There is no helper deletion, prune or recovery action. Existing test-owned positive
cleanup semantics are unchanged. Hosted ephemeral teardown eventually removes VM
state; it is not evidence-preservation or user-authorized cleanup of old resources.
Publication-gated `always()` upload is best effort: cancellation, timeout, disk/network failure or VM
loss can prevent it. Missing upload, incomplete capture, missing required records,
cancelled/lost run or failed gates means **NO acceptance**, not a retry grant.
Even a fully green workflow and collection-checks record are not Task34/33a/M1
acceptance. Independent download/audit remains mandatory.

## K reuse/adaptation map

Input: `focused34-kitcore-declarations-wpkf1mln/templates/native-evidence.py`, SHA256
`c1b6360798d775fe0a594797b80473a515aed7e860191c85da7b379b01749ca5`.
Its historical pure125+33 acceptance does **not** transfer to this adaptation.

- AST-identical definitions: `fatal_child_lines`, `reject_failures`,
  `protocol_inventory`, `check_text`, `check_coordinator`, `check_flock`, `check_umask`,
  `export_metadata`, `export_encoded`, `export_decoded`, `_check_positive_removal`,
  `protocol_case_events`, `protocol_root_roles`, `diagnostic_root_roles`,
  `protocol_entry_role`, `protocol_required_records`, `diagnostic_entry_role`,
  `check_fatal_owner_witnesses`, `_check_export_entry_count`.
- Adapted definitions: `check_protocol` binds the dynamic TempDir child case to the
  actual new namespace; `check_export_records` requires the observed nonroot CI UID
  instead of UID10001 and now validates every entry's UID and complete permitted
  regular-inode alias sets before decoding. Neither changes required cases/outcomes
  or conditional FD9 witness scope.
- New `configure` binds root grammar/UID once. The dynamic `PROTOCOL_CHILD` path is
  assigned there; `PROTOCOL_ROOT` starts unbound. Other case inventories, descendants,
  counts, order, record roles, witness rules and bounds are retained.
- Omitted: K controller IDs, run-log/transport/export framing, Docker paths, runtime
  dispatcher and archive transport wrapper. `native_ci.py` contains the CI-specific
  sequencing, capture, provenance and curated archive adaptation only.

## Earlier checkpoint: prepared regressions and remaining approvals

**77 prepared regressions, all UNRUN**: 11 portable parser definitions in
`test_native_evidence.py`; 65 Linux-dependent definitions and one portable routing
definition in `test_native_ci.py`. All 67 inherited definitions remain AST-identical
relative to `ci-second-review-3nia6vl2`, preserving all prior purposes. Parser tests now reach complete configured-root declarations
and exports with actual UID, combined `check_text(..., 'verify')` plus a singly
removed required descendant, and qualified/unqualified FatalOwner enter2 schedules
with missing/altered frames and mandatory-record omissions. Capture/collector tests
exercise the actual local supervisor, signal-before-release ordering, bounded
retained/escaped pipes, overflow, interruption/store failure, nofollow/drift,
metadata-first external/unapproved hardlinks, unsafe/replaced private parents,
whole-collection deadlines after a retained member and during semantic validation,
and isolated missing record/lane/receipt refusals after preservation. Synthetic
consumer-wiring fixtures mock only full transcript checking; real attribution,
required-record/export checks and collection run, with a complete positive baseline.

The previous 26 additions prepare scan-budget/error shutdown ordering and known-exit
preservation, live-deadline shutdown, deadline-clamped absence waits, captured-child
private environment, output symlink/hardlink and ancestor swaps, runner environment
capability append/role/bounds/refusals, direct-evidence hardlink/unexpected/hidden-name
upload exclusion, partial-artifact upload, input-read curation races, actual collector
root/tmp reopen races, regular-read/readlink/parent-rename races, and aggregate limits
counting real payload read/open calls. Publication cases prepare output drift with
no readiness/marker, output hardlink revalidation, partial marker-write and
post-append failures, stale/preexisting marker and candidate/run/attempt mismatch,
expired/invalid/missing original clock state, and safe partial one-use publication
after semantic failure. Unsupported routing is tested as routing only,
not mocked Linux capability proof. Every definition and subcase remains **UNRUN**.

The latest 10 additions prepare a positive closed-writer/readiness baseline; actual
curation replacements with different sentinel bytes, identical bytes/new inode, and
an identical-byte replacement manifest (each checks a positive baseline first);
production `collect(capture=True)` expiry during reads, semantics, partial final
receipt writes and archive footer initiation; both capture-handler propagation
paths; and the production curation-wrapper deadline path. Deadline cases retain
and compare all pre-expiry artifact bytes, require no subsequent exit receipt,
curation or readiness, and use a controlled clock with alarm calls mocked. They
are prepared regressions, not live signal or runtime evidence.

Selected K pure builders are embedded, not imported at runtime: `evidence` (renamed
`native_log`), `umask_block`, `flock_facts`, `flock_fixture`, `flock_block`,
`coordinator_facts`, `coordinator_fixture`, `coordinator_block`, `support_evidence`,
`full_evidence`, `fatal_seed`, `protocol_seed`, `export_entry`; the four protocol
inventory data maps come from K's `CASE-INVENTORY.json`. `fatal_witness_seed` adapts
`FatalOwnerWitnessChecks.seed` from K's `strict-consumer-regressions.py`. Adaptations
construct synthetic paths in the configured namespace, set export UID/GID to the
actual test IDs, and give standalone synthetic regular inodes one link. No captured
text is rewritten, no controller or historical PASS is reused.

Relative to snapshot `ci-fixes-review-svyr2ylp`, `native_evidence.py` and all
11 parser regressions/13 reused K fixture builders are byte-identical. Native case
inventories and conditional FD9 gates are unchanged. All 30 previous CI test
names remain; only the store-failure injection moved from pathname opening to
the bound-writer seam, and the capacity fixture no longer mocks Linux. Setup now
binds private environment paths and skips unsupported ordinary hosts before setup.
`native_ci.py` changes remain ordinary local supervision, output authority, evidence
collection and curated-upload helpers; there is no API/resource controller or new
production Go/release behavior. The source-only hash inventory below excludes this
README's self-hash; the final tool-bound report records all seven source hashes.
The latest narrow checkpoint changes only this README, `native_ci.py` and
`test_native_ci.py`. Workflow, mise tasks, consumer and consumer tests remain
byte-identical to `ci-second-review-3nia6vl2`. Process supervision, source-object
binding, native inventories, platform routing and private-environment setup are
unchanged. This full checkpoint is ready for **independent source review**, not
runtime or native acceptance.

One task, `mise run test:native-helpers`, uses Python stdlib unittest discovery and
is wired into real `mise run verify` and the ordinary CI check sequence. Python
**3.9+** is checked before helper/test imports and the actual interpreter remains
recorded in runtime context; no same-binary attestation is asserted. Supervisor/ACL
regressions require nonroot Linux with actual supported POSIX ACL absence,
waitid/WNOWAIT and `/proc`. The Linux class is guarded **before setup**: ordinary
Darwin discovery explicitly skips it while portable parser/routing tests remain
active. `DOTTY_NATIVE_ACCEPTANCE=1` on an unsupported platform/capability fails,
never green-skips acceptance. Actual Linux ACL failures remain failures.
Tests create private temporary subtrees, bind HOME/all XDG/TMPDIR/DOTTY_REPO,
Go/mise paths and Python's tempfile cache for themselves and actual children,
restore the prior environment/cache afterward, retain roots and print their
locations; no cleanup hook deletes them. Pure parser/routing tests create no
filesystem fixtures or children. Tests bypass authority claims
for their pre-existing public host ancestry only, while applying the actual policy
within each test-owned subtree. They never chmod or adopt public parents.

Only source reading/writing, hashing and AST preparation were performed. No imports
of prepared modules, tests, shell syntax, formatting, linting, verification, tool
installations, Git/gh commands, Docker actions, remote runs or uploads occurred.
Parent owns index inspection and all Git integration; no staging was performed.

Remaining gates: independent source/control review; separately authorized parser and
collector regressions (including runtime filesystem drift and interruption tests),
workflow/task syntax/format review, real `mise run verify`/vuln/native checks in the
approved isolated environment; parent publication/exact execution decision; actual
runner/tool/capacity observations; complete downloaded artifact audit. No local or
native success is claimed. Task34 closure does not accept33a or finish Milestone1.

## Earlier checkpoint source hashes (not current)

Data-only SHA256 observations; all behavior remains UNRUN. README self-hash is in
the final findings report.

| Source | SHA256 |
| --- | --- |
| `.github/workflows/ci.yml` | `b2c8be0ec4f1c4d417e1ede090f23fb636d7c50673aa6d949aadcf27932b3c18` |
| `mise.toml` | `76df106cbc9150c69f478f860d28513bd129eb2a28dee14a8881451b16e83140` |
| `.github/scripts/native_ci.py` | `d68c7275a8033e547a6061115d3de4c162a40510859537e8437b6dc35e1fbb97` |
| `.github/scripts/native_evidence.py` | `a919569beb4245548c1db1b8669301b1b992d2c8c00a43eb614d6a37c13d11a0` |
| `.github/scripts/test_native_ci.py` | `f7dd704d70dbcbdbe2e5a39744d092bcabe7d612bc3c2e6a948346a4b1dd46fb` |
| `.github/scripts/test_native_evidence.py` | `329880c00eea9b2156ee6f3ee8df362ee3fea809164ca2c5cbfb6de7cdca6ca1` |
