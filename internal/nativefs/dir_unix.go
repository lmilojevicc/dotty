//go:build linux || darwin

package nativefs

import (
	"errors"
	"sync"

	"golang.org/x/sys/unix"
)

const directoryFlags = unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC

type pinnedDir struct {
	fd       int
	path     string
	name     Component
	identity Identity
}

type dirState struct {
	// Immutable after publication; accessible only while leased or by Close.
	nodes   []pinnedDir
	closeFD func(int) error

	// All lifecycle fields are guarded by lifecycleMu.
	active   int
	closing  bool
	closed   bool
	closeErr error
}

// One short critical section acquires both handles atomically. This provides a
// deterministic acquisition order without nested per-handle locks (including
// reversed operands and the same-handle case). No syscall runs under this lock.
var (
	lifecycleMu      sync.Mutex
	lifecycleChanged = sync.NewCond(&lifecycleMu)
)

func (s *dirState) leaf() pinnedDir { return s.nodes[len(s.nodes)-1] }

func acquire(dirs ...*Dir) (func(), error) {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	states := make([]*dirState, 0, len(dirs))
	for _, d := range dirs {
		if d == nil || d.state == nil || d.state.closing {
			return nil, &Error{Op: "acquire", Reason: ReasonClosed}
		}
		duplicate := false
		for _, state := range states {
			if state == d.state {
				duplicate = true
				break
			}
		}
		if !duplicate {
			states = append(states, d.state)
		}
	}
	for _, state := range states {
		state.active++
	}
	return func() {
		lifecycleMu.Lock()
		defer lifecycleMu.Unlock()
		for _, state := range states {
			state.active--
		}
		lifecycleChanged.Broadcast()
	}, nil
}

// Close prevents new leases, waits for active operations through their native
// syscall, then closes each owned descriptor exactly once. Concurrent callers
// and copied wrappers receive the same result. A close error never causes retry:
// the OS may already have released and reused that descriptor number.
func (d *Dir) Close() error {
	if d == nil || d.state == nil {
		return nil
	}
	s := d.state
	lifecycleMu.Lock()
	if s.closing {
		for !s.closed {
			lifecycleChanged.Wait()
		}
		err := s.closeErr
		lifecycleMu.Unlock()
		return err
	}
	s.closing = true
	for s.active != 0 {
		lifecycleChanged.Wait()
	}
	lifecycleMu.Unlock()

	err := closeNodes(s.nodes, s.closeFD)
	lifecycleMu.Lock()
	s.closeErr = err
	s.closed = true
	lifecycleChanged.Broadcast()
	lifecycleMu.Unlock()
	return err
}

func closeNodes(nodes []pinnedDir, closeFD func(int) error) error {
	var errs []error
	for i := len(nodes) - 1; i >= 0; i-- {
		if err := closeFD(nodes[i].fd); err != nil {
			errs = append(
				errs,
				&Error{Op: "close-directory", Path: nodes[i].path, Reason: ReasonIO, Err: err},
			)
		}
	}
	return errors.Join(errs...)
}

// acquisitionCalls is a per-call test seam, never a mutable global or exported
// descriptor-adoption API. Production always supplies the native functions.
type acquisitionCalls struct {
	openat func(int, string, int, uint32) (int, error)
	close  func(int) error
}

func systemAcquisition() acquisitionCalls {
	return acquisitionCalls{openat: unix.Openat, close: unix.Close}
}

// OpenPhysicalDir pins a physical absolute directory path from /. Every
// component must already be explicit: no cleaning, symlink following, logical
// alias resolution, or creation is performed. The caller must Close the result.
func OpenPhysicalDir(abs string) (*Dir, error) {
	return openPhysicalDir(abs, systemAcquisition())
}

func openPhysicalDir(abs string, calls acquisitionCalls) (result *Dir, resultErr error) {
	parts, err := physicalComponents(abs)
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open("/", directoryFlags, 0)
	if err != nil {
		return nil, nativeError("open-directory", "/", Component{}, err)
	}
	nodes := []pinnedDir{{fd: fd, path: "/"}}
	defer func() {
		if result == nil {
			resultErr = errors.Join(resultErr, closeNodes(nodes, calls.close))
		}
	}()
	rootID, err := statFD(fd)
	if err != nil {
		return nil, nativeError("open-directory", "/", Component{}, err)
	}
	nodes[0].identity = rootID
	for _, name := range parts {
		parent := nodes[len(nodes)-1]
		path := entryPath(parent.path, name)
		before, err := statEntry(parent.fd, name)
		if err != nil {
			return nil, nativeError("open-directory", path, name, err)
		}
		if before.Kind != unix.S_IFDIR {
			return nil, &Error{
				Op:        "open-directory",
				Path:      path,
				Component: name.name,
				Reason:    ReasonTopology,
				Observed:  before,
			}
		}
		childFD, err := calls.openat(parent.fd, name.name, directoryFlags, 0)
		if err != nil {
			return nil, nativeError("open-directory", path, name, err)
		}
		nodes = append(nodes, pinnedDir{fd: childFD, path: path, name: name, identity: before})
		opened, err := statFD(childFD)
		if err != nil {
			return nil, nativeError("open-directory", path, name, err)
		}
		if opened != before {
			return nil, mismatch("open-directory", path, name, before, opened)
		}
		after, err := statEntry(parent.fd, name)
		if err != nil {
			return nil, nativeError("open-directory", path, name, err)
		}
		if after != opened {
			return nil, mismatch("open-directory", path, name, opened, after)
		}
	}
	state := &dirState{nodes: nodes, closeFD: calls.close}
	if err := state.guard("open-directory"); err != nil {
		return nil, err
	}
	return &Dir{state: state}, nil
}

func statFD(fd int) (Identity, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return Identity{}, err
	}
	return statIdentity(&st), nil
}

func statEntry(fd int, name Component) (Identity, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(fd, name.name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return Identity{}, err
	}
	return statIdentity(&st), nil
}

// guard revalidates every opened identity and every pinned parent/name edge.
// It is an observation, not an atomic proof against external directory writers.
func (s *dirState) guard(op string) *Error {
	for i, node := range s.nodes {
		opened, err := statFD(node.fd)
		if err != nil {
			return nativeError(op, node.path, node.name, err)
		}
		if opened != node.identity {
			return mismatch(op, node.path, node.name, node.identity, opened)
		}
		var observed Identity
		if i == 0 {
			// Confirm the pinned root still agrees with the absolute root.
			var st unix.Stat_t
			err = unix.Fstatat(node.fd, "/", &st, unix.AT_SYMLINK_NOFOLLOW)
			if err == nil {
				observed = statIdentity(&st)
			}
		} else {
			observed, err = statEntry(s.nodes[i-1].fd, node.name)
		}
		if err != nil {
			e := nativeError(op, node.path, node.name, err)
			e.Expected = node.identity
			return e
		}
		if observed != node.identity {
			return mismatch(op, node.path, node.name, node.identity, observed)
		}
	}
	return nil
}

// Identity reobserves the directory and its pinned ancestry under a lease.
func (d *Dir) Identity() (Identity, error) {
	release, err := acquire(d)
	if err != nil {
		return Identity{}, err
	}
	defer release()
	if err := d.state.guard("identity"); err != nil {
		return Identity{}, err
	}
	return d.state.leaf().identity, nil
}

// Observe returns the identity of a leaf entry itself, including a symlink,
// without following its referent. Missing entries return a wrapped ENOENT.
func (d *Dir) Observe(name Component) (Identity, error) {
	if !name.valid() {
		return Identity{}, &Error{
			Op:        "observe",
			Component: name.name,
			Reason:    ReasonInvalidComponent,
		}
	}
	release, err := acquire(d)
	if err != nil {
		return Identity{}, err
	}
	defer release()
	if err := d.state.guard("observe"); err != nil {
		return Identity{}, err
	}
	parent := d.state.leaf()
	id, err := statEntry(parent.fd, name)
	if err != nil {
		return Identity{}, nativeError("observe", entryPath(parent.path, name), name, err)
	}
	return id, nil
}

func mismatch(op, path string, name Component, expected, observed Identity) *Error {
	return &Error{
		Op:        op,
		Path:      path,
		Component: name.name,
		Reason:    ReasonIdentityChanged,
		Expected:  expected,
		Observed:  observed,
	}
}

func nativeError(op, path string, name Component, err error) *Error {
	reason := ReasonIO
	switch {
	case errors.Is(err, unix.ENOSYS), errors.Is(err, unix.ENOTSUP), errors.Is(err, unix.EOPNOTSUPP):
		reason = ReasonUnsupported
	case errors.Is(err, unix.EINVAL):
		// EINVAL also describes invalid filesystem operations/topology. Even
		// with validated names and ancestry it does not prove a missing flag.
		reason = ReasonInvalidOperation
	case errors.Is(err, unix.EXDEV):
		reason = ReasonCrossDevice
	case errors.Is(err, unix.EEXIST), errors.Is(err, unix.ENOTEMPTY):
		reason = ReasonExists
	case errors.Is(err, unix.ENOENT):
		reason = ReasonMissing
	case errors.Is(err, unix.ENOTDIR), errors.Is(err, unix.ELOOP):
		reason = ReasonTopology
	}
	return &Error{Op: op, Path: path, Component: name.name, Reason: reason, Err: err}
}
