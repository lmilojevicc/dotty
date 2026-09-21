package nativefs

import (
	"errors"
	"sync"
)

// Lease owns one independent locked description and its pinned ancestry. Copies
// share lifetime. Release before awaiting the originating Dir.Close. Context
// cancellation after publication does not revoke a returned lease.
type Lease struct {
	state *leaseState
}

type leaseState struct {
	fd            int
	path          string
	unlock        func(int) error
	close         func() error
	releaseParent func()
	once          sync.Once
	err           error
}

// Release attempts unlock and close exactly once, independently, retaining all
// path-wrapped causes. Neither ambiguous native failure is retried. It never
// reacquires an originating Dir that may already be closing.
func (l *Lease) Release() error {
	if l == nil || l.state == nil {
		return nil
	}
	s := l.state
	s.once.Do(func() {
		if s.releaseParent != nil {
			defer s.releaseParent()
		}
		var unlockErr error
		if s.unlock != nil {
			if err := s.unlock(s.fd); err != nil {
				unlockErr = &Error{Op: "unlock", Path: s.path, Reason: ReasonIO, Err: err}
			}
		}
		s.err = errors.Join(unlockErr, s.close())
	})
	return s.err
}
