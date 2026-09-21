//go:build (linux || (darwin && cgo)) && !android && !ios

package nativefs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Defined, with their implementations and supporting regressions, before 31c
// production edits. This is source ordering only, not executed red evidence.
var requiredFlockCases = []string{
	"FileLockSerializesProcesses",
	"DirectoryLockSerializesProcesses",
	"IndependentDescriptionPerLease",
	"ContentionCancellation",
	"EINTRRetryAndCancellation",
	"CloseCancelsWaiters",
	"ReturnedLeasePinsAncestry",
	"ConcurrentReleaseOnce",
	"UnlockCloseErrorsJoined",
	"FailedAcquisitionReleasesResources",
	"PreWaitPolicyDrift",
	"PostWaitPolicyIdentityTopologyDrift",
	"UnsupportedFlockRefused",
	"NoSystemAncestorFlock",
	"NoC4Classification",
}

func TestFlockBoundary(t *testing.T) {
	runAuthorityInventory(t, requiredFlockCases, map[string]func(*testing.T){
		"FileLockSerializesProcesses":         func(t *testing.T) { testFlockProcesses(t, false) },
		"DirectoryLockSerializesProcesses":    func(t *testing.T) { testFlockProcesses(t, true) },
		"IndependentDescriptionPerLease":      testFlockIndependent,
		"ContentionCancellation":              testFlockCancellation,
		"EINTRRetryAndCancellation":           testFlockEINTR,
		"CloseCancelsWaiters":                 testFlockClose,
		"ReturnedLeasePinsAncestry":           testFlockPins,
		"ConcurrentReleaseOnce":               testFlockReleaseOnce,
		"UnlockCloseErrorsJoined":             testFlockReleaseErrors,
		"FailedAcquisitionReleasesResources":  testFlockFailures,
		"PreWaitPolicyDrift":                  func(t *testing.T) { testFlockDrift(t, false) },
		"PostWaitPolicyIdentityTopologyDrift": func(t *testing.T) { testFlockDrift(t, true) },
		"UnsupportedFlockRefused":             testFlockUnsupported,
		"NoSystemAncestorFlock":               testFlockAncestors,
		"NoC4Classification":                  testFlockPrimitiveErrors,
	})
}

func flockVariants(t *testing.T, fn func(*testing.T, bool)) {
	t.Helper()
	for _, directory := range []bool{false, true} {
		name := "file"
		if directory {
			name = "directory"
		}
		t.Run(name, func(t *testing.T) { fn(t, directory) })
	}
}

func flockFixture(t *testing.T, directory bool) (string, *Dir, flockCalls) {
	t.Helper()
	root, _, calls := privateFixture(t)
	path := filepath.Join(root, "selected")
	mkdir(t, path)
	d := openDir(t, path)
	if !directory {
		write(t, filepath.Join(path, "lock"), "untouched")
	}
	return root, d, flockCalls{private: calls, flock: unix.Flock}
}

func takeFlock(ctx context.Context, d *Dir, directory bool, calls flockCalls) (*Lease, error) {
	if directory {
		return d.lockDirectory(ctx, calls)
	}
	return d.lockFileLease(ctx, Component{name: "lock"}, calls)
}

func flockPath(d *Dir, directory bool) string {
	if directory {
		return d.state.leaf().path
	}
	return filepath.Join(d.state.leaf().path, "lock")
}

func requireFlock(
	t *testing.T,
	ctx context.Context,
	d *Dir,
	directory bool,
	calls flockCalls,
) *Lease {
	t.Helper()
	lease, err := takeFlock(ctx, d, directory, calls)
	if lease != nil {
		t.Cleanup(func() { must(t, lease.Release()) })
	}
	must(t, err)
	if lease == nil {
		t.Fatal("successful acquisition without lease")
	}
	return lease
}

func refuseFlock(t *testing.T, lease *Lease, err error, cause error, path string) {
	t.Helper()
	// Release unexpected successes before Fatal, so parent cleanup cannot hang.
	if lease != nil {
		_ = lease.Release()
		t.Fatal("unexpected published lease")
	}
	if err == nil || (cause != nil && !errors.Is(err, cause)) ||
		!strings.Contains(err.Error(), path) {
		t.Fatalf("want refusal cause=%v path=%q, got %v", cause, path, err)
	}
}

func flockContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(t.Context(), 10*time.Second)
}

func awaitFlock[T any](t *testing.T, ctx context.Context, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-ctx.Done():
		t.Fatalf("flock handshake timed out: %v", ctx.Err())
	}
	var zero T
	return zero
}

type flockResult struct {
	lease *Lease
	err   error
}

func pendingFlock(
	ctx context.Context,
	d *Dir,
	directory bool,
	calls flockCalls,
) <-chan flockResult {
	result := make(chan flockResult, 1)
	go func() { lease, err := takeFlock(ctx, d, directory, calls); result <- flockResult{lease, err} }()
	return result
}

func testFlockIndependent(t *testing.T) {
	flockVariants(t, func(t *testing.T, directory bool) {
		_, d, calls := flockFixture(t, directory)
		ctx, cancel := flockContext(t)
		defer cancel()
		first := requireFlock(t, ctx, d, directory, calls)
		contended := make(chan int, 1)
		var once sync.Once
		calls.flock = func(fd, flags int) error {
			err := unix.Flock(fd, flags)
			if flags == unix.LOCK_EX|unix.LOCK_NB && errors.Is(err, unix.EWOULDBLOCK) {
				once.Do(func() { contended <- fd })
			}
			return err
		}
		result := pendingFlock(ctx, d, directory, calls)
		received := false
		defer func() {
			cancel()
			if !received {
				// Drain an unexpected early publication before parent Close.
				r := <-result
				if r.lease != nil {
					_ = r.lease.Release()
				}
			}
		}()
		fd := awaitFlock(t, ctx, contended)
		if fd == first.state.fd {
			t.Error("same descriptor reused")
		}
		must(t, first.Release())
		second := awaitFlock(t, ctx, result)
		received = true
		if second.lease != nil {
			defer func() { must(t, second.lease.Release()) }()
		}
		must(t, second.err)
		if second.lease == nil {
			t.Fatal("contender did not acquire")
		}
		// Releasing the already released first wrapper must not unlock the second.
		must(t, first.Release())
		waitCtx, stop := context.WithCancel(ctx)
		calls.flock = func(fd, flags int) error {
			err := unix.Flock(fd, flags)
			if flags == unix.LOCK_EX|unix.LOCK_NB && errors.Is(err, unix.EWOULDBLOCK) {
				stop()
			}
			return err
		}
		third, err := takeFlock(waitCtx, d, directory, calls)
		stop()
		refuseFlock(t, third, err, context.Canceled, flockPath(d, directory))
	})
}

func testFlockCancellation(t *testing.T) {
	flockVariants(t, func(t *testing.T, directory bool) {
		for _, deadline := range []bool{false, true} {
			t.Run(map[bool]string{false: "cancel", true: "deadline"}[deadline], func(t *testing.T) {
				_, d, calls := flockFixture(t, directory)
				ctx, cancel := flockContext(t)
				defer cancel()
				first := requireFlock(t, ctx, d, directory, calls)
				defer func() { must(t, first.Release()) }()
				waitCtx, stop := context.WithCancel(ctx)
				if deadline {
					stop()
					waitCtx, stop = context.WithTimeout(ctx, 2*time.Second)
				}
				defer stop()
				attempts := 0
				calls.flock = func(fd, flags int) error {
					err := unix.Flock(fd, flags)
					if flags == unix.LOCK_EX|unix.LOCK_NB && errors.Is(err, unix.EWOULDBLOCK) {
						attempts++
						if !deadline {
							stop()
						}
					}
					return err
				}
				lease, err := takeFlock(waitCtx, d, directory, calls)
				cause := context.Canceled
				if deadline {
					cause = context.DeadlineExceeded
				}
				refuseFlock(t, lease, err, cause, flockPath(d, directory))
				if attempts == 0 || attempts > 500 {
					t.Fatalf("contention attempts=%d", attempts)
				}
			})
		}
	})
}

func testFlockEINTR(t *testing.T) {
	flockVariants(t, func(t *testing.T, directory bool) {
		for _, cancellation := range []bool{false, true} {
			t.Run(
				map[bool]string{false: "retry", true: "cancel"}[cancellation],
				func(t *testing.T) {
					_, d, calls := flockFixture(t, directory)
					ctx, cancel := flockContext(t)
					defer cancel()
					attempts := 0
					calls.flock = func(fd, flags int) error {
						if flags == unix.LOCK_UN {
							return unix.Flock(fd, flags)
						}
						attempts++
						if attempts <= 3 {
							if cancellation && attempts == 3 {
								cancel()
							}
							return unix.EINTR
						}
						return unix.Flock(fd, flags)
					}
					lease, err := takeFlock(ctx, d, directory, calls)
					if cancellation {
						refuseFlock(t, lease, err, context.Canceled, flockPath(d, directory))
					} else {
						if lease != nil {
							must(t, lease.Release())
						}
						must(t, err)
						if lease == nil {
							t.Fatal("EINTR retry returned no lease")
						}
					}
					want := 4
					if cancellation {
						want = 3
					}
					if attempts != want {
						t.Fatalf("EINTR attempts=%d want=%d", attempts, want)
					}
				},
			)
		}
	})
}

func testFlockClose(t *testing.T) {
	flockVariants(t, func(t *testing.T, directory bool) {
		for _, acquired := range []bool{false, true} {
			t.Run(
				map[bool]string{false: "waiter", true: "publication-loses"}[acquired],
				func(t *testing.T) {
					_, d, calls := flockFixture(t, directory)
					ctx, cancel := flockContext(t)
					defer cancel()
					closed := make(chan error, 1)
					attempts, unlocks := 0, 0
					calls.flock = func(fd, flags int) error {
						if flags == unix.LOCK_UN {
							unlocks++
							return unix.Flock(fd, flags)
						}
						attempts++
						var err error
						if acquired {
							err = unix.Flock(fd, flags)
						} else {
							err = unix.EWOULDBLOCK
						}
						go func() { closed <- d.Close() }()
						awaitFlock(t, ctx, closingSignal(d.state))
						return err
					}
					lease, err := takeFlock(ctx, d, directory, calls)
					refuseFlock(t, lease, err, nil, flockPath(d, directory))
					_ = reason(t, err, ReasonClosed)
					must(t, awaitFlock(t, ctx, closed))
					wantUnlocks := 0
					if acquired {
						wantUnlocks = 1
					}
					if attempts != 1 || unlocks != wantUnlocks {
						t.Fatalf("attempts/unlocks=%d/%d", attempts, unlocks)
					}
					lease, err = takeFlock(ctx, d, directory, calls)
					refuseFlock(t, lease, err, nil, "")
					_ = reason(t, err, ReasonClosed)
				},
			)
		}
	})
}

func testFlockPins(t *testing.T) {
	flockVariants(t, func(t *testing.T, directory bool) {
		_, d, calls := flockFixture(t, directory)
		ctx, cancel := flockContext(t)
		defer cancel()
		lease := requireFlock(t, ctx, d, directory, calls)
		closed := make(chan error, 1)
		go func() { closed <- d.Close() }()
		awaitFlock(t, ctx, closingSignal(d.state))
		select {
		case err := <-closed:
			t.Fatalf("Close bypassed published lease: %v", err)
		default:
		}
		for _, node := range d.state.nodes {
			_, err := statFD(node.fd)
			must(t, err)
		}
		// An independent process still sees contention while parent Close waits.
		child := startFlockProcess(t, d, directory)
		child.send(t, "go")
		child.expect(t, "contended")
		must(t, lease.Release())
		child.expect(t, "acquired")
		child.send(t, "release")
		child.expect(t, "released")
		child.finish(t)
		must(t, awaitFlock(t, ctx, closed))
	})
}

func testFlockReleaseOnce(t *testing.T) {
	flockVariants(t, func(t *testing.T, directory bool) {
		_, d, calls := flockFixture(t, directory)
		unlocks := 0
		closed := map[int]int{}
		calls.flock = func(fd, flags int) error {
			if flags == unix.LOCK_UN {
				unlocks++
			}
			return unix.Flock(fd, flags)
		}
		calls.private.acquisition.close = func(fd int) error { closed[fd]++; return unix.Close(fd) }
		ctx, cancel := flockContext(t)
		defer cancel()
		lease := requireFlock(t, ctx, d, directory, calls)
		copy := *lease
		var wg sync.WaitGroup
		results := make(chan error, 32)
		for i := 0; i < 32; i++ {
			wg.Go(func() { results <- copy.Release() })
		}
		wg.Wait()
		close(results)
		for err := range results {
			must(t, err)
		}
		wantClosed := 1
		if directory {
			wantClosed = len(d.state.nodes)
		}
		if unlocks != 1 || len(closed) != wantClosed {
			t.Fatalf("release counts=%d/%v want closed=%d", unlocks, closed, wantClosed)
		}
		for fd, count := range closed {
			if count != 1 {
				t.Fatalf("fd=%d closed %d times", fd, count)
			}
		}
		assertFlockInactive(t, d)
	})
}

func testFlockReleaseErrors(t *testing.T) {
	flockVariants(t, func(t *testing.T, directory bool) {
		_, d, calls := flockFixture(t, directory)
		unlockErr, closeErr := errors.New("injected unlock"), errors.New("injected close")
		unlocks, closes, selected := 0, 0, -1
		calls.flock = func(fd, flags int) error {
			if flags == unix.LOCK_UN {
				unlocks++
				return errors.Join(unix.Flock(fd, flags), unlockErr)
			}
			selected = fd
			return unix.Flock(fd, flags)
		}
		calls.private.acquisition.close = func(fd int) error {
			err := unix.Close(fd)
			if fd == selected {
				closes++
				return errors.Join(err, closeErr)
			}
			return err
		}
		ctx, cancel := flockContext(t)
		defer cancel()
		lease, err := takeFlock(ctx, d, directory, calls)
		if err != nil {
			if lease != nil {
				_ = lease.Release()
			}
			t.Fatal(err)
		}
		if lease == nil {
			t.Fatal("missing lease")
		}
		result := lease.Release()
		if !errors.Is(result, unlockErr) || !errors.Is(result, closeErr) ||
			strings.Count(result.Error(), flockPath(d, directory)) < 2 {
			t.Fatalf("lost joined path/cause: %v", result)
		}
		//nolint:errorlint // Compare cached error identity, not wrapped-cause membership.
		if again := lease.Release(); again != result || unlocks != 1 || closes != 1 {
			t.Fatalf("release retried: %v %d/%d", again, unlocks, closes)
		}
		assertFlockInactive(t, d)
	})
}

func assertFlockInactive(t *testing.T, d *Dir) {
	t.Helper()
	lifecycleMu.Lock()
	active := d.state.active
	lifecycleMu.Unlock()
	if active != 0 {
		t.Fatalf("retained parent tokens=%d", active)
	}
}

func testFlockFailures(t *testing.T) {
	flockVariants(t, func(t *testing.T, directory bool) {
		for _, phase := range []string{"open", "wait", "postguard", "publication", "publication-joined-cleanup"} {
			t.Run(phase, func(t *testing.T) {
				_, d, calls := flockFixture(t, directory)
				ctx, cancel := flockContext(t)
				defer cancel()
				cause := errors.New("injected " + phase)
				closeErr, unlockErr := errors.New("cleanup close"), errors.New("cleanup unlock")
				hits, attempts, unlocks := 0, 0, 0
				opened, closed := map[int]int{}, map[int]int{}
				open := calls.private.acquisition.openat
				calls.private.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
					if phase == "open" {
						hits++
						return -1, cause
					}
					out, err := open(fd, name, flags, mode)
					if err == nil {
						opened[out]++
					}
					return out, err
				}
				calls.private.acquisition.close = func(fd int) error {
					closed[fd]++
					err := unix.Close(fd)
					if phase == "publication-joined-cleanup" {
						return errors.Join(err, closeErr)
					}
					return err
				}
				calls.flock = func(fd, flags int) error {
					if flags == unix.LOCK_UN {
						unlocks++
						err := unix.Flock(fd, flags)
						if phase == "publication-joined-cleanup" {
							return errors.Join(err, unlockErr)
						}
						return err
					}
					attempts++
					if phase == "wait" {
						hits++
						return cause
					}
					err := unix.Flock(fd, flags)
					if strings.HasPrefix(phase, "publication") {
						hits++
						cancel()
					}
					return err
				}
				observe := calls.private.observe
				calls.private.observe = func(nodes []pinnedDir, role AuthorityRole, readers authorityCalls) ([]AuthorityFacts, error) {
					if phase == "postguard" && attempts != 0 {
						hits++
						return nil, &Error{
							Op:     "injected-postguard",
							Path:   d.state.leaf().path,
							Reason: ReasonIO,
							Err:    cause,
						}
					}
					return observe(nodes, role, readers)
				}
				lease, err := takeFlock(ctx, d, directory, calls)
				if strings.HasPrefix(phase, "publication") {
					cause = context.Canceled
				}
				// Repinning may fail at a higher component, so require its exact injected path below.
				path := d.state.leaf().path
				if phase == "open" && directory {
					path = d.state.nodes[1].path
				}
				refuseFlock(t, lease, err, cause, path)
				if hits != 1 {
					t.Fatalf("failure hook reached %d times", hits)
				}
				wantAttempts, wantUnlocks := 1, 1
				if phase == "open" {
					wantAttempts, wantUnlocks = 0, 0
				}
				if phase == "wait" {
					wantUnlocks = 0
				}
				if attempts != wantAttempts || unlocks != wantUnlocks {
					t.Fatalf("attempt/unlock=%d/%d", attempts, unlocks)
				}
				if phase == "publication-joined-cleanup" &&
					(!errors.Is(err, closeErr) || !errors.Is(err, unlockErr)) {
					t.Fatalf("cleanup causes lost: %v", err)
				}
				wantClosed := len(opened)
				if directory {
					wantClosed++
				} // openPhysicalDir also owns its fresh / FD.
				if len(closed) != wantClosed {
					t.Fatalf("closed descriptions=%d want=%d", len(closed), wantClosed)
				}
				for fd, count := range opened {
					if count != 1 || closed[fd] != 1 {
						t.Fatalf("fd=%d open/close=%d/%d", fd, count, closed[fd])
					}
				}
				for fd, count := range closed {
					if count != 1 {
						t.Fatalf("fd=%d close=%d", fd, count)
					}
				}
				assertFlockInactive(t, d)
			})
		}
	})
}

// Drift is injected into real descriptor observations only after native fixture
// authority is established. Every intended hook must fire, never an early refusal.
func testFlockDrift(t *testing.T, post bool) {
	flockVariants(t, func(t *testing.T, directory bool) {
		for _, target := range []string{"parent", "leaf"} {
			for _, field := range []string{"uid", "gid", "mode", "acl", "mount", "nlink"} {
				t.Run(target+"/"+field, func(t *testing.T) {
					root, d, calls := flockFixture(t, directory)
					ctx, cancel := flockContext(t)
					defer cancel()
					attempts, unlocks, hits, observations := 0, 0, 0, 0
					armed, transitionReached := false, false
					selectedFD, selectedOpens, fileStats, fileReads := -1, 0, 0, 0
					fileBaselineComplete := false
					var baseline, fileFacts, firstStat AuthorityFacts
					path := flockPath(d, directory)
					if target == "parent" {
						path = root
						if !directory {
							path = d.state.leaf().path
						}
					}
					calls.flock = func(fd, flags int) error {
						if flags == unix.LOCK_UN {
							unlocks++
						} else {
							attempts++
							if post {
								armed = true
							}
						}
						return unix.Flock(fd, flags)
					}
					open := calls.private.acquisition.openat
					calls.private.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
						out, err := open(fd, name, flags, mode)
						selected := name == "lock"
						if directory {
							selected = name == d.state.leaf().name.name
						}
						if err == nil && selected {
							selectedFD = out
							selectedOpens++
						}
						return out, err
					}
					alter := func(f *AuthorityFacts) {
						switch field {
						case "uid":
							f.uid++
						case "gid":
							f.gid++ // Still role-valid; equality must reject it.
						case "mode":
							f.mode ^= 0o100
						case "acl":
							f.security.state = EvidencePresent
						case "mount":
							f.filesystem.flags ^= 1
						case "nlink":
							f.nlink++
						}
					}
					if !directory {
						stat := calls.private.authority.stat
						security := calls.private.authority.security
						calls.private.authority.security = func(fd int, dir bool) (SecurityEvidence, error) {
							f, err := security(fd, dir)
							if err == nil && fd == selectedFD && !dir {
								fileReads++
								fileFacts.security = f
								if armed && target == "leaf" && field == "acl" {
									hits++
									f.state = EvidencePresent
								}
							}
							return f, err
						}
						filesystem := calls.private.authority.filesystem
						calls.private.authority.filesystem = func(fd int) (FilesystemFacts, error) {
							f, err := filesystem(fd)
							if err == nil && fd == selectedFD {
								fileFacts.filesystem = f
								if armed && target == "leaf" && field == "mount" {
									hits++
									f.flags ^= 1
								}
							}
							return f, err
						}
						calls.private.authority.stat = func(fd int, st *unix.Stat_t) error {
							err := stat(fd, st)
							if err == nil && fd == selectedFD {
								fileStats++
								if fileStats%2 == 1 {
									firstStat = statAuthority(st)
									fileFacts = firstStat
								}
								// Capture the complete, unaltered second 31b observation,
								// including its final stat. Arming here would still be too early.
								if fileStats == 4 && fileReads == 2 && !armed &&
									firstStat == statAuthority(st) {
									fileBaselineComplete = true
									if target == "leaf" {
										baseline = fileFacts
									}
								}
								if armed && target == "leaf" {
									switch field {
									case "uid":
										st.Uid++
										hits++
									case "gid":
										st.Gid++
										hits++
									case "mode":
										st.Mode ^= 0o100
										hits++
									case "nlink":
										st.Nlink++
										hits++
									}
								}
							}
							return err
						}
					}
					observe := calls.private.observe
					calls.private.observe = func(nodes []pinnedDir, role AuthorityRole, readers authorityCalls) ([]AuthorityFacts, error) {
						facts, err := observe(nodes, role, readers)
						if err != nil {
							return facts, err
						}
						observations++
						if observations == 2 && (directory || target == "parent") {
							for i, node := range nodes {
								if node.path == path {
									baseline = facts[i]
								}
							}
						}
						// Directory observation 3 uses the freshly repinned selected FD;
						// reaching it proves privateParent's two-observation baseline passed.
						// File parent observation 7 is the first boundFile in flock validate:
						// both 31b file observations and their policy/equality checks returned.
						// Arm before this boundFile starts its first file authority read.
						if selectedOpens == 1 && selectedFD >= 0 &&
							selectedFD != d.state.leaf().fd &&
							((directory && observations == 3 && nodes[len(nodes)-1].fd == selectedFD) ||
								(!directory && observations == 7 && fileBaselineComplete && fileStats == 4 && fileReads == 2)) {
							transitionReached = true
							if !post {
								armed = true
							}
						}
						if armed && (directory || target == "parent") {
							for i, node := range nodes {
								if node.path == path {
									alter(&facts[i])
									hits++
								}
							}
						}
						return facts, nil
					}
					lease, err := takeFlock(ctx, d, directory, calls)
					refuseFlock(t, lease, err, nil, path)
					if !transitionReached || selectedOpens != 1 || !baseline.identity.Valid {
						t.Fatalf(
							"flock transition not proven: reached=%t opens=%d baseline=%+v",
							transitionReached,
							selectedOpens,
							baseline,
						)
					}
					wantObservations := 3
					if post {
						wantObservations = 5
					}
					if !directory {
						wantObservations = 7
						if post {
							wantObservations = 9
						}
						wantReads := 2
						if post {
							wantReads = 4
						}
						if target == "leaf" {
							wantReads++
						}
						if !fileBaselineComplete || fileReads != wantReads ||
							fileStats != 2*wantReads {
							t.Fatalf(
								"file phase: baseline=%t security/stat=%d/%d want=%d/%d",
								fileBaselineComplete,
								fileReads,
								fileStats,
								wantReads,
								2*wantReads,
							)
						}
					}
					if observations != wantObservations {
						t.Fatalf(
							"parent/owned-chain observations=%d want=%d",
							observations,
							wantObservations,
						)
					}
					var authority *AuthorityError
					wantRole := RoleRepositoryDirectory
					if target == "parent" {
						wantRole = RoleSystemAncestor
					}
					if !directory {
						wantRole = RoleLockFile
						if target == "parent" {
							wantRole = RolePrivateAnchor
						}
					}
					wantReason := ReasonAuthorityChanged
					wantExpected, wantObserved := baseline, baseline
					alter(&wantObserved)
					wantDetail := "authority changed between guarded observations" // compareAuthorityNodes, not readAuthorityFacts self-drift.
					if !directory && target == "leaf" && field != "gid" && field != "mount" {
						wantReason = ReasonAuthorityPolicy
						wantExpected = AuthorityFacts{} // validateAuthority does not report an expected baseline.
						wantDetail = "lock file requires effective UID, regular type, exact 0600, no special bits, and one link"
						if field == "acl" {
							wantDetail = "an ACL is present; this boundary supports verified absence only"
						}
					}
					if !errors.As(err, &authority) ||
						authority.Op != "authority" ||
						authority.Path != path ||
						authority.Role != wantRole ||
						authority.Reason != wantReason ||
						authority.Expected != wantExpected ||
						authority.Observed != wantObserved ||
						authority.Detail != wantDetail {
						t.Fatalf(
							"wrong native drift diagnostic: got=%+v want role=%d reason=%s path=%q expected=%+v observed=%+v detail=%q",
							authority,
							wantRole,
							wantReason,
							path,
							wantExpected,
							wantObserved,
							wantDetail,
						)
					}
					// Stat-field drift now covers both ends of a whole observation,
					// including pre-wait: it cannot pass via reader-internal disagreement.
					wantHits := 1
					if !directory && target == "leaf" && field != "acl" && field != "mount" {
						wantHits = 2
					}
					if hits != wantHits {
						t.Fatalf(
							"drift hook=%d want=%d observations=%d",
							hits,
							wantHits,
							observations,
						)
					}
					want := 0
					if post {
						want = 1
					}
					if attempts != want || unlocks != want {
						t.Fatalf("drift flock/unlock=%d/%d want=%d", attempts, unlocks, want)
					}
					assertFlockInactive(t, d)
				})
			}
		}
		if post {
			testFlockTopology(t, directory)
		}
	})
}

func testFlockTopology(t *testing.T, directory bool) {
	for _, swap := range []string{"leaf-replace", "leaf-symlink", "ancestor-replace", "ancestor-symlink", "above-boundary"} {
		t.Run(swap, func(t *testing.T) {
			root, d, calls := flockFixture(t, directory)
			ctx, cancel := flockContext(t)
			defer cancel()
			path := flockPath(d, directory)
			if strings.HasPrefix(swap, "ancestor") {
				path = root
			}
			if swap == "above-boundary" {
				path = filepath.Dir(root)
			}
			attempts, unlocks := 0, 0
			calls.flock = func(fd, flags int) error {
				if flags == unix.LOCK_UN {
					unlocks++
					return unix.Flock(fd, flags)
				}
				attempts++
				err := unix.Flock(fd, flags)
				if err != nil {
					return err
				}
				moved := path + "-moved"
				must(t, os.Rename(path, moved))
				// All paths are private test trees, including the parent allocated
				// by testing for this individual case. Restore only fixture names.
				t.Cleanup(func() { must(t, os.RemoveAll(path)); must(t, os.Rename(moved, path)) })
				if strings.Contains(swap, "symlink") {
					must(t, os.Symlink(moved, path))
				} else if !directory && swap == "leaf-replace" {
					write(t, path, "replacement")
				} else {
					mkdir(t, path)
				}
				return nil
			}
			lease, err := takeFlock(ctx, d, directory, calls)
			refuseFlock(t, lease, err, nil, path)
			if attempts != 1 || unlocks != 1 {
				t.Fatalf("swap reachability=%d/%d", attempts, unlocks)
			}
			assertFlockInactive(t, d)
		})
	}
}

func testFlockUnsupported(t *testing.T) {
	flockVariants(t, func(t *testing.T, directory bool) {
		for _, test := range []struct {
			name   string
			err    error
			reason Reason
		}{
			{"ENOSYS", unix.ENOSYS, ReasonUnsupported},
			{"ENOTSUP", unix.ENOTSUP, ReasonUnsupported},
			{"EOPNOTSUPP", unix.EOPNOTSUPP, ReasonUnsupported},
			{"EINVAL", unix.EINVAL, ReasonInvalidOperation},
			{"EBADF", unix.EBADF, ReasonIO},
		} {
			t.Run(test.name, func(t *testing.T) {
				_, d, calls := flockFixture(t, directory)
				attempts := 0
				calls.flock = func(int, int) error { attempts++; return test.err }
				ctx, cancel := flockContext(t)
				defer cancel()
				lease, err := takeFlock(ctx, d, directory, calls)
				refuseFlock(t, lease, err, test.err, flockPath(d, directory))
				_ = reason(t, err, test.reason)
				if attempts != 1 {
					t.Fatalf("unsupported hook=%d", attempts)
				}
				assertFlockInactive(t, d)
			})
		}
	})
}

func testFlockAncestors(t *testing.T) {
	for _, target := range []struct{ name, path string }{
		{"root", "/"},
		{"temporary-root", systemTemporaryRoot},
	} {
		t.Run(target.name, func(t *testing.T) {
			path := target.path
			d := openDir(t, path) // Read-only pins only; never mutate or flock it.
			calls := systemFlockCalls()
			attempts := 0
			calls.flock = func(int, int) error { attempts++; return errors.New("system flock reached") }
			lease, err := d.lockDirectory(t.Context(), calls)
			refuseFlock(t, lease, err, nil, path)
			_ = authorityReason(t, err, ReasonAuthorityPolicy, path)
			if attempts != 0 {
				t.Fatal("system ancestor flocked")
			}
		})
	}
	flockVariants(t, func(t *testing.T, directory bool) {
		_, d, calls := flockFixture(t, directory)
		wanted := d.state.leaf().identity
		if !directory {
			wanted = observe(t, d, "lock")
		}
		attempts := 0
		calls.flock = func(fd, flags int) error {
			id, err := statFD(fd)
			if err != nil {
				return err
			}
			if id != wanted {
				t.Error("flock targeted an ancestor")
			}
			attempts++
			return unix.Flock(fd, flags)
		}
		ctx, cancel := flockContext(t)
		defer cancel()
		lease := requireFlock(t, ctx, d, directory, calls)
		must(t, lease.Release())
		if attempts != 2 {
			t.Fatalf("flock/unlock calls=%d", attempts)
		}
	})
}

func testFlockPrimitiveErrors(t *testing.T) {
	flockVariants(t, func(t *testing.T, directory bool) {
		_, d, calls := flockFixture(t, directory)
		attempts := 0
		calls.flock = func(int, int) error { attempts++; return unix.EIO }
		lease, err := takeFlock(t.Context(), d, directory, calls)
		refuseFlock(t, lease, err, unix.EIO, flockPath(d, directory))
		_ = reason(t, err, ReasonIO)
		for _, forbidden := range []string{"No changes", "restored", "committed", "rollback", "Outcome"} {
			if strings.Contains(err.Error(), forbidden) {
				t.Fatalf("C4 claim: %v", err)
			}
		}
		if attempts != 1 {
			t.Fatal("failure hook not reached exactly once")
		}
	})
}

func TestFlockInputsAndPublication(t *testing.T) {
	flockVariants(t, func(t *testing.T, directory bool) {
		for _, input := range []string{"nil-context", "canceled", "nil-dir", "zero-dir", "closed-dir", "cancel-after-publication"} {
			t.Run(input, func(t *testing.T) {
				_, d, calls := flockFixture(t, directory)
				original := d
				ctx, cancel := flockContext(t)
				defer cancel()
				attempts := 0
				calls.flock = func(fd, flags int) error { attempts++; return unix.Flock(fd, flags) }
				switch input {
				case "nil-context":
					ctx = nil
				case "canceled":
					cancel()
				case "nil-dir":
					d = nil
				case "zero-dir":
					d = &Dir{}
				case "closed-dir":
					must(t, d.Close())
				}
				lease, err := takeFlock(ctx, d, directory, calls)
				if input == "cancel-after-publication" {
					if lease != nil {
						defer func() { must(t, lease.Release()) }()
					}
					must(t, err)
					cancel()
					if lease == nil {
						t.Fatal("missing published lease")
					}
					// Cancellation cannot revoke an already returned lease.
					otherCtx, stop := context.WithCancel(t.Context())
					defer stop()
					calls.flock = func(fd, flags int) error {
						err := unix.Flock(fd, flags)
						if errors.Is(err, unix.EWOULDBLOCK) {
							stop()
						}
						return err
					}
					other, otherErr := takeFlock(otherCtx, d, directory, calls)
					refuseFlock(t, other, otherErr, context.Canceled, flockPath(d, directory))
					return
				}
				refuseFlock(t, lease, err, nil, "")
				if input == "canceled" && !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if attempts != 0 {
					t.Fatalf("invalid input reached flock=%d", attempts)
				}
				assertFlockInactive(t, original)
			})
		}
	})
	var empty *Lease
	must(t, empty.Release())
	must(t, (&Lease{}).Release())
	_, d, calls := flockFixture(t, false)
	lease, err := d.lockFileLease(t.Context(), Component{}, calls)
	refuseFlock(t, lease, err, nil, "")
	_ = reason(t, err, ReasonInvalidComponent)
	if typ := reflect.TypeFor[*Lease](); typ.NumMethod() != 1 || typ.Method(0).Name != "Release" {
		t.Fatal("unsafe Lease API")
	}
}
