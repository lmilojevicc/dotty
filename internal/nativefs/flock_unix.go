//go:build linux || (darwin && cgo)

package nativefs

import (
	"context"
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

// Per-call negative-test seams only; public operations always use full native
// security ancestry. There are no selectable flags, callbacks or role overrides.
type flockCalls struct {
	private privateCalls
	flock   func(int, int) error
}

func systemFlockCalls() flockCalls {
	return flockCalls{private: systemPrivateCalls(), flock: unix.Flock}
}

// LockFile acquires an existing exact-0600 single-link regular file beneath an
// exact-0700 private parent. Each acquisition owns a fresh read-only File, never
// a duplicate/shared description. It creates, repairs and removes nothing.
func (d *Dir) LockFile(ctx context.Context, name Component) (*Lease, error) {
	return d.lockFileLease(ctx, name, systemFlockCalls())
}

// LockDirectory independently repins and locks only the selected repository
// directory. System ancestors are validated, never flocked. Release the lease
// before awaiting d.Close, which cancels waiters and waits for returned leases.
func (d *Dir) LockDirectory(ctx context.Context) (*Lease, error) {
	return d.lockDirectory(ctx, systemFlockCalls())
}

func flockContextError(ctx context.Context, path string) error {
	if ctx == nil {
		return &Error{Op: "flock", Path: path, Reason: ReasonInvalidOperation}
	}
	if err := ctx.Err(); err != nil {
		return &Error{Op: "flock", Path: path, Reason: ReasonIO, Err: err}
	}
	return nil
}

func flockStopped(ctx context.Context, closing <-chan struct{}, path string) error {
	if err := flockContextError(ctx, path); err != nil {
		return err
	}
	select {
	case <-closing:
		return &Error{Op: "flock", Path: path, Reason: ReasonClosed}
	default:
		return nil
	}
}

func waitFlock(ctx context.Context, closing <-chan struct{}, l *Lease, calls flockCalls) error {
	s := l.state
	for {
		if err := flockStopped(ctx, closing, s.path); err != nil {
			return err
		}
		err := calls.flock(s.fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			s.unlock = func(fd int) error { return calls.flock(fd, unix.LOCK_UN) }
			return nil
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return nativeError("flock", s.path, Component{}, err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
		case <-closing:
		case <-timer.C:
		}
		timer.Stop()
	}
}

// Publication is serialized with Close. All observations/syscalls and all waits
// occur outside lifecycleMu. The already-held original token pins a winner; a
// loser is cleaned by the operation's deferred Release, never by reacquisition.
func publishFlock(ctx context.Context, s *dirState, path string) error {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	if s.closing {
		return &Error{Op: "flock", Path: path, Reason: ReasonClosed}
	}
	return flockContextError(ctx, path)
}

func (d *Dir) lockFileLease(
	ctx context.Context,
	name Component,
	calls flockCalls,
) (result *Lease, resultErr error) {
	if !name.valid() {
		return nil, &Error{Op: "flock-file", Component: name.name, Reason: ReasonInvalidComponent}
	}
	if err := flockContextError(ctx, ""); err != nil {
		return nil, err
	}
	release, err := acquire(d)
	if err != nil {
		return nil, err
	}
	defer release() // Setup token only; the fresh File retains its own old token.
	s := d.state
	path := entryPath(s.leaf().path, name)
	closing := closingSignal(s)
	if err := flockStopped(ctx, closing, path); err != nil {
		return nil, err
	}
	baseline, err := privateParent(s, RolePrivateAnchor, calls.private)
	if err != nil {
		return nil, err
	}
	file, _, err := d.lockFile(name, false, calls.private)
	if err != nil {
		return nil, err
	}
	lease := &Lease{state: &leaseState{fd: file.state.fd, path: path, close: file.Close}}
	defer func() {
		if result == nil {
			resultErr = errors.Join(resultErr, lease.Release())
		}
	}()
	node := pinnedDir{
		fd:       file.state.fd,
		path:     path,
		name:     name,
		identity: file.state.authority.identity,
	}
	validate := func() error {
		for observation := 0; observation < 2; observation++ {
			facts, err := boundFile(s, node, baseline, calls.private, &CreationRecord{})
			if err != nil {
				return err
			}
			if err := validateAuthority(
				path,
				facts,
				RoleLockFile,
				uint32(unix.Geteuid()),
			); err != nil {
				return err
			}
			if err := compareAuthorityNodes(
				[]pinnedDir{node},
				RoleLockFile,
				[]AuthorityFacts{file.state.authority},
				[]AuthorityFacts{facts},
			); err != nil {
				return err
			}
		}
		return nil
	}
	if err := validate(); err != nil {
		return nil, err
	}
	if err := waitFlock(ctx, closing, lease, calls); err != nil {
		return nil, err
	}
	if err := validate(); err != nil {
		return nil, err
	}
	if err := publishFlock(ctx, s, path); err != nil {
		return nil, err
	}
	return lease, nil
}

func (d *Dir) lockDirectory(
	ctx context.Context,
	calls flockCalls,
) (result *Lease, resultErr error) {
	if err := flockContextError(ctx, ""); err != nil {
		return nil, err
	}
	release, err := acquire(d)
	if err != nil {
		return nil, err
	}
	// Transfer this original token to the owned lease, including failed-acquire
	// cleanup. Never reacquire a parent whose Close may already be waiting.
	defer func() {
		if release != nil {
			release()
		}
	}()
	s := d.state
	path := s.leaf().path
	if path == "/" || path == systemTemporaryRoot {
		return nil, &AuthorityError{
			Op:          "flock-directory",
			Path:        path,
			Role:        RoleRepositoryDirectory,
			Reason:      ReasonAuthorityPolicy,
			Observed:    AuthorityFacts{identity: s.leaf().identity},
			Detail:      "selected system root is validation-only and must never be flocked",
			Remediation: "Select a user-owned repository directory other than / or the fixed system temporary root.",
		}
	}
	closing := closingSignal(s)
	if err := flockStopped(ctx, closing, path); err != nil {
		return nil, err
	}
	baseline, err := privateParent(s, RoleRepositoryDirectory, calls.private)
	if err != nil {
		return nil, err
	}
	owned, err := openPhysicalDir(path, calls.private.acquisition)
	if err != nil {
		return nil, err
	}
	lease := &Lease{state: &leaseState{
		fd: owned.state.leaf().fd, path: path, close: owned.Close, releaseParent: release,
	}}
	release = nil
	defer func() {
		if result == nil {
			resultErr = errors.Join(resultErr, lease.Release())
		}
	}()
	for i, node := range owned.state.nodes {
		if node.identity != s.nodes[i].identity {
			return nil, mismatch(
				"repin-flock-directory",
				node.path,
				node.name,
				s.nodes[i].identity,
				node.identity,
			)
		}
	}
	validate := func() error {
		for observation := 0; observation < 2; observation++ {
			if err := s.guard("flock-directory"); err != nil {
				return err
			}
			if err := owned.state.guard("flock-directory"); err != nil {
				return err
			}
			facts, err := calls.private.observe(
				owned.state.nodes,
				RoleRepositoryDirectory,
				calls.private.authority,
			)
			if err != nil {
				return err
			}
			if err := compareAuthorityNodes(
				s.nodes,
				RoleRepositoryDirectory,
				baseline,
				facts,
			); err != nil {
				return err
			}
			if err := s.guard("flock-directory"); err != nil {
				return err
			}
			if err := owned.state.guard("flock-directory"); err != nil {
				return err
			}
		}
		return nil
	}
	if err := validate(); err != nil {
		return nil, err
	}
	if err := waitFlock(ctx, closing, lease, calls); err != nil {
		return nil, err
	}
	if err := validate(); err != nil {
		return nil, err
	}
	if err := publishFlock(ctx, s, path); err != nil {
		return nil, err
	}
	return lease, nil
}
