//go:build (linux || (darwin && cgo)) && !android && !ios

package nativefs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

// The exact slice-32 roots and implementations precede production coordinator
// edits. This records source ordering, not an executed red run.
var requiredCoordinatorCases = []string{
	"CanonicalEUIDIgnoresEnvironment",
	"UserBeforeResolution",
	"OneCompleteBatch",
	"AliasDedupStableOrder",
	"AliasRetargetRefused",
	"PhysicalAuthorityDrift",
	"ExplicitEEXISTOpen",
	"CreationRecordsSurviveFailure",
	"CancellationUnwindsReverse",
	"ReleaseConcurrentOnceAllErrors",
	"NoSharedDescription",
	"NoAnchorRemoval",
	"BootstrapIdentityContinuity",
	"MissingRepositoryRefused",
}

func TestCoordinatorBoundary(t *testing.T) {
	runAuthorityInventory(t, requiredCoordinatorCases, map[string]func(*testing.T){
		"CanonicalEUIDIgnoresEnvironment": testCoordinatorCanonical,
		"UserBeforeResolution":            testCoordinatorUserFirst,
		"OneCompleteBatch":                testCoordinatorBatch,
		"AliasDedupStableOrder":           testCoordinatorAliases,
		"AliasRetargetRefused":            testCoordinatorRetarget,
		"PhysicalAuthorityDrift":          testCoordinatorDrift,
		"ExplicitEEXISTOpen":              testCoordinatorCollision,
		"CreationRecordsSurviveFailure":   testCoordinatorRecords,
		"CancellationUnwindsReverse":      testCoordinatorCancel,
		"ReleaseConcurrentOnceAllErrors":  testCoordinatorRelease,
		"NoSharedDescription":             testCoordinatorDescriptions,
		"NoAnchorRemoval":                 testCoordinatorRetained,
		"BootstrapIdentityContinuity":     testCoordinatorBootstrap,
		"MissingRepositoryRefused":        testCoordinatorMissing,
	})
}

func coordinatorFixture(t *testing.T) (string, flockCalls, []string) {
	t.Helper()
	root, _, calls := privateFixture(t)
	// Allocate all siblings before any strict coordinator baselines.
	repos := []string{filepath.Join(root, "a"), filepath.Join(root, "z")}
	for _, path := range repos {
		mkdir(t, path)
	}
	return root, flockCalls{private: calls, flock: unix.Flock}, repos
}

func coordinatorTake(t *testing.T, root string, calls flockCalls) *UserLease {
	t.Helper()
	lease, err := acquireUserAt(t.Context(), root, calls)
	if lease != nil {
		t.Cleanup(func() { _ = lease.Release() })
	}
	must(t, err)
	if lease == nil {
		t.Fatal("missing user lease")
	}
	return lease
}

func coordinatorRefusal(t *testing.T, lease *UserLease, err, cause error, path string) {
	t.Helper()
	if lease != nil {
		_ = lease.Release()
		t.Fatal("unexpected user lease")
	}
	if err == nil || (cause != nil && !errors.Is(err, cause)) ||
		!strings.Contains(err.Error(), path) {
		t.Fatalf("want cause=%v path=%q, got %v", cause, path, err)
	}
	var report *CoordinatorError
	if !errors.As(err, &report) || !strings.Contains(err.Error(), "Inspect") {
		t.Fatalf("missing coordinator diagnostic: %v", err)
	}
}

func testCoordinatorCanonical(t *testing.T) {
	want := filepath.Join(systemTemporaryRoot, "dotty-"+strconv.Itoa(os.Geteuid()), "mutation.lock")
	for _, value := range []string{"", "/not/a/repository", "relative"} {
		t.Run("environment-"+value, func(t *testing.T) {
			for _, key := range []string{"HOME", "XDG_CONFIG_HOME", "DOTTY_REPO", "TMPDIR", "TMP", "TEMP"} {
				t.Setenv(key, value)
			}
			if got := canonicalUserLockPath(); got != want {
				t.Fatalf("canonical path=%q want=%q", got, want)
			}
		})
	}
	// Identity only: never call AcquireUser or touch the canonical UID anchor.
	t.Run("authority-before-create", func(t *testing.T) {
		root, _, _ := coordinatorFixture(t)
		calls := systemFlockCalls()
		creates, observations := 0, 0
		calls.private.mkdir = func(int, string, uint32) error { creates++; return unix.EIO }
		calls.private.authority.filesystem = func(int) (FilesystemFacts, error) {
			observations++
			return FilesystemFacts{state: EvidenceUnsupported}, nil
		}
		lease, err := acquireUserAt(t.Context(), root, calls)
		coordinatorRefusal(t, lease, err, nil, "/")
		_ = authorityReason(t, err, ReasonUnsupported, "/")
		if creates != 0 || observations != 1 {
			t.Fatalf("create/authority observations=%d/%d", creates, observations)
		}
	})
}

func testCoordinatorUserFirst(t *testing.T) {
	root, calls, repos := coordinatorFixture(t)
	lease := coordinatorTake(t, root, calls)
	if len(lease.RepositoryBindings()) != 0 {
		t.Fatal("repositories published in first stage")
	}
	// A private native child selects a different repository using a different
	// config-resolution choice, but cannot get past the same user rendezvous.
	child := startCoordinatorProcess(t, root, repos[1])
	child.send(t, "go")
	child.expect(t, "contended")
	must(t, lease.LockRepositories(t.Context(), repos[:1]))
	must(t, lease.Release())
	child.expect(t, "acquired")
	child.send(t, "release")
	child.expect(t, "released")
	child.finish(t)
}

func testCoordinatorBatch(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(strconv.FormatBool(empty), func(t *testing.T) {
			root, calls, repos := coordinatorFixture(t)
			lease := coordinatorTake(t, root, calls)
			if empty {
				repos = nil
			}
			ctx, cancel := context.WithCancel(t.Context())
			must(t, lease.LockRepositories(ctx, repos))
			cancel() // Published ownership is not automatically revoked.
			if err := lease.LockRepositories(t.Context(), nil); err == nil {
				t.Fatal("second batch accepted")
			}
			if len(lease.RepositoryBindings()) != len(repos) {
				t.Fatal("published batch lost")
			}
			waitCtx, stop := context.WithCancel(t.Context())
			defer stop()
			contentions := 0
			calls.flock = func(fd, flags int) error {
				err := unix.Flock(fd, flags)
				if flags == unix.LOCK_EX|unix.LOCK_NB && errors.Is(err, unix.EWOULDBLOCK) {
					contentions++
					stop()
				}
				return err
			}
			other, err := acquireUserAt(waitCtx, root, calls)
			coordinatorRefusal(t, other, err, context.Canceled, root)
			if contentions != 1 {
				t.Fatalf("published cancellation contention=%d", contentions)
			}
			must(t, lease.Release())
		})
	}
	t.Run("concurrent-winner-and-release", func(t *testing.T) {
		root, calls, repos := coordinatorFixture(t)
		entered, proceed := make(chan struct{}), make(chan struct{})
		ctx, cancel := flockContext(t)
		defer cancel()
		lease := coordinatorTake(t, root, calls)
		var once sync.Once
		lease.state.calls.flock = func(fd, flags int) error {
			if flags == unix.LOCK_EX|unix.LOCK_NB {
				once.Do(func() {
					close(entered)
					select {
					case <-proceed:
					case <-ctx.Done():
					}
				})
			}
			return unix.Flock(fd, flags)
		}
		result := make(chan error, 1)
		go func() { result <- lease.LockRepositories(ctx, repos) }()
		// Unblock before cleanup even if an assertion aborts.
		defer close(proceed)
		awaitFlock(t, ctx, entered)
		if err := lease.LockRepositories(ctx, nil); err == nil {
			t.Error("concurrent loser accepted")
		}
		lease.state.mu.Lock()
		if lease.state.batchContext.Err() != nil {
			t.Error("loser canceled winner")
		}
		lease.state.mu.Unlock()
		copy := *lease
		released := make(chan error, 1)
		go func() { released <- copy.Release() }()
		awaitFlock(t, ctx, lease.state.batchContext.Done())
		// The flock hook also observes cancellation, avoiding Release/wait cycles.
		cancel()
		err := <-result
		coordinatorRefusal(t, nil, err, context.Canceled, repos[0])
		must(t, <-released)
		if len(lease.RepositoryBindings()) != 0 {
			t.Fatal("released batch published")
		}
	})
}

func testCoordinatorAliases(t *testing.T) {
	root, calls, repos := coordinatorFixture(t)
	alias := filepath.Join(root, "alias")
	must(t, os.Symlink(repos[0], alias))
	// symlink-sensitive .. must not be lexically cleaned before resolution.
	nested := filepath.Join(repos[1], "nested")
	mkdir(t, nested)
	jump := filepath.Join(root, "jump")
	must(t, os.Symlink(nested, jump))
	logicalParent := jump + "/.."
	lease := coordinatorTake(t, root, calls)
	var locked []Identity
	lease.state.calls.flock = func(fd, flags int) error {
		if flags == unix.LOCK_EX|unix.LOCK_NB {
			id, err := statFD(fd)
			if err != nil {
				return err
			}
			locked = append(locked, id)
		}
		return unix.Flock(fd, flags)
	}
	input := []string{logicalParent, alias, repos[0], repos[1]}
	must(t, lease.LockRepositories(t.Context(), input))
	bindings := lease.RepositoryBindings()
	if len(bindings) != 4 || len(locked) != 2 {
		t.Fatalf("bindings/locks=%d/%d", len(bindings), len(locked))
	}
	for i, path := range input {
		if bindings[i].LogicalPath() != path {
			t.Fatal("logical selection lost")
		}
		want := repos[0]
		if i == 0 || i == 3 {
			want = repos[1]
		}
		if bindings[i].PhysicalPath() != want {
			t.Fatalf("wrong physical path: %+v", bindings[i])
		}
	}
	if locked[0] != bindings[1].Identity() || locked[1] != bindings[0].Identity() {
		t.Fatal("not physical byte order")
	}
	bindings[0] = RepositoryBinding{}
	if lease.RepositoryBindings()[0].LogicalPath() != logicalParent {
		t.Fatal("mutable bindings alias")
	}
	must(t, lease.Release())
	t.Run("conflicting-physical-topologies", func(t *testing.T) {
		id := locked[0]
		_, err := orderedRepositories([]RepositoryBinding{
			{logical: alias, physical: repos[0], identity: id},
			{logical: logicalParent, physical: repos[1], identity: id},
		})
		coordinatorRefusal(t, nil, err, nil, repos[1])
		if !strings.Contains(err.Error(), repos[0]) ||
			!strings.Contains(err.Error(), logicalParent) {
			t.Fatal("conflicting topology evidence lost")
		}
	})
}

func testCoordinatorRetarget(t *testing.T) {
	root, calls, repos := coordinatorFixture(t)
	alias := filepath.Join(root, "alias")
	must(t, os.Symlink(repos[0], alias))
	// Replacement prepared before baselines; rename keeps the entry count stable.
	replacement := filepath.Join(repos[1], "replacement")
	must(t, os.Symlink(repos[1], replacement))
	lease := coordinatorTake(t, root, calls)
	hits := 0
	lease.state.calls.flock = func(fd, flags int) error {
		err := unix.Flock(fd, flags)
		if flags == unix.LOCK_EX|unix.LOCK_NB && err == nil {
			hits++
			if hits == 1 {
				return os.Rename(replacement, alias)
			}
		}
		return err
	}
	err := lease.LockRepositories(t.Context(), []string{alias})
	_ = lease.Release()
	coordinatorRefusal(t, nil, err, nil, alias)
	if hits != 1 || !strings.Contains(err.Error(), repos[0]) ||
		!strings.Contains(err.Error(), repos[1]) {
		t.Fatalf("alias phase/evidence: hits=%d %v", hits, err)
	}
	if len(lease.RepositoryBindings()) != 0 {
		t.Fatal("replacement published")
	}
}

func testCoordinatorDrift(t *testing.T) {
	for _, target := range []string{"earlier-repository", "repository-ancestor", "final-user-file", "final-anchor", "final-root", "empty-anchor"} {
		t.Run(target, func(t *testing.T) {
			root, calls, repos := coordinatorFixture(t)
			lease := coordinatorTake(t, root, calls)
			path := repos[0]
			switch target {
			case "repository-ancestor", "final-root":
				path = root
			case "final-user-file":
				path = filepath.Join(root, "dotty-"+strconv.Itoa(os.Geteuid()), "mutation.lock")
			case "final-anchor", "empty-anchor":
				path = filepath.Join(root, "dotty-"+strconv.Itoa(os.Geteuid()))
			}
			hits, locks := 0, 0
			alter := func() error { hits++; return os.Chmod(path, 0o500) }
			lease.state.calls.flock = func(fd, flags int) error {
				err := unix.Flock(fd, flags)
				if err == nil && flags == unix.LOCK_EX|unix.LOCK_NB {
					locks++
					if locks == 2 && target != "final-root" {
						return alter()
					}
				}
				return err
			}
			observe := lease.state.calls.private.observe
			lease.state.calls.private.observe = func(nodes []pinnedDir, role AuthorityRole, readers authorityCalls) ([]AuthorityFacts, error) {
				if target == "final-root" && locks == 2 && hits == 0 && role == RolePrivateAnchor {
					// This is the coordinator's final user check, AFTER the last
					// directory flock has finished its own post-wait validation.
					if err := alter(); err != nil {
						return nil, err
					}
				}
				return observe(nodes, role, readers)
			}
			if target == "empty-anchor" {
				must(t, alter())
				repos = nil
			}
			err := lease.LockRepositories(t.Context(), repos)
			_ = lease.Release()
			coordinatorRefusal(t, nil, err, nil, path)
			if hits != 1 || (target != "empty-anchor" && locks != 2) {
				t.Fatalf("final drift not reached: hits=%d locks=%d", hits, locks)
			}
			var authority *AuthorityError
			if !errors.As(err, &authority) || authority.Path != path ||
				authority.Observed.Mode() != 0o500 {
				t.Fatalf("wrong drift: %v", err)
			}
			mode := os.FileMode(0o700)
			if target == "final-user-file" {
				mode = 0o600
			}
			must(t, os.Chmod(path, mode))
		})
	}
}

func testCoordinatorCollision(t *testing.T) {
	path := "/private-fixture/anchor"
	name := Component{name: "anchor"}
	exact := nativeError("create-private-directory", path, name, unix.EEXIST)
	for _, test := range []struct {
		name   string
		record CreationRecord
		err    error
		ok     bool
	}{
		{"exact", CreationRecord{}, exact, true},
		{"joined", CreationRecord{}, errors.Join(exact, unix.EIO), false},
		{"wrapped", CreationRecord{}, &CreationError{err: exact}, false},
		{"nested-errno", CreationRecord{}, nativeError("create-private-directory", path, name, errors.Join(unix.EEXIST, unix.EIO)), false},
		{"path", CreationRecord{path: path}, exact, false},
		{"kind", CreationRecord{kind: unix.S_IFDIR}, exact, false},
		{"observations", CreationRecord{observations: []CreationObservation{}}, exact, false},
		{"created", CreationRecord{created: true}, exact, false},
		{"wrong-op", CreationRecord{}, nativeError("observe-private-directory", path, name, unix.EEXIST), false},
		{"wrong-path", CreationRecord{}, nativeError("create-private-directory", path+"x", name, unix.EEXIST), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := exactCreationCollision(
				test.err,
				test.record,
				"create-private-directory",
				path,
				name,
			); got != test.ok {
				t.Fatalf("collision=%t want=%t", got, test.ok)
			}
		})
	}
	root, calls, _ := coordinatorFixture(t)
	first := coordinatorTake(t, root, calls)
	must(t, first.Release())
	opens, creates := 0, 0
	open := calls.private.acquisition.openat
	calls.private.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
		if name == "mutation.lock" {
			if flags&unix.O_EXCL != 0 {
				creates++
			} else {
				opens++
			}
		}
		return open(fd, name, flags, mode)
	}
	second := coordinatorTake(t, root, calls)
	must(t, second.Release())
	if creates != 1 || opens != 2 || len(second.CreationRecords()) != 0 {
		t.Fatalf(
			"separate create/open/lock=%d/%d records=%v",
			creates,
			opens,
			second.CreationRecords(),
		)
	}
	for _, directory := range []bool{false, true} {
		t.Run("disappearance-"+strconv.FormatBool(directory), func(t *testing.T) {
			root, calls, _ := coordinatorFixture(t)
			first := coordinatorTake(t, root, calls)
			must(t, first.Release())
			anchor := filepath.Join(root, userAnchorName().name)
			path := filepath.Join(anchor, "mutation.lock")
			hits := 0
			mkdir := calls.private.mkdir
			calls.private.mkdir = func(fd int, name string, mode uint32) error {
				err := mkdir(fd, name, mode)
				if directory && errors.Is(err, unix.EEXIST) {
					hits++
					if err := os.Remove(path); err != nil {
						return err
					}
					if err := os.Remove(anchor); err != nil {
						return err
					}
				}
				return err
			}
			open := calls.private.acquisition.openat
			calls.private.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
				out, err := open(fd, name, flags, mode)
				if !directory && flags&unix.O_EXCL != 0 && errors.Is(err, unix.EEXIST) {
					hits++
					if err := os.Remove(path); err != nil {
						return out, err
					}
				}
				return out, err
			}
			lease, err := acquireUserAt(t.Context(), root, calls)
			if directory {
				path = anchor
			}
			coordinatorRefusal(t, lease, err, unix.ENOENT, path)
			if hits != 1 || reason(t, err, ReasonMissing).Path != path {
				t.Fatalf("disappearance phase=%d: %v", hits, err)
			}
			if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("disappeared anchor recreated: %v", err)
			}
		})
	}
}

func testCoordinatorRecords(t *testing.T) {
	for _, phase := range []string{"file-create", "flock", "batch"} {
		t.Run(phase, func(t *testing.T) {
			root, calls, repos := coordinatorFixture(t)
			cause := errors.New("injected " + phase)
			hits := 0
			open := calls.private.acquisition.openat
			calls.private.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
				if phase == "file-create" && name == "mutation.lock" && flags&unix.O_EXCL != 0 {
					hits++
					return -1, cause
				}
				return open(fd, name, flags, mode)
			}
			calls.flock = func(fd, flags int) error {
				if phase == "flock" && flags != unix.LOCK_UN {
					hits++
					return cause
				}
				return unix.Flock(fd, flags)
			}
			lease, err := acquireUserAt(t.Context(), root, calls)
			if phase == "batch" {
				if lease != nil {
					owned := lease
					defer func() { _ = owned.Release() }()
				}
				must(t, err)
				if lease == nil {
					t.Fatal("missing user lease")
				}
				lease.state.calls.flock = func(int, int) error { hits++; return cause }
				err = lease.LockRepositories(t.Context(), repos)
				_ = lease.Release()
				lease = nil
			}
			coordinatorRefusal(t, lease, err, cause, root)
			var report *CoordinatorError
			if !errors.As(err, &report) {
				t.Fatal(err)
			}
			want := 2
			if phase == "file-create" {
				want = 1
			}
			records := report.CreationRecords()
			if hits != 1 || len(records) != want {
				t.Fatalf("hits=%d records=%+v", hits, records)
			}
			for _, r := range records {
				if !r.Created() || !strings.HasPrefix(r.Path(), root+"/") ||
					len(r.Observations()) == 0 {
					t.Fatal("creation evidence lost")
				}
			}
			records[0] = CreationRecord{}
			if !report.CreationRecords()[0].Created() {
				t.Fatal("mutable record slice")
			}
		})
	}
}

func testCoordinatorCancel(t *testing.T) {
	// Defined before the cancellation-checkpoint production edit; no red run
	// is claimed. Cancel at preparation, not at the existing second-flock hook.
	for _, concurrentRelease := range []bool{false, true} {
		t.Run("first-authority-release-"+strconv.FormatBool(concurrentRelease), func(t *testing.T) {
			root, calls, repos := coordinatorFixture(t)
			alias := filepath.Join(root, "first-repository")
			must(t, os.Symlink(repos[0], alias))
			var closed, expected []int
			tracking := false
			calls.private.acquisition.close = func(fd int) error {
				if tracking {
					closed = append(closed, fd)
				}
				return unix.Close(fd)
			}
			lease := coordinatorTake(t, root, calls)
			records := lease.CreationRecords()
			tracking = true // Bootstrap already closed; later FD reuse is a new lifetime.
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			waitCtx, stop := flockContext(t)
			defer stop()
			released := make(chan error, 1)
			hits, laterOpens, locks, unlocks := 0, 0, 0, 0
			observedPath := ""
			var unlocked Identity
			open := lease.state.calls.private.acquisition.openat
			lease.state.calls.private.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
				if hits != 0 {
					laterOpens++
				}
				return open(fd, name, flags, mode)
			}
			lease.state.calls.flock = func(fd, flags int) error {
				if flags != unix.LOCK_UN {
					locks++
				}
				return unix.Flock(fd, flags)
			}
			// The user lease captured the acquisition-time flock callback.
			unlock := lease.state.user.state.unlock
			lease.state.user.state.unlock = func(fd int) error {
				unlocks++
				var err error
				unlocked, err = statFD(fd)
				return errors.Join(err, unlock(fd))
			}
			observe := lease.state.calls.private.observe
			lease.state.calls.private.observe = func(nodes []pinnedDir, role AuthorityRole, readers authorityCalls) ([]AuthorityFacts, error) {
				if role == RoleRepositoryDirectory && hits == 0 {
					hits++
					observedPath = nodes[len(nodes)-1].path
					// Capture all owned descriptors BEFORE cancellation or Release.
					appendDir := func(dir *Dir) {
						for i := len(dir.state.nodes) - 1; i >= 0; i-- {
							expected = append(expected, dir.state.nodes[i].fd)
						}
					}
					for i := len(lease.state.repositories) - 1; i >= 0; i-- {
						appendDir(lease.state.repositories[i].dir)
					}
					expected = append(expected, lease.state.user.state.fd)
					appendDir(lease.state.anchor)
					appendDir(lease.state.root)
					if concurrentRelease {
						go func() { released <- lease.Release() }()
						select {
						case <-lease.state.batchContext.Done():
						case <-waitCtx.Done():
							return nil, waitCtx.Err()
						}
					} else {
						cancel()
					}
				}
				return observe(nodes, role, readers)
			}
			err := lease.LockRepositories(ctx, []string{alias, repos[1]})
			if concurrentRelease && hits != 0 {
				must(t, awaitFlock(t, waitCtx, released))
			}
			must(
				t,
				lease.Release(),
			) // Also release an unexpectedly published batch before assertions.
			coordinatorRefusal(t, nil, err, context.Canceled, alias)
			if hits != 1 || observedPath != repos[0] || laterOpens != 0 || locks != 0 ||
				len(lease.state.repositories) != 1 {
				t.Fatalf(
					"preparation path=%q hits/later opens/flocks/pins=%d/%d/%d/%d",
					observedPath,
					hits,
					laterOpens,
					locks,
					len(lease.state.repositories),
				)
			}
			if len(lease.RepositoryBindings()) != 0 {
				t.Fatal("canceled preparation published")
			}
			seen := map[int]bool{}
			for _, fd := range expected {
				if seen[fd] {
					t.Fatalf("multiply owned descriptor %d", fd)
				}
				seen[fd] = true
			}
			if !reflect.DeepEqual(closed, expected) {
				t.Fatalf("owned close sequence=%v want=%v", closed, expected)
			}
			if unlocks != 1 || unlocked != lease.state.userFacts.Identity() {
				t.Fatalf("user unlocks=%d identity=%v", unlocks, unlocked)
			}
			var report *CoordinatorError
			if len(records) != 2 || !errors.As(err, &report) ||
				!reflect.DeepEqual(records, report.CreationRecords()) ||
				!reflect.DeepEqual(records, lease.CreationRecords()) {
				t.Fatal("cancellation lost creation records")
			}
			if report.LogicalPath() != alias || report.PhysicalPath() != repos[0] {
				t.Fatalf("cancellation paths lost: %v", err)
			}
			if err := lease.LockRepositories(t.Context(), nil); err == nil {
				_ = lease.Release()
				t.Fatal("canceled batch was not terminal")
			}
		})
	}
	root, calls, repos := coordinatorFixture(t)
	lease := coordinatorTake(t, root, calls)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var locks, unlocks []string
	lease.state.calls.flock = func(fd, flags int) error {
		id, err := statFD(fd)
		if err != nil {
			return err
		}
		path := ""
		for _, repo := range lease.state.repositories {
			if repo.dir.state.leaf().identity == id {
				path = repo.binding.physical
			}
		}
		if flags == unix.LOCK_UN {
			unlocks = append(unlocks, path)
			return unix.Flock(fd, flags)
		}
		locks = append(locks, path)
		err = unix.Flock(fd, flags)
		if len(locks) == 2 {
			cancel()
		}
		return err
	}
	err := lease.LockRepositories(ctx, repos)
	_ = lease.Release()
	coordinatorRefusal(t, nil, err, context.Canceled, repos[1])
	if !reflect.DeepEqual(locks, repos) ||
		!reflect.DeepEqual(unlocks, []string{repos[1], repos[0]}) {
		t.Fatalf("lock/unlock order=%v/%v", locks, unlocks)
	}
	if len(lease.RepositoryBindings()) != 0 {
		t.Fatal("canceled batch published")
	}
	must(t, lease.Release())
}

func testCoordinatorRelease(t *testing.T) {
	root, calls, repos := coordinatorFixture(t)
	unlockCause, closeCause := errors.New("coordinator unlock"), errors.New("coordinator close")
	var mu sync.Mutex
	var unlocks []Identity
	closing := false
	// Track acquisition lifetimes, not the set of observed cleanup calls. The
	// direct / opens appear as previously unseen openat parents. Removing the
	// bootstrap lifetime here allows its FD number to be reused by later pins.
	parents := map[int]int{}
	bootstrap := -1
	var bootstrapCloses []int
	type cleanupEvent struct {
		op string
		fd int
	}
	var actual, expected []cleanupEvent
	open := calls.private.acquisition.openat
	calls.private.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
		if _, ok := parents[fd]; !ok {
			parents[fd] = -1
		}
		out, err := open(fd, name, flags, mode)
		if err == nil {
			if _, exists := parents[out]; exists {
				t.Errorf("overlapping descriptor lifetime: %d", out)
			}
			parents[out] = fd
			if name == "mutation.lock" && flags&unix.O_EXCL != 0 {
				bootstrap = out
			}
		}
		return out, err
	}
	calls.flock = func(fd, flags int) error {
		err := unix.Flock(fd, flags)
		if flags == unix.LOCK_UN {
			id, statErr := statFD(fd)
			mu.Lock()
			unlocks = append(unlocks, id)
			actual = append(actual, cleanupEvent{"unlock", fd})
			mu.Unlock()
			return errors.Join(err, statErr, unlockCause)
		}
		return err
	}
	calls.private.acquisition.close = func(fd int) error {
		err := unix.Close(fd)
		mu.Lock()
		delete(parents, fd)
		if closing {
			actual = append(actual, cleanupEvent{"close", fd})
		} else {
			bootstrapCloses = append(bootstrapCloses, fd)
		}
		mu.Unlock()
		if closing {
			return errors.Join(err, closeCause)
		}
		return err
	}
	lease := coordinatorTake(t, root, calls)
	must(t, lease.LockRepositories(t.Context(), repos))
	bindings := lease.RepositoryBindings()
	records := lease.CreationRecords()
	if bootstrap < 0 || !reflect.DeepEqual(bootstrapCloses, []int{bootstrap}) || len(actual) != 0 {
		t.Fatal("unexpected pre-release cleanup")
	}
	expectedFDs := map[int]bool{}
	closeFD := func(fd int) {
		if _, ok := parents[fd]; !ok || expectedFDs[fd] {
			t.Fatalf("missing or multiply owned descriptor %d", fd)
		}
		expectedFDs[fd] = true
		expected = append(expected, cleanupEvent{"close", fd})
	}
	closeDir := func(dir *Dir) {
		for i := len(dir.state.nodes) - 1; i >= 0; i-- {
			closeFD(dir.state.nodes[i].fd)
		}
	}
	// Reconstruct each independent directory lease's ancestry from open edges,
	// not from its opaque close callback or from any observed closes.
	for i := len(lease.state.repositories) - 1; i >= 0; i-- {
		fd := lease.state.repositories[i].lease.state.fd
		expected = append(expected, cleanupEvent{"unlock", fd})
		for fd != -1 {
			parent := parents[fd]
			closeFD(fd)
			fd = parent
		}
	}
	for i := len(lease.state.repositories) - 1; i >= 0; i-- {
		closeDir(lease.state.repositories[i].dir)
	}
	userFD := lease.state.user.state.fd
	expected = append(expected, cleanupEvent{"unlock", userFD})
	closeFD(userFD)
	closeDir(lease.state.anchor)
	closeDir(lease.state.root)
	if len(expectedFDs) != len(parents) {
		t.Fatalf("owned inventory incomplete: expected=%v acquired=%v", expectedFDs, parents)
	}
	closing = true
	copy := *lease
	results := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Go(func() { results <- copy.Release() })
	}
	wg.Wait()
	close(results)
	var first error
	for err := range results {
		if !errors.Is(err, unlockCause) || !errors.Is(err, closeCause) {
			t.Fatalf("lost release causes: %v", err)
		}
		if first == nil {
			first = err
		}
		if !reflect.DeepEqual(err, first) {
			t.Fatal("different shared release result")
		}
		var report *CoordinatorError
		if len(records) != 2 || !errors.As(err, &report) ||
			!reflect.DeepEqual(report.CreationRecords(), records) ||
			!reflect.DeepEqual(lease.CreationRecords(), records) {
			t.Fatal("release records lost")
		}
		for _, path := range append(repos, filepath.Join(root, "dotty-"+strconv.Itoa(os.Geteuid()), "mutation.lock")) {
			if !strings.Contains(err.Error(), path) {
				t.Fatalf("release path lost: %s", path)
			}
		}
	}
	if len(unlocks) != 3 || unlocks[0] != bindings[1].Identity() ||
		unlocks[1] != bindings[0].Identity() ||
		unlocks[2] != lease.state.userFacts.Identity() {
		t.Fatalf("reverse releases/final user unlock=%v", unlocks)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("complete cleanup sequence=%v want=%v", actual, expected)
	}
	if len(parents) != 0 {
		t.Fatalf("owned descriptors not closed: %v", parents)
	}
}

func testCoordinatorDescriptions(t *testing.T) {
	root, calls, _ := coordinatorFixture(t)
	bootstrap, locked := -1, -1
	open := calls.private.acquisition.openat
	calls.private.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
		out, err := open(fd, name, flags, mode)
		if name == "mutation.lock" && flags&unix.O_EXCL != 0 && err == nil {
			bootstrap = out
		}
		return out, err
	}
	calls.flock = func(fd, flags int) error {
		if flags != unix.LOCK_UN {
			locked = fd
			if bootstrap < 0 || fd == bootstrap {
				return errors.New("shared bootstrap description")
			}
			if _, err := statFD(bootstrap); err != nil {
				return err
			}
			// Independent descriptions contend even inside this same process.
			if err := unix.Flock(bootstrap, unix.LOCK_EX|unix.LOCK_NB); err != nil {
				return err
			}
			err := unix.Flock(fd, flags)
			_ = unix.Flock(bootstrap, unix.LOCK_UN)
			if !errors.Is(err, unix.EWOULDBLOCK) {
				return errors.New("bootstrap did not exclude independent description")
			}
		}
		return unix.Flock(fd, flags)
	}
	lease := coordinatorTake(t, root, calls)
	if bootstrap < 0 || locked < 0 || bootstrap == locked {
		t.Fatal("description test not reached")
	}
	if _, err := statFD(bootstrap); !errors.Is(err, unix.EBADF) {
		t.Fatalf("bootstrap not closed: %v", err)
	}
	must(t, lease.Release())
}

func testCoordinatorRetained(t *testing.T) {
	root, calls, _ := coordinatorFixture(t)
	lease := coordinatorTake(t, root, calls)
	records := lease.CreationRecords()
	must(t, lease.Release())
	for _, r := range records {
		if _, err := os.Lstat(r.Path()); err != nil {
			t.Fatalf("anchor removed: %s: %v", r.Path(), err)
		}
	}
	if !reflect.DeepEqual(records, lease.CreationRecords()) {
		t.Fatal("release lost records")
	}
}

func testCoordinatorBootstrap(t *testing.T) {
	for _, phase := range []string{"replacement", "close-failure"} {
		t.Run(phase, func(t *testing.T) {
			root, calls, repos := coordinatorFixture(t)
			first := coordinatorTake(t, root, calls)
			must(t, first.Release())
			path := filepath.Join(root, "dotty-"+strconv.Itoa(os.Geteuid()), "mutation.lock")
			replacement := filepath.Join(repos[0], "prepared")
			write(t, replacement, "replacement")
			bootstrap, reads, observations, hits, locks, closes := -1, 0, 0, 0, 0, 0
			cause := errors.New("bootstrap close failure")
			open := calls.private.acquisition.openat
			calls.private.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
				out, err := open(fd, name, flags, mode)
				if name == "mutation.lock" && err == nil && bootstrap < 0 {
					bootstrap = out
				}
				return out, err
			}
			security := calls.private.authority.security
			calls.private.authority.security = func(fd int, directory bool) (SecurityEvidence, error) {
				if fd == bootstrap && !directory {
					reads++
				}
				return security(fd, directory)
			}
			observe := calls.private.observe
			calls.private.observe = func(nodes []pinnedDir, role AuthorityRole, readers authorityCalls) ([]AuthorityFacts, error) {
				if reads == 2 {
					observations++
					// Two post-bootstrap baseline observations precede the fresh
					// flock's own baseline. Replace before that independent baseline.
					if observations == 3 && phase == "replacement" {
						hits++
						if err := os.Rename(replacement, path); err != nil {
							return nil, err
						}
					}
				}
				return observe(nodes, role, readers)
			}
			calls.flock = func(fd, flags int) error {
				if flags != unix.LOCK_UN {
					locks++
				}
				return unix.Flock(fd, flags)
			}
			calls.private.acquisition.close = func(fd int) error {
				err := unix.Close(fd)
				if fd == bootstrap {
					closes++
					if phase == "close-failure" {
						hits++
						return errors.Join(err, cause)
					}
				}
				return err
			}
			lease, err := acquireUserAt(t.Context(), root, calls)
			wantCause := error(nil)
			if phase == "close-failure" {
				wantCause = cause
			}
			coordinatorRefusal(t, lease, err, wantCause, path)
			if hits != 1 || locks != 1 || closes != 1 {
				t.Fatalf("bootstrap phase hits/locks/closes=%d/%d/%d: %v", hits, locks, closes, err)
			}
			if phase == "replacement" {
				e := reason(t, err, ReasonIdentityChanged)
				if e.Path != path || !e.Expected.Valid || !e.Observed.Valid ||
					e.Expected == e.Observed {
					t.Fatalf("bootstrap binding diagnostic=%v", err)
				}
			}
		})
	}
}

func testCoordinatorMissing(t *testing.T) {
	for _, variant := range []string{"missing", "dangling", "file", "relative", "nul"} {
		t.Run(variant, func(t *testing.T) {
			root, calls, _ := coordinatorFixture(t)
			path := filepath.Join(root, variant)
			switch variant {
			case "dangling":
				must(t, os.Symlink(filepath.Join(root, "absent"), path))
			case "file":
				write(t, path, "not directory")
			case "relative":
				path = "relative"
			case "nul":
				path += "\x00"
			}
			lease := coordinatorTake(t, root, calls)
			locks := 0
			lease.state.calls.flock = func(int, int) error { locks++; return errors.New("unexpected repository flock") }
			err := lease.LockRepositories(t.Context(), []string{path})
			_ = lease.Release()
			// Diagnostic quotes NUL rather than emitting it literally.
			var report *CoordinatorError
			if !errors.As(err, &report) || report.LogicalPath() != path || locks != 0 {
				t.Fatalf("selection widened or lost: locks=%d %v", locks, err)
			}
			if err := lease.LockRepositories(t.Context(), nil); err == nil {
				t.Fatal("failed batch was not terminal")
			}
			must(t, lease.Release())
		})
	}
}
