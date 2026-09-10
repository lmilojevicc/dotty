# Darwin required-attribute probe (test validation only)

`TestDarwinRequiredACLProbe` builds `main_darwin.go` with the host Go driver
(`runtime.GOROOT()/bin/go`) into a private `t.TempDir`, then executes that
artifact directly. It never uses `go run`. `ExtraFiles[0]` supplies the private
fixture as fd 3; no fixture pathname is passed to the executable.

## Protocol and limits

One argument selects `normal`, `invalid-fd`, `short-buffer`, `invalid-request`,
or `truncated-buffer`. One JSON object reports syscall status and captured
errno; fstat errors, device/inode/mode and before/after identity stability;
buffer capacity, reported length, signed reference offset, reference length,
and fixed-field validity/completeness. Exit status reports protocol execution,
not syscall success. No ACL payload or user/group GUID is emitted or parsed.

The public libc `fgetattrlist` request contains **only** common
`ATTR_CMN_EXTENDED_SECURITY`, with `FSOPT_REPORT_FULLSIZE`. There is no
`ATTR_CMN_RETURNED_ATTRS`, `FSOPT_PACK_INVAL_ATTRS`, raw syscall, or private ABI.
The aligned buffer is fixed at 4096 bytes. Oversize results are incomplete,
not absence; the probe neither follows references nor resizes/retries.

The test checks the raw absent-object shape: successful complete result,
length 12, reference offset 8 (relative to the reference at buffer offset 4),
reference length 0. Any nonempty security data is not absence. This diagnostic
is **not** production-reader acceptance by itself. Errors are not absence.
Negatives use fd -1, a three-byte advertised buffer (valid allocated memory),
bitmapcount 0, and a four-byte truncation buffer respectively.

## Fixture privacy and subprocess bounds

Fixtures are naturally created private directories and regular files; absent
fixtures are not ACL-cleared and do not require `authorityFixture` or
allocated-empty ACL validation to succeed. Real allow, deny, and inherited ACLs are added
only beneath the private root with existing bounded `/bin/chmod` fixture
methods. The inherited entry flag is checked using the existing libc getter.
The getter's NULL/errno observation is logged for correlation only; it does
not establish absence. No fixture mutation hook is added to production.

The parent and probe fstat the descriptor before/after probing, compare
identity and stable metadata (not access time), and match fd 3's identity to
the parent's fixture. These checks do not claim security ancestry authority
or protection against uncooperative concurrent writers.

Build timeout: two minutes. Probe timeout: ten seconds. Both have a one-second
WaitDelay and capped, continuously drained output via the existing 64 KiB
writer (separate stdout/stderr for the probe). Child environment starts empty:
private HOME/config/repo/temp/caches, no GOCACHEPROG or inherited compiler/SDK/
DYLD hooks, no module resolution/network, local host Go target, and
`CC=/usr/bin/clang`, `CXX=/usr/bin/clang++` with
`PATH=/usr/bin:/bin:/usr/sbin:/sbin`. Compilation reads a private source copy;
no checkout artifacts or dependency changes are needed. The outer focused
launcher retains its existing cancellation ownership; this adds no process
groups or launcher infrastructure.

## Sources and bounded implementation decision

Public installed SDK consulted (MacOSX26.5.sdk under CommandLineTools):

- `usr/include/sys/attr.h`: `FSOPT_REPORT_FULLSIZE`, `ATTR_BIT_MAP_COUNT`,
  `struct attrlist`, `attrreference_t`, `ATTR_CMN_EXTENDED_SECURITY`.
- `usr/include/unistd.h`: public `fgetattrlist` declaration.
- `usr/share/man/man2/getattrlist.2`: attribute-buffer format and relative
  references; FULLSIZE/truncation; extended security; ERANGE for fewer than
  four bytes, EINVAL for invalid bitmapcount/unsupported requested attributes.

The supplied source investigation identifies
[XNU f6217f891ac0bb64f3d375211650a4c1ff8ca1ea, 12377.1.9](https://github.com/apple-oss-distributions/xnu/blob/f6217f891ac0bb64f3d375211650a4c1ff8ca1ea/bsd/vfs/vfs_attrlist.c):
`fgetattrlist` → `getattrlist_internal` → `getattrlistbulk_internal` (`is_bulk=0`)
→ `vfs_attr_pack_internal`. With this required-attribute request, unsupported
active `va_acl` fails EINVAL before packing (no ACL altdata fallback), while
supported NULL `va_acl` packs the 12/8/0 result. This source finding
is supplied context, not a new source audit by this implementation.

The reported runtime XNU 12377.91.3 has no established exact public-source
mapping (the requested exact public tag returned 404 in the supplied
investigation). No exact kernel-source identity is claimed.

Supplied private APFS probe proc9579 PASS covered eight fixtures (directory and
regular file, each naturally absent / allow / deny / inherited) and four
negatives. Natural absence correlated with NULL/ENOENT and complete 12/8/0;
allocated allow/deny/inherited ACLs returned complete 80/8/68. Invalid fd gave
EBADF; capacity 3 gave ERANGE; invalid bitmapcount gave EINVAL; capacity 4
succeeded with reported length 12 but incomplete fixed fields. Durable logs:
`/Users/milo/Worktrees/dotty/agent-validation/coord-authority-1adkot34/darwin-required-acl-probe.stdout.log`
and sibling `darwin-required-acl-probe.stderr.log`. These are supplied prior
observations, not checks run by this implementation worker.

Oracle `86ffd4f1` approved the bounded 31a change from the public required-
attribute contract, pinned source, and this native observation together. The
production-unused reader now confirms only NULL/ENOENT independently on the
same held fd, accepting only the complete fixed 12/8/0 shape. Allocated ACLs
retain validation/flags/entries/free and never invoke confirmation. This does
not prove unsupported-ACL behavior on every filesystem or eliminate ACL races;
identity/topology/reobservations and the narrow APFS policy remain required.
The maintained probe remains a diagnostic, not sole acceptance evidence.

## Deferred parent launch

After independent probe/launcher review, from this worktree in the parent's
approved private focused-launch environment (host Go on PATH, Darwin and cgo):

```sh
CGO_ENABLED=1 CC=/usr/bin/clang CXX=/usr/bin/clang++ GOCACHEPROG= GOFLAGS= GOTOOLCHAIN=local mise run test:focused ./internal/nativefs '^TestDarwinRequiredACLProbe$'
```

No build, probe, test, formatter, or check was executed during this adjustment.
Stop for independent review, formatting, and fresh native 31a gates; do not use
this diagnostic alone as authority-suite acceptance. Production integration and
Darwin cgo-disabled release support remain blocked.
