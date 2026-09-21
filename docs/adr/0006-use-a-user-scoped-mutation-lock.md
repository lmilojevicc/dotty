# Use a canonical user-scoped mutation lock

Dotty will serialize cooperating mutations for one effective Unix user before acquiring repository-specific locks. Lock identity is independent of `HOME`, `XDG_CONFIG_HOME`, repository selection, flags, and Dotty config: it is the decimal effective UID beneath a fixed physical system temporary root.

Supported locations are `/tmp/dotty-<euid>/mutation.lock` on Linux and `/private/tmp/dotty-<euid>/mutation.lock` on Darwin. Dotty first verifies the fixed temporary root is the expected root-owned sticky directory, then creates or opens the per-user directory no-follow, verifies effective-user ownership and mode `0700`, and creates or opens a regular lock file no-follow with mode `0600`. It takes an advisory lock on the opened file descriptor and verifies owner, mode, device/inode, and pathname identity before and after acquisition. A symlink, alias, insecure parent, unexpected owner/mode/type, inode replacement, or unavailable locking primitive fails closed. Other platforms remain unsupported until they define an equivalent stable OS-user anchor.

After the user lock, operations acquire affected repository locks in normalized physical-path order. This ordering covers repository aliases, alternate repository-resolution mechanisms, concurrent user-config writes, and different repositories that share a Target Path without deadlock.

The lock coordinates cooperating Dotty processes only. Same-user external writers, other uncooperative processes, and open descriptors remain outside the guarantee and are handled by anchored identity checks and fail-closed recovery.

## Considered Options

- Stable effective-UID lock plus ordered repository locks
- Environment-derived user lock
- Repository locks only
- Per-Target-Path locks
- No inter-process coordination
