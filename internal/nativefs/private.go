package nativefs

import (
	"fmt"
	"slices"
	"sync"
)

// File owns an independent read-only descriptor and a retained parent lease.
// Copies share ownership. Close every File before waiting for its parent's
// Close: Dir.Close waits for dependent Files. No descriptor or lock is exposed.
type File struct {
	state *fileState
}

type fileState struct {
	fd            int
	path          string
	closeFD       func(int) error
	releaseParent func()
	once          sync.Once
	closeErr      error
}

// Close closes once across concurrent/copy wrappers and releases the original
// parent lease even on close error, without reacquiring a possibly closing Dir.
// A native close error is never retried against a potentially reused FD number.
func (f *File) Close() error {
	if f == nil || f.state == nil {
		return nil
	}
	s := f.state
	s.once.Do(func() {
		defer s.releaseParent()
		if err := s.closeFD(s.fd); err != nil {
			s.closeErr = &Error{Op: "close-file", Path: s.path, Reason: ReasonIO, Err: err}
		}
	})
	return s.closeErr
}

// CreationObservation is an immutable observation at an exact requested or
// ancestry path. It is not proof that mkdir created the observed inode. Facts
// may be unavailable; Identity can still retain a successful no-follow stat.
type CreationObservation struct {
	path     string
	phase    string
	identity Identity
	facts    AuthorityFacts
}

func (o CreationObservation) Path() string          { return o.path }
func (o CreationObservation) Phase() string         { return o.phase }
func (o CreationObservation) Identity() Identity    { return o.identity }
func (o CreationObservation) Facts() AuthorityFacts { return o.facts }

// CreationRecord records a successful exclusive creation syscall and available
// observations, never creator-inode ownership, restoration or deletion authority.
// The zero value records no creation. Getters return values, not mutable aliases.
type CreationRecord struct {
	path         string
	kind         uint32
	created      bool
	observations []CreationObservation
}

func (r CreationRecord) Path() string                        { return r.path }
func (r CreationRecord) Kind() uint32                        { return r.kind }
func (r CreationRecord) Created() bool                       { return r.created }
func (r CreationRecord) Observations() []CreationObservation { return slices.Clone(r.observations) }

// CreationError preserves a creation event through every subsequent failure,
// including cleanup failures. Underlying errors identify failing paths. It does
// not infer a moved object's current location or classify transaction outcomes.
type CreationError struct {
	record CreationRecord
	err    error
}

func (e *CreationError) Record() CreationRecord { return e.record }
func (e *CreationError) Unwrap() error          { return e.err }
func (e *CreationError) Error() string {
	return fmt.Sprintf(
		"nativefs creation retained or uncertain: requested path=%q kind=%#x observations=%+v; Inspect this exact path and the failing paths below before retrying; no removal or restoration was attempted: %v",
		e.record.path,
		e.record.kind,
		e.record.observations,
		e.err,
	)
}
