//go:build (linux || (darwin && cgo)) && !android && !ios

package nativefs

import (
	"context"
	"testing"

	"golang.org/x/sys/unix"
)

// Refines the tests-first publication cases: cancellation occurs in the last
// post-flock observation, not merely in the wait syscall callback.
func TestFlockFinalObservationCancellation(t *testing.T) {
	flockVariants(t, func(t *testing.T, directory bool) {
		_, d, calls := flockFixture(t, directory)
		ctx, cancel := flockContext(t)
		defer cancel()
		held := false
		observations, unlocks := 0, 0
		calls.flock = func(fd, flags int) error {
			err := unix.Flock(fd, flags)
			if flags == unix.LOCK_UN {
				unlocks++
			} else if err == nil {
				held = true
			}
			return err
		}
		observe := calls.private.observe
		calls.private.observe = func(nodes []pinnedDir, role AuthorityRole, readers authorityCalls) ([]AuthorityFacts, error) {
			facts, err := observe(nodes, role, readers)
			if held && err == nil {
				observations++
				if observations == 2 {
					cancel()
				}
			}
			return facts, err
		}
		lease, err := takeFlock(ctx, d, directory, calls)
		refuseFlock(t, lease, err, context.Canceled, flockPath(d, directory))
		if observations != 2 || unlocks != 1 {
			t.Fatalf("final-observation/unlock=%d/%d", observations, unlocks)
		}
		assertFlockInactive(t, d)
	})
}
