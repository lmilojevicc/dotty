//go:build linux || (darwin && cgo)

package nativefs

import (
	"errors"

	"golang.org/x/sys/unix"
)

const lockFileFlags = unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_CLOEXEC | unix.O_NONBLOCK | unix.O_NOCTTY

// Per-operation seams only. Production always observes full security ancestry
// using native readers; private security-root adapters exist only in _test.go.
type privateCalls struct {
	acquisition acquisitionCalls
	authority   authorityCalls
	observe     func([]pinnedDir, AuthorityRole, authorityCalls) ([]AuthorityFacts, error)
	mkdir       func(int, string, uint32) error
	chmod       func(int, uint32) error
}

func systemPrivateCalls() privateCalls {
	return privateCalls{
		acquisition: systemAcquisition(), authority: systemAuthorityCalls(),
		observe: observeAuthorityNodes, mkdir: unix.Mkdirat, chmod: unix.Fchmod,
	}
}

// OpenPrivateDir opens only an existing exact-0700 private directory. The
// parent's full ancestry uses system-ancestor policy; existing objects are never
// repaired. The returned Dir independently owns its complete pinned ancestry.
func (d *Dir) OpenPrivateDir(name Component) (*Dir, error) {
	child, _, err := d.privateDir(name, false, systemPrivateCalls())
	return child, err
}

// CreatePrivateDir exclusively requests 0700. EEXIST refuses, never adopts.
// Success means mkdir succeeded and the currently bound directory is role-valid,
// not that the opened inode is proven to be mkdir's inode. Restrictive-umask
// directories are retained without chmod, deletion, or a process umask change.
func (d *Dir) CreatePrivateDir(name Component) (*Dir, CreationRecord, error) {
	return d.privateDir(name, true, systemPrivateCalls())
}

// OpenLockFile opens only an existing regular exact-0600 single-link file under
// an exact-0700 private parent. Fixed read-only/nonblocking/no-ctty flags narrow
// FIFO/device races but do not eliminate every device-open side effect.
func (d *Dir) OpenLockFile(name Component) (*File, error) {
	file, _, err := d.lockFile(name, false, systemPrivateCalls())
	return file, err
}

// CreateLockFile uses exclusive creation; only its newly returned, security-bound
// FD may establish 0600 from umask-reduced bits. Existing/race-winner objects are
// never chmodded, truncated or removed. No flock capability is implied here.
func (d *Dir) CreateLockFile(name Component) (*File, CreationRecord, error) {
	return d.lockFile(name, true, systemPrivateCalls())
}

func recordNodes(record *CreationRecord, nodes []pinnedDir, phase string, facts []AuthorityFacts) {
	if !record.created {
		return
	}
	for i, node := range nodes {
		observation := CreationObservation{path: node.path, phase: phase, identity: node.identity}
		if i < len(facts) {
			observation.facts = facts[i]
		}
		record.observations = append(record.observations, observation)
	}
}

func recordFailure(record *CreationRecord, err error) {
	if err == nil {
		return
	}
	switch e := err.(type) { //nolint:errorlint // Retain each exact diagnostic node, not its first matching descendant.
	case *AuthorityError:
		record.observations = append(
			record.observations,
			CreationObservation{
				path:     e.Path,
				phase:    "failure-expected",
				identity: e.Expected.identity,
				facts:    e.Expected,
			},
			CreationObservation{
				path:     e.Path,
				phase:    "failure-observed",
				identity: e.Observed.identity,
				facts:    e.Observed,
			},
		)
	case *Error:
		record.observations = append(record.observations,
			CreationObservation{path: e.Path, phase: "failure-expected", identity: e.Expected},
			CreationObservation{path: e.Path, phase: "failure-observed", identity: e.Observed})
	}
	switch wrapped := err.(type) { //nolint:errorlint // Walk only this node's edges; all joined cleanup causes remain available.
	case interface{ Unwrap() []error }:
		for _, cause := range wrapped.Unwrap() {
			recordFailure(record, cause)
		}
	case interface{ Unwrap() error }:
		recordFailure(record, wrapped.Unwrap())
	}
}

func finishCreation(record *CreationRecord, err *error) {
	if !record.created || *err == nil {
		return
	}
	recordFailure(record, *err)
	*err = &CreationError{record: *record, err: *err}
}

func privateParent(s *dirState, role AuthorityRole, calls privateCalls) ([]AuthorityFacts, error) {
	if err := s.guard("private-parent"); err != nil {
		return nil, err
	}
	before, err := calls.observe(s.nodes, role, calls.authority)
	if err != nil {
		return nil, err
	}
	if err := s.guard("private-parent"); err != nil {
		return nil, err
	}
	after, err := calls.observe(s.nodes, role, calls.authority)
	if err != nil {
		return nil, err
	}
	if err := compareAuthorityNodes(s.nodes, role, before, after); err != nil {
		return nil, err
	}
	if err := s.guard("private-parent"); err != nil {
		return nil, err
	}
	return after, nil
}

// This successful-mkdir transition disregards direct-parent directory nlink.
// Compare actual values without rewriting either observation. This cannot assign
// causality to the whole delta, prove child inventory, or grant deletion rights.
// Every other fact and every higher ancestor's nlink must remain identical.
func compareMkdirTransition(nodes []pinnedDir, before, after []AuthorityFacts) error {
	last := len(nodes) - 1
	if err := compareAuthorityNodes(
		nodes[:last],
		RoleSystemAncestor,
		before[:last],
		after[:last],
	); err != nil {
		return err
	}
	b, a := before[last], after[last]
	if b.identity.Kind == unix.S_IFDIR && a.identity.Kind == unix.S_IFDIR &&
		b.identity == a.identity && b.uid == a.uid && b.gid == a.gid && b.mode == a.mode &&
		b.filesystem == a.filesystem && b.security == a.security {
		return nil
	}
	return compareAuthorityNodes(nodes[last:], RoleSystemAncestor, before[last:], after[last:])
}

// Only successful exclusive file creation on the verified APFS model permits
// direct-parent directory nlink inequality. Native APFS observations can differ
// across regular-file creation too; the window is not proof of the delta's cause.
// Keep actual observations and require every other fact/higher ancestor to match.
// This comparison is not an option for opens, failed creates, or later phases.
func compareFileCreateTransition(nodes []pinnedDir, before, after []AuthorityFacts) error {
	last := len(nodes) - 1
	b, a := before[last], after[last]
	if b.filesystem.state == EvidencePresent &&
		b.filesystem.model == "darwin-local-apfs-ownership-enforced" &&
		b.identity.Kind == unix.S_IFDIR && a.identity.Kind == unix.S_IFDIR &&
		b.identity == a.identity && b.uid == a.uid && b.gid == a.gid && b.mode == a.mode &&
		b.filesystem == a.filesystem && b.security == a.security {
		return compareAuthorityNodes(nodes[:last], RoleSystemAncestor, before[:last], after[:last])
	}
	return compareAuthorityNodes(nodes, RolePrivateAnchor, before, after)
}

func (d *Dir) privateDir(
	name Component,
	create bool,
	calls privateCalls,
) (result *Dir, record CreationRecord, resultErr error) {
	defer finishCreation(&record, &resultErr)
	if !name.valid() {
		return nil, record, &Error{
			Op:        "private-directory",
			Component: name.name,
			Reason:    ReasonInvalidComponent,
		}
	}
	release, err := acquire(d)
	if err != nil {
		return nil, record, err
	}
	defer release()
	s := d.state
	parent := s.leaf()
	path := entryPath(parent.path, name)
	baseline, err := privateParent(s, RoleSystemAncestor, calls)
	if err != nil {
		return nil, record, err
	}
	if create {
		if err := calls.mkdir(parent.fd, name.name, 0o700); err != nil {
			return nil, record, nativeError("create-private-directory", path, name, err)
		}
		record = CreationRecord{path: path, kind: unix.S_IFDIR, created: true}
		recordNodes(&record, s.nodes, "before-mkdir", baseline)
		if err := s.guard("after-mkdir"); err != nil {
			return nil, record, err
		}
		after, err := calls.observe(s.nodes, RoleSystemAncestor, calls.authority)
		recordNodes(&record, s.nodes, "after-mkdir", after)
		if err != nil {
			return nil, record, err
		}
		if err := compareMkdirTransition(s.nodes, baseline, after); err != nil {
			return nil, record, err
		}
		if err := s.guard("after-mkdir"); err != nil {
			return nil, record, err
		}
		baseline = after
	}
	first, err := statEntry(parent.fd, name)
	if err != nil {
		return nil, record, nativeError("observe-private-directory", path, name, err)
	}
	if record.created {
		record.observations = append(
			record.observations,
			CreationObservation{path: path, phase: "first-child", identity: first},
		)
	}
	if first.Kind != unix.S_IFDIR {
		return nil, record, &Error{
			Op:        "private-directory",
			Path:      path,
			Component: name.name,
			Reason:    ReasonTopology,
			Observed:  first,
		}
	}
	child, err := openPhysicalDir(path, calls.acquisition)
	if err != nil {
		return nil, record, err
	}
	defer func() {
		if result == nil {
			resultErr = errors.Join(resultErr, child.Close())
		}
	}()
	// Repinning owns new FDs, not copied parent integers. It must agree with
	// BOTH the leased parent's full chain and the first observed child identity.
	for i, node := range child.state.nodes {
		expected := first
		if i < len(s.nodes) {
			expected = s.nodes[i].identity
		}
		if node.identity != expected {
			return nil, record, mismatch(
				"repin-private-directory",
				node.path,
				node.name,
				expected,
				node.identity,
			)
		}
	}
	var previous []AuthorityFacts
	for observation := 0; observation < 2; observation++ {
		if err := s.guard("private-directory"); err != nil {
			return nil, record, err
		}
		if err := child.state.guard("private-directory"); err != nil {
			return nil, record, err
		}
		facts, err := calls.observe(child.state.nodes, RolePrivateAnchor, calls.authority)
		recordNodes(&record, child.state.nodes, "bound-directory", facts)
		if err != nil {
			return nil, record, err
		}
		if err := compareAuthorityNodes(
			s.nodes,
			RoleSystemAncestor,
			baseline,
			facts[:len(s.nodes)],
		); err != nil {
			return nil, record, err
		}
		if previous != nil {
			if err := compareAuthorityNodes(
				child.state.nodes,
				RolePrivateAnchor,
				previous,
				facts,
			); err != nil {
				return nil, record, err
			}
		}
		previous = facts
	}
	if err := s.guard("private-directory"); err != nil {
		return nil, record, err
	}
	if err := child.state.guard("private-directory"); err != nil {
		return nil, record, err
	}
	return child, record, nil
}

func boundFile(
	s *dirState,
	node pinnedDir,
	baseline []AuthorityFacts,
	calls privateCalls,
	record *CreationRecord,
) (AuthorityFacts, error) {
	if err := s.guard("bind-lock-file"); err != nil {
		return AuthorityFacts{}, err
	}
	opened, err := statFD(node.fd)
	if err != nil {
		return AuthorityFacts{}, nativeError("bind-lock-file", node.path, node.name, err)
	}
	if opened != node.identity {
		return AuthorityFacts{}, mismatch(
			"bind-lock-file",
			node.path,
			node.name,
			node.identity,
			opened,
		)
	}
	bound, err := statEntry(s.leaf().fd, node.name)
	if err != nil {
		return AuthorityFacts{}, nativeError("bind-lock-file", node.path, node.name, err)
	}
	if bound != opened {
		return AuthorityFacts{}, mismatch("bind-lock-file", node.path, node.name, opened, bound)
	}
	parentFacts, err := calls.observe(s.nodes, RolePrivateAnchor, calls.authority)
	recordNodes(record, s.nodes, "bound-file-parent", parentFacts)
	if err != nil {
		return AuthorityFacts{}, err
	}
	if err := compareAuthorityNodes(s.nodes, RolePrivateAnchor, baseline, parentFacts); err != nil {
		return AuthorityFacts{}, err
	}
	facts, err := readAuthorityFacts(node.fd, calls.authority)
	recordNodes(record, []pinnedDir{node}, "bound-file", []AuthorityFacts{facts})
	if err != nil {
		var authority *AuthorityError
		if errors.As(err, &authority) {
			copy := *authority
			copy.Path, copy.Role = node.path, RoleLockFile
			return facts, &copy
		}
		return facts, &AuthorityError{
			Op:          "bind-lock-file",
			Path:        node.path,
			Role:        RoleLockFile,
			Reason:      nativeError("bind-lock-file", node.path, node.name, err).Reason,
			Observed:    facts,
			Err:         err,
			Detail:      "descriptor authority could not be read",
			Remediation: "Inspect this exact path and the native security error before retrying.",
		}
	}
	if facts.identity != node.identity {
		return facts, mismatch(
			"bind-lock-file",
			node.path,
			node.name,
			node.identity,
			facts.identity,
		)
	}
	if err := s.guard("bind-lock-file"); err != nil {
		return facts, err
	}
	bound, err = statEntry(s.leaf().fd, node.name)
	if err != nil {
		return facts, nativeError("bind-lock-file", node.path, node.name, err)
	}
	if bound != node.identity {
		return facts, mismatch("bind-lock-file", node.path, node.name, node.identity, bound)
	}
	return facts, nil
}

func validateCreatedFile(path string, facts AuthorityFacts) error {
	// The only permitted pre-chmod deviation is missing requested 0600 bits.
	// Validate the remaining real native facts, retaining original observations
	// in any diagnostic instead of claiming the provisional mode was observed.
	provisional := facts
	if facts.mode & ^uint32(0o600) == 0 {
		provisional.mode = 0o600
	}
	if err := validateAuthority(
		path,
		provisional,
		RoleLockFile,
		uint32(unix.Geteuid()),
	); err != nil {
		var authority *AuthorityError
		if errors.As(err, &authority) {
			copy := *authority
			copy.Observed = facts
			copy.Detail = "exclusive-created file requires supported security, effective UID, regular type, one link and only umask-reduced 0600 bits before chmod"
			return &copy
		}
		return err
	}
	return nil
}

func compareFileModeTransition(node pinnedDir, before, after AuthorityFacts) error {
	if after.mode == 0o600 && before.identity == after.identity && before.uid == after.uid &&
		before.gid == after.gid && before.nlink == after.nlink && before.filesystem == after.filesystem && before.security == after.security {
		return nil
	}
	return &AuthorityError{
		Op:          "establish-lock-mode",
		Path:        node.path,
		Role:        RoleLockFile,
		Reason:      ReasonAuthorityChanged,
		Expected:    before,
		Observed:    after,
		Detail:      "facts other than the exclusive-created file's mode changed, or exact 0600 was not established",
		Remediation: "Inspect this exact retained path and its ownership, links and security before retrying.",
	}
}

func (d *Dir) lockFile(
	name Component,
	create bool,
	calls privateCalls,
) (result *File, record CreationRecord, resultErr error) {
	defer finishCreation(&record, &resultErr)
	if !name.valid() {
		return nil, record, &Error{
			Op:        "lock-file",
			Component: name.name,
			Reason:    ReasonInvalidComponent,
		}
	}
	release, err := acquire(d)
	if err != nil {
		return nil, record, err
	}
	// Success transfers this very lease, never a reacquired lease, to File.
	defer func() {
		if result == nil {
			release()
		}
	}()
	s := d.state
	parent := s.leaf()
	path := entryPath(parent.path, name)
	baseline, err := privateParent(s, RolePrivateAnchor, calls)
	if err != nil {
		return nil, record, err
	}
	var first Identity
	if !create {
		first, err = statEntry(parent.fd, name)
		if err != nil {
			return nil, record, nativeError("observe-lock-file", path, name, err)
		}
		if first.Kind != unix.S_IFREG {
			return nil, record, &Error{
				Op:        "open-lock-file",
				Path:      path,
				Component: name.name,
				Reason:    ReasonTopology,
				Observed:  first,
			}
		}
	}
	flags, mode := lockFileFlags, uint32(0)
	if create {
		flags |= unix.O_CREAT | unix.O_EXCL
		mode = 0o600
	}
	fd, err := calls.acquisition.openat(parent.fd, name.name, flags, mode)
	if err != nil {
		return nil, record, nativeError("open-lock-file", path, name, err)
	}
	if create {
		record = CreationRecord{path: path, kind: unix.S_IFREG, created: true}
		recordNodes(&record, s.nodes, "before-file-create", baseline)
	}
	defer func() {
		if result == nil {
			if err := calls.acquisition.close(fd); err != nil {
				resultErr = errors.Join(
					resultErr,
					&Error{Op: "close-file", Path: path, Reason: ReasonIO, Err: err},
				)
			}
		}
	}()
	opened, err := statFD(fd)
	if err != nil {
		return nil, record, nativeError("observe-opened-lock-file", path, name, err)
	}
	if record.created {
		record.observations = append(
			record.observations,
			CreationObservation{path: path, phase: "opened-file", identity: opened},
		)
	}
	if !create && opened != first {
		return nil, record, mismatch("open-lock-file", path, name, first, opened)
	}
	node := pinnedDir{fd: fd, path: path, name: name, identity: opened}
	if create {
		if opened.Kind != unix.S_IFREG {
			return nil, record, &Error{
				Op:        "open-lock-file",
				Path:      path,
				Component: name.name,
				Reason:    ReasonTopology,
				Observed:  opened,
			}
		}
		if err := s.guard("bind-lock-file"); err != nil {
			return nil, record, err
		}
		bound, err := statEntry(parent.fd, name)
		if err != nil {
			return nil, record, nativeError("bind-lock-file", path, name, err)
		}
		if bound != opened {
			return nil, record, mismatch("bind-lock-file", path, name, opened, bound)
		}
		// privateParent retains strict equality BETWEEN its two postcreation
		// observations. Only the cross-creation comparison below is model-bound.
		after, err := privateParent(s, RolePrivateAnchor, calls)
		recordNodes(&record, s.nodes, "after-file-create", after)
		if err != nil {
			return nil, record, err
		}
		if err := compareFileCreateTransition(s.nodes, baseline, after); err != nil {
			return nil, record, err
		}
		if err := s.guard("bind-lock-file"); err != nil {
			return nil, record, err
		}
		bound, err = statEntry(parent.fd, name)
		if err != nil {
			return nil, record, nativeError("bind-lock-file", path, name, err)
		}
		if bound != opened {
			return nil, record, mismatch("bind-lock-file", path, name, opened, bound)
		}
		baseline = after
	}
	var previous AuthorityFacts
	for observation := 0; observation < 2; observation++ {
		facts, err := boundFile(s, node, baseline, calls, &record)
		if err != nil {
			return nil, record, err
		}
		if create {
			err = validateCreatedFile(path, facts)
		} else {
			err = validateAuthority(path, facts, RoleLockFile, uint32(unix.Geteuid()))
		}
		if err != nil {
			return nil, record, err
		}
		if observation != 0 {
			if err := compareAuthorityNodes(
				[]pinnedDir{node},
				RoleLockFile,
				[]AuthorityFacts{previous},
				[]AuthorityFacts{facts},
			); err != nil {
				return nil, record, err
			}
		}
		previous = facts
	}
	if create {
		if err := calls.chmod(fd, 0o600); err != nil {
			return nil, record, nativeError("establish-lock-mode", path, name, err)
		}
		for observation := 0; observation < 2; observation++ {
			facts, err := boundFile(s, node, baseline, calls, &record)
			if err != nil {
				return nil, record, err
			}
			if err := validateAuthority(
				path,
				facts,
				RoleLockFile,
				uint32(unix.Geteuid()),
			); err != nil {
				return nil, record, err
			}
			if observation == 0 {
				if err := compareFileModeTransition(node, previous, facts); err != nil {
					return nil, record, err
				}
			} else if err := compareAuthorityNodes([]pinnedDir{node}, RoleLockFile, []AuthorityFacts{previous}, []AuthorityFacts{facts}); err != nil {
				return nil, record, err
			}
			previous = facts
		}
	}
	return &File{
		state: &fileState{
			fd:            fd,
			path:          path,
			authority:     previous,
			closeFD:       calls.acquisition.close,
			releaseParent: release,
		},
	}, record, nil
}
