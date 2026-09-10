//go:build linux || (darwin && cgo)

package nativefs

import (
	"errors"

	"golang.org/x/sys/unix"
)

// Per-call, unexported observation seams; production always uses native readers.
// There is no descriptor adoption, callback, or security-root override in the API.
type authorityCalls struct {
	stat       func(int, *unix.Stat_t) error
	filesystem func(int) (FilesystemFacts, error)
	security   func(int, bool) (SecurityEvidence, error)
}

func systemAuthorityCalls() authorityCalls {
	return authorityCalls{stat: unix.Fstat, filesystem: nativeFilesystem, security: nativeSecurity}
}

// Authority observes and validates the complete pinned ancestry and the leaf's
// role under a lease. It changes nothing and conveys no durable authorization.
// Future create/open/flock operations must use these guards internally, not a
// caller's saved AuthorityFacts. Lock-file observations require the later file
// API; a Dir cannot satisfy RoleLockFile.
func (d *Dir) Authority(role AuthorityRole) (AuthorityFacts, error) {
	return d.authority(role, systemAuthorityCalls())
}

func (d *Dir) authority(role AuthorityRole, calls authorityCalls) (AuthorityFacts, error) {
	release, err := acquire(d)
	if err != nil {
		return AuthorityFacts{}, err
	}
	defer release()
	if err := d.state.guard("authority"); err != nil {
		return AuthorityFacts{}, err
	}
	before, err := observeAuthorityNodes(d.state.nodes, role, calls)
	if err != nil {
		return AuthorityFacts{}, err
	}
	if err := d.state.guard("authority"); err != nil {
		return AuthorityFacts{}, err
	}
	after, err := observeAuthorityNodes(d.state.nodes, role, calls)
	if err != nil {
		return AuthorityFacts{}, err
	}
	if err := compareAuthorityNodes(d.state.nodes, role, before, after); err != nil {
		return AuthorityFacts{}, err
	}
	if err := d.state.guard("authority"); err != nil {
		return AuthorityFacts{}, err
	}
	return after[len(after)-1], nil
}

func statAuthority(st *unix.Stat_t) AuthorityFacts {
	mode, nlink := normalizeStatModeAndLinks(st.Mode, st.Nlink)
	return AuthorityFacts{
		identity: statIdentity(st), uid: st.Uid, gid: st.Gid,
		mode: mode, nlink: nlink,
	}
}

// Stat_t uses 16-bit mode/link fields on Darwin, 32-bit modes on Linux,
// and 32- or 64-bit link counts depending on the Linux architecture.
func normalizeStatModeAndLinks[M ~uint16 | ~uint32, N ~uint16 | ~uint32 | ~uint64](
	mode M,
	nlink N,
) (uint32, uint64) {
	return uint32(mode) & 0o7777, uint64(nlink)
}

func readAuthorityFacts(fd int, calls authorityCalls) (AuthorityFacts, error) {
	var before, after unix.Stat_t
	if err := calls.stat(fd, &before); err != nil {
		return AuthorityFacts{}, err
	}
	facts := statAuthority(&before)
	var err error
	facts.filesystem, err = calls.filesystem(fd)
	if err != nil || facts.filesystem.state != EvidencePresent {
		return facts, err
	}
	facts.security, err = calls.security(fd, facts.identity.Kind == unix.S_IFDIR)
	if err != nil {
		reason := nativeError("authority", "", Component{}, err).Reason
		// A security API's ENOENT on this held descriptor is not evidence
		// that its pathname is missing. Keep the cause without that diagnosis.
		if errors.Is(err, unix.ENOENT) {
			reason = ReasonIO
		}
		return facts, &AuthorityError{
			Op:          "authority",
			Reason:      reason,
			Expected:    statAuthority(&before),
			Observed:    facts,
			Detail:      "descriptor security evidence could not be read; ACL absence is unproven",
			Err:         err,
			Remediation: "Inspect the affected path and native security error; use a supported filesystem/security backend before retrying.",
		}
	}
	if err := calls.stat(fd, &after); err != nil {
		return facts, err
	}
	if statAuthority(&before) != statAuthority(&after) {
		observed := statAuthority(&after)
		observed.filesystem, observed.security = facts.filesystem, facts.security
		return observed, &AuthorityError{
			Op:          "authority",
			Reason:      ReasonAuthorityChanged,
			Expected:    facts,
			Observed:    observed,
			Detail:      "descriptor identity, ownership, mode, or link count changed during security observation",
			Remediation: "Stop concurrent edits to the affected path and inspect its ownership and security before retrying.",
		}
	}
	return facts, nil
}

func observeAuthorityNodes(
	nodes []pinnedDir,
	role AuthorityRole,
	calls authorityCalls,
) ([]AuthorityFacts, error) {
	facts := make([]AuthorityFacts, 0, len(nodes))
	for i, node := range nodes {
		nodeRole := RoleSystemAncestor
		if i == len(nodes)-1 {
			nodeRole = role
		}
		observed, err := readAuthorityFacts(node.fd, calls)
		if err != nil {
			var drift *AuthorityError
			if errors.As(err, &drift) {
				copy := *drift
				copy.Path, copy.Role = node.path, nodeRole
				return nil, &copy
			}
			return nil, &AuthorityError{
				Op:          "authority",
				Path:        node.path,
				Role:        nodeRole,
				Reason:      nativeError("authority", node.path, node.name, err).Reason,
				Observed:    observed,
				Detail:      "descriptor authority evidence could not be read",
				Err:         err,
				Remediation: "Inspect the affected path and native capability error; use a supported filesystem/security backend before retrying.",
			}
		}
		if observed.identity != node.identity {
			return nil, mismatch(
				"authority",
				node.path,
				node.name,
				node.identity,
				observed.identity,
			)
		}
		if err := validateAuthority(
			node.path,
			observed,
			nodeRole,
			uint32(unix.Geteuid()),
		); err != nil {
			return nil, err
		}
		facts = append(facts, observed)
	}
	return facts, nil
}

func compareAuthorityNodes(
	nodes []pinnedDir,
	role AuthorityRole,
	before, after []AuthorityFacts,
) error {
	for i, node := range nodes {
		if before[i] != after[i] {
			nodeRole := RoleSystemAncestor
			if i == len(nodes)-1 {
				nodeRole = role
			}
			return &AuthorityError{
				Op:          "authority",
				Path:        node.path,
				Role:        nodeRole,
				Reason:      ReasonAuthorityChanged,
				Expected:    before[i],
				Observed:    after[i],
				Detail:      "authority changed between guarded observations",
				Remediation: "Stop concurrent edits to the affected path and inspect its ownership, ACLs, and mount before retrying.",
			}
		}
	}
	return nil
}

func validateAuthority(path string, f AuthorityFacts, role AuthorityRole, uid uint32) error {
	refuse := func(reason Reason, detail string) error {
		return &AuthorityError{
			Op:          "authority",
			Path:        path,
			Role:        role,
			Reason:      reason,
			Observed:    f,
			Detail:      detail,
			Remediation: "Inspect this exact path's owner, mode, ACLs, and mount; resolve the stated policy mismatch explicitly before retrying. Dotty does not repair authority objects.",
		}
	}
	if !f.identity.Valid {
		return refuse(ReasonInvalidIdentity, "descriptor identity is uninitialized")
	}
	if f.filesystem.state != EvidencePresent {
		return refuse(ReasonUnsupported, "filesystem/mount authority is not supported and verified")
	}
	if f.security.state != EvidenceAbsent {
		if f.security.state == EvidencePresent {
			return refuse(
				ReasonAuthorityPolicy,
				"an ACL is present; this boundary supports verified absence only",
			)
		}
		return refuse(ReasonUnsupported, "ACL absence is not verified")
	}
	if role == RoleLockFile {
		if f.identity.Kind != unix.S_IFREG || f.uid != uid || f.mode != 0o600 || f.nlink != 1 {
			return refuse(
				ReasonAuthorityPolicy,
				"lock file requires effective UID, regular type, exact 0600, no special bits, and one link",
			)
		}
		return nil
	}
	if f.identity.Kind != unix.S_IFDIR {
		return refuse(ReasonAuthorityPolicy, "directory role requires a directory descriptor")
	}
	switch role {
	case RolePrivateAnchor:
		if f.uid != uid || f.mode != 0o700 {
			return refuse(
				ReasonAuthorityPolicy,
				"private anchor requires effective UID and exact 0700 without special bits",
			)
		}
	case RoleRepositoryDirectory:
		if f.uid != uid || f.mode&0o7022 != 0 {
			return refuse(
				ReasonAuthorityPolicy,
				"repository directory requires effective UID and no group/other write or special bits",
			)
		}
	case RoleSystemAncestor:
		if path == systemTemporaryRoot {
			if f.uid != 0 || f.mode != 0o1777 {
				return refuse(
					ReasonAuthorityPolicy,
					"fixed system temporary root requires root ownership and exact sticky 01777",
				)
			}
			return nil
		}
		if (f.uid != 0 && f.uid != uid) || (path == "/" && f.uid != 0) || f.mode&0o7022 != 0 {
			return refuse(
				ReasonAuthorityPolicy,
				"ancestor requires root or effective UID ownership (root for /), with no group/other write or special bits",
			)
		}
	default:
		return refuse(ReasonInvalidOperation, "uninitialized or unknown authority role")
	}
	return nil
}
