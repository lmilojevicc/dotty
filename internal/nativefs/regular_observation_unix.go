//go:build linux || darwin

package nativefs

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

const regularReadFlags = unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_NOCTTY | unix.O_CLOEXEC

// Per-call private negative-test seams. No mutable global hooks, caller-supplied
// descriptors, security-root overrides, or selectable access flags exist.
type regularCalls struct {
	stat   func(int, *unix.Stat_t) error
	statat func(int, string, *unix.Stat_t, int) error
	openat func(int, string, int, uint32) (int, error)
	read   func(int, []byte) (int, error)
	close  func(int) error
}

func systemRegularCalls() regularCalls {
	return regularCalls{
		stat:   unix.Fstat,
		statat: unix.Fstatat,
		openat: unix.Openat,
		read:   unix.Read,
		close:  unix.Close,
	}
}

// Stat_t's mode/link widths differ across Darwin/Linux architectures; both are
// unsigned and widen losslessly. Timestamp members are signed 32- or 64-bit.
// Keep malformed values as diagnostic evidence, not as a usable baseline.
func regularStatMetadata(st *unix.Stat_t) (regularMetadata, error) {
	mode, nlink := normalizeStatModeAndLinks(st.Mode, st.Nlink)
	mtimeSec, mtimeNsec := st.Mtim.Unix()
	ctimeSec, ctimeNsec := st.Ctim.Unix()
	m := regularMetadata{
		identity: statIdentity(st), uid: st.Uid, gid: st.Gid,
		mode: mode, nlink: nlink, size: st.Size,
		mtimeSec: mtimeSec, mtimeNsec: mtimeNsec,
		ctimeSec: ctimeSec, ctimeNsec: ctimeNsec,
	}
	_, err := normalizeRegularMetadata(
		m.identity,
		m.uid,
		m.gid,
		m.mode,
		m.nlink,
		m.size,
		m.mtimeSec,
		m.mtimeNsec,
		m.ctimeSec,
		m.ctimeNsec,
	)
	return m, err
}

// ObserveRegular reads a fresh no-follow description using fixed buffer memory
// and an initial-size byte budget plus one EOF probe. It intentionally writes
// nothing, but reads may update atime, including on failure or cancellation.
// Context/Close checks occur between synchronous calls, not within a blocked
// syscall. Arbitrary readable ownership/modes/hardlinks are data, not authority.
// Mixed reads, ABA, and changes after final checks remain observational limits.
func (d *Dir) ObserveRegular(ctx context.Context, name Component) (RegularObservation, error) {
	return d.observeRegular(ctx, name, systemRegularCalls())
}

type regularOperation struct {
	ctx     context.Context
	state   *dirState
	closing <-chan struct{}
	path    string
	name    Component
	calls   regularCalls
}

func regularContext(ctx context.Context, path string, name Component) error {
	if ctx == nil {
		return regularError(
			path,
			name,
			ReasonInvalidOperation,
			"nil context",
			regularMetadata{},
			regularMetadata{},
			nil,
		)
	}
	if err := ctx.Err(); err != nil {
		return regularError(
			path,
			name,
			ReasonIO,
			"observation cancelled",
			regularMetadata{},
			regularMetadata{},
			err,
		)
	}
	return nil
}

func (o *regularOperation) checkpoint() error {
	if err := regularContext(o.ctx, o.path, o.name); err != nil {
		return err
	}
	select {
	case <-o.closing:
		return regularError(
			o.path,
			o.name,
			ReasonClosed,
			"parent is closing",
			regularMetadata{},
			regularMetadata{},
			nil,
		)
	default:
		return nil
	}
}

func regularNativeError(path string, name Component, err error) *RegularObservationError {
	e := nativeError("observe-regular", path, name, err)
	return regularError(
		path,
		name,
		e.Reason,
		"native observation failed",
		regularMetadata{},
		regularMetadata{},
		e,
	)
}

// guard covers every held descriptor and physical edge, including /. Unlike a
// single call to the earlier identity guard, it checkpoints between each syscall.
// It adds no ownership, mount, ACL, or authority-role policy to general payloads.
func (o *regularOperation) guard() error {
	for i, node := range o.state.nodes {
		if err := o.checkpoint(); err != nil {
			return err
		}
		var st unix.Stat_t
		if err := o.calls.stat(node.fd, &st); err != nil {
			e := regularNativeError(node.path, node.name, err)
			e.Expected.metadata.identity = node.identity
			return e
		}
		opened := statIdentity(&st)
		if opened != node.identity {
			return regularError(
				node.path,
				node.name,
				ReasonIdentityChanged,
				"pinned ancestry descriptor changed",
				regularMetadata{identity: node.identity},
				regularMetadata{identity: opened},
				nil,
			)
		}
		if err := o.checkpoint(); err != nil {
			return err
		}
		fd, name := node.fd, "/"
		if i > 0 {
			fd, name = o.state.nodes[i-1].fd, node.name.name
		}
		if err := o.calls.statat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			e := regularNativeError(node.path, node.name, err)
			e.Expected.metadata.identity = node.identity
			return e
		}
		observed := statIdentity(&st)
		if observed != node.identity {
			return regularError(
				node.path,
				node.name,
				ReasonIdentityChanged,
				"physical ancestry edge changed",
				regularMetadata{identity: node.identity},
				regularMetadata{identity: observed},
				nil,
			)
		}
	}
	return o.checkpoint()
}

func (o *regularOperation) metadata(
	fd int,
	byName bool,
	baseline regularMetadata,
) (regularMetadata, error) {
	if err := o.checkpoint(); err != nil {
		return regularMetadata{}, err
	}
	var st unix.Stat_t
	var err error
	if byName {
		err = o.calls.statat(fd, o.name.name, &st, unix.AT_SYMLINK_NOFOLLOW)
	} else {
		err = o.calls.stat(fd, &st)
	}
	if err != nil {
		e := regularNativeError(o.path, o.name, err)
		e.Expected.metadata = baseline
		return regularMetadata{}, e
	}
	m, err := regularStatMetadata(&st)
	// Identity replacement takes precedence over same-inode malformed metadata.
	// Wrong-kind baselines and swaps never reach a payload read.
	if baseline.identity.Valid && m.identity != baseline.identity {
		return regularMetadata{}, compareRegularMetadata(o.path, o.name, baseline, m)
	}
	if err != nil {
		return regularMetadata{}, regularError(
			o.path,
			o.name,
			ReasonInvalidMetadata,
			"regular type and representable metadata required",
			baseline,
			m,
			err,
		)
	}
	if err := o.checkpoint(); err != nil {
		return regularMetadata{}, err
	}
	return m, nil
}

func (o *regularOperation) bind(fd int, baseline regularMetadata) error {
	opened, err := o.metadata(fd, false, baseline)
	if err != nil {
		return err
	}
	if err := compareRegularMetadata(o.path, o.name, baseline, opened); err != nil {
		return err
	}
	named, err := o.metadata(o.state.leaf().fd, true, baseline)
	if err != nil {
		return err
	}
	return compareRegularMetadata(o.path, o.name, baseline, named)
}

// read retries only a no-progress EINTR. Contradictory native count/error pairs
// refuse with the native cause intact rather than discard or hash dubious bytes.
func (o *regularOperation) read(fd int, buf []byte) (int, error) {
	for {
		if err := o.checkpoint(); err != nil {
			return 0, err
		}
		n, err := o.calls.read(fd, buf)
		if n > len(buf) || n < -1 || n < 0 && err == nil || n > 0 && err != nil {
			return 0, regularError(
				o.path,
				o.name,
				ReasonIO,
				fmt.Sprintf("invalid native read count=%d buffer=%d", n, len(buf)),
				regularMetadata{},
				regularMetadata{},
				err,
			)
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return 0, regularNativeError(o.path, o.name, err)
		}
		if err := o.checkpoint(); err != nil {
			return 0, err
		}
		return n, nil
	}
}

func (o *regularOperation) digest(fd int, baseline regularMetadata) ([32]byte, error) {
	hash := sha256.New()
	var buffer [32768]byte
	remaining := baseline.size
	for remaining > 0 {
		limit := int64(len(buffer))
		if remaining < limit {
			limit = remaining
		}
		n, err := o.read(fd, buffer[:int(limit)])
		if err != nil {
			return [32]byte{}, err
		}
		if n == 0 {
			return [32]byte{}, regularError(
				o.path,
				o.name,
				ReasonMetadataChanged,
				fmt.Sprintf(
					"premature EOF after %d of %d baseline bytes",
					baseline.size-remaining,
					baseline.size,
				),
				baseline,
				regularMetadata{},
				nil,
			)
		}
		_, _ = hash.Write(buffer[:n]) // SHA-256 Write never fails.
		remaining -= int64(n)
	}
	// Do not calculate size+1: even MaxInt64 has a safe one-byte EOF probe.
	n, err := o.read(fd, buffer[:1])
	if err != nil {
		return [32]byte{}, err
	}
	if n != 0 {
		return [32]byte{}, regularError(
			o.path,
			o.name,
			ReasonMetadataChanged,
			fmt.Sprintf("extra data after %d baseline bytes", baseline.size),
			baseline,
			regularMetadata{},
			nil,
		)
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

func (d *Dir) observeRegular(
	ctx context.Context,
	name Component,
	calls regularCalls,
) (result RegularObservation, resultErr error) {
	path := ""
	if d != nil && d.state != nil {
		path = entryPath(d.state.leaf().path, name)
	}
	if !name.valid() {
		return RegularObservation{}, regularError(
			path,
			name,
			ReasonInvalidComponent,
			"one valid component required",
			regularMetadata{},
			regularMetadata{},
			nil,
		)
	}
	if err := regularContext(ctx, path, name); err != nil {
		return RegularObservation{}, err
	}
	release, err := acquire(d)
	if err != nil {
		return RegularObservation{}, regularError(
			path,
			name,
			ReasonClosed,
			"parent unavailable",
			regularMetadata{},
			regularMetadata{},
			err,
		)
	}
	defer release() // Last defer to run: owned cleanup and publication keep this lease.
	o := regularOperation{
		ctx:     ctx,
		state:   d.state,
		closing: closingSignal(d.state),
		path:    path,
		name:    name,
		calls:   calls,
	}
	if err := o.guard(); err != nil {
		return RegularObservation{}, err
	}
	baseline, err := o.metadata(d.state.leaf().fd, true, regularMetadata{})
	if err != nil {
		return RegularObservation{}, err
	}
	if err := o.checkpoint(); err != nil {
		return RegularObservation{}, err
	}
	fd, err := calls.openat(d.state.leaf().fd, name.name, regularReadFlags, 0)
	if err != nil {
		return RegularObservation{}, regularNativeError(path, name, err)
	}
	if fd < 0 {
		return RegularObservation{}, regularError(
			path,
			name,
			ReasonIO,
			fmt.Sprintf("invalid native open descriptor=%d", fd),
			baseline,
			regularMetadata{},
			nil,
		)
	}
	owned := true
	closeOwned := func() error {
		if !owned {
			return nil
		}
		owned = false // Never retry, even when the OS reports a close failure.
		if err := calls.close(fd); err != nil {
			e := regularNativeError(path, name, err)
			e.Detail = "owned regular-file descriptor close failed; close will not be retried"
			e.Expected.metadata = baseline
			return e
		}
		return nil
	}
	defer func() {
		resultErr = errors.Join(resultErr, closeOwned())
		if resultErr != nil {
			result = RegularObservation{}
		}
	}()
	if err := o.guard(); err != nil {
		return RegularObservation{}, err
	}
	if err := o.bind(fd, baseline); err != nil {
		return RegularObservation{}, err
	}
	digest, err := o.digest(fd, baseline)
	if err != nil {
		return RegularObservation{}, err
	}
	if err := o.bind(fd, baseline); err != nil {
		return RegularObservation{}, err
	}
	if err := o.guard(); err != nil {
		return RegularObservation{}, err
	}
	if err := closeOwned(); err != nil {
		return RegularObservation{}, err
	}

	// Close has completed outside the lifecycle mutex while the parent is leased.
	// Only the final context/closing check and value publication serialize here.
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	if err := o.checkpoint(); err != nil {
		return RegularObservation{}, err
	}
	result = RegularObservation{metadata: baseline, digest: digest, valid: true}
	return result, nil
}
