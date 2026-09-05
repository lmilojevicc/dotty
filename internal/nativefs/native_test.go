//go:build linux || darwin

package nativefs

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// This inventory is deliberately independent of the implementation map. Run the
// whole aggregator: filtering out or skipping a required case is a failure.
var requiredNativeCases = []string{
	"root",
	"normal",
	"missing_ancestry",
	"nondirectory_ancestry",
	"ancestor_symlink",
	"final_directory_symlink",
	"physical_path_syntax",
	"leaf_symlink",
	"invalid_components",
	"zero_identity",
	"missing_source",
	"absent_destination",
	"absent_directory_destination",
	"existing_file",
	"existing_directory",
	"existing_dangling_symlink",
	"source_swap",
	"source_parent_swap",
	"destination_parent_swap",
	"ancestor_swap",
	"directory_into_self",
	"directory_into_descendant",
	"closed",
	"concurrent_close",
	"active_lease",
	"copied_wrapper_double_close",
	"owned_api_reuse",
	"two_handle_acquisition",
	"capability_ENOSYS",
	"capability_ENOTSUP",
	"capability_EOPNOTSUPP",
	"context_EINVAL",
	"EXDEV",
	"acquisition_failure_cleanup",
	"acquisition_capabilities",
	"acquisition_identity_swap",
	"close_error_no_retry",
	"fd_zero",
	"no_unsafe_fallback",
	"private_api",
}

func TestNativeHandleBoundary(t *testing.T) {
	cases := map[string]func(*testing.T){
		"root":                         testRoot,
		"normal":                       testNormal,
		"missing_ancestry":             func(t *testing.T) { testBadAncestry(t, "missing") },
		"nondirectory_ancestry":        func(t *testing.T) { testBadAncestry(t, "file") },
		"ancestor_symlink":             func(t *testing.T) { testBadAncestry(t, "ancestor_link") },
		"final_directory_symlink":      func(t *testing.T) { testBadAncestry(t, "final_link") },
		"physical_path_syntax":         testPhysicalPathSyntax,
		"leaf_symlink":                 testLeafSymlink,
		"invalid_components":           testInvalidComponents,
		"zero_identity":                testZeroIdentity,
		"missing_source":               testMissingSource,
		"absent_destination":           testAbsentDestination,
		"absent_directory_destination": testAbsentDirectoryDestination,
		"existing_file":                func(t *testing.T) { testExistingDestination(t, "file") },
		"existing_directory":           func(t *testing.T) { testExistingDestination(t, "directory") },
		"existing_dangling_symlink":    func(t *testing.T) { testExistingDestination(t, "symlink") },
		"source_swap":                  func(t *testing.T) { testSwap(t, "source") },
		"source_parent_swap":           func(t *testing.T) { testSwap(t, "source_parent") },
		"destination_parent_swap":      func(t *testing.T) { testSwap(t, "destination_parent") },
		"ancestor_swap":                func(t *testing.T) { testSwap(t, "ancestor") },
		"directory_into_self":          func(t *testing.T) { testDirectoryTopology(t, false) },
		"directory_into_descendant":    func(t *testing.T) { testDirectoryTopology(t, true) },
		"closed":                       testClosed,
		"concurrent_close":             testConcurrentClose,
		"active_lease":                 testActiveLease,
		"copied_wrapper_double_close":  testCopiedWrapper,
		"owned_api_reuse":              testOwnedAPIReuse,
		"two_handle_acquisition":       testTwoHandleAcquisition,
		"capability_ENOSYS":            func(t *testing.T) { testNativeError(t, unix.ENOSYS, ReasonUnsupported) },
		"capability_ENOTSUP":           func(t *testing.T) { testNativeError(t, unix.ENOTSUP, ReasonUnsupported) },
		"capability_EOPNOTSUPP":        func(t *testing.T) { testNativeError(t, unix.EOPNOTSUPP, ReasonUnsupported) },
		"context_EINVAL":               func(t *testing.T) { testNativeError(t, unix.EINVAL, ReasonInvalidOperation) },
		"EXDEV":                        func(t *testing.T) { testNativeError(t, unix.EXDEV, ReasonCrossDevice) },
		"acquisition_failure_cleanup":  testAcquisitionFailureCleanup,
		"acquisition_capabilities":     testAcquisitionCapabilities,
		"acquisition_identity_swap":    testAcquisitionIdentitySwap,
		"close_error_no_retry":         testCloseError,
		"fd_zero":                      testFDZero,
		"no_unsafe_fallback":           testNoUnsafeFallback,
		"private_api":                  testPrivateAPI,
	}
	if len(cases) != len(requiredNativeCases) {
		t.Fatalf(
			"case inventory mismatch: %d implementations, %d required",
			len(cases),
			len(requiredNativeCases),
		)
	}
	seen := map[string]bool{}
	for _, name := range requiredNativeCases {
		fn, ok := cases[name]
		if !ok || seen[name] {
			t.Fatalf("missing or duplicate required case %q", name)
		}
		seen[name] = true
		executed := false
		t.Run(name, func(t *testing.T) {
			t.Cleanup(func() {
				if t.Skipped() {
					t.Errorf("required native case skipped: %s", name)
				}
			})
			fn(t)
			executed = true
		})
		if !executed {
			t.Errorf("required native case did not complete: %s", name)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	must(t, err)
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("DOTTY_REPO", filepath.Join(root, "repo"))
	return root
}

func mkdir(t *testing.T, path string) { t.Helper(); must(t, os.MkdirAll(path, 0o700)) }
func write(t *testing.T, path, value string) {
	t.Helper()
	must(t, os.WriteFile(path, []byte(value), 0o600))
}

func component(t *testing.T, name string) Component {
	t.Helper()
	c, err := ParseComponent(name)
	must(t, err)
	return c
}

func openDir(t *testing.T, path string) *Dir {
	t.Helper()
	d, err := OpenPhysicalDir(path)
	must(t, err)
	t.Cleanup(func() { must(t, d.Close()) })
	return d
}

func observe(t *testing.T, d *Dir, name string) Identity {
	t.Helper()
	id, err := d.Observe(component(t, name))
	must(t, err)
	return id
}

func reason(t *testing.T, err error, want Reason) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Reason != want {
		t.Fatalf("want reason %s, got %v", want, err)
	}
	return e
}

func absent(t *testing.T, path string) {
	t.Helper()
	_, err := os.Lstat(path)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected absent %q: %v", path, err)
	}
}

func content(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	must(t, err)
	if string(b) != want {
		t.Fatalf("%q content = %q, want %q", path, b, want)
	}
}

func testRoot(t *testing.T) {
	d := openDir(t, "/") // Read-only pinning; no fixture writes to the OS root.
	id, err := d.Identity()
	must(t, err)
	if !id.Valid || id.Kind != unix.S_IFDIR {
		t.Fatalf("invalid root identity: %+v", id)
	}
}

func testNormal(t *testing.T) {
	r := fixture(t)
	mkdir(t, filepath.Join(r, "a", "b"))
	d := openDir(t, filepath.Join(r, "a", "b"))
	id, err := d.Identity()
	must(t, err)
	parent := openDir(t, filepath.Join(r, "a"))
	if id != observe(t, parent, "b") {
		t.Fatal("parent/name and opened identity disagree")
	}
	for _, n := range d.state.nodes {
		flags, err := unix.FcntlInt(uintptr(n.fd), unix.F_GETFD, 0)
		must(t, err)
		if flags&unix.FD_CLOEXEC == 0 {
			t.Fatal("descriptor lacks CLOEXEC")
		}
	}
}

func testBadAncestry(t *testing.T, kind string) {
	r := fixture(t)
	mkdir(t, filepath.Join(r, "real", "child"))
	path := filepath.Join(r, "bad", "child")
	switch kind {
	case "file":
		write(t, filepath.Join(r, "bad"), "untouched")
	case "ancestor_link":
		must(t, os.Symlink("real", filepath.Join(r, "bad")))
	case "final_link":
		must(t, os.Symlink("real", filepath.Join(r, "bad")))
		path = filepath.Join(r, "bad")
	}
	d, err := OpenPhysicalDir(path)
	if err == nil || d != nil {
		t.Fatalf("opened unsafe ancestry %q", path)
	}
	entries, err := os.ReadDir(filepath.Join(r, "real", "child"))
	must(t, err)
	if len(entries) != 0 {
		t.Fatal("acquisition wrote content")
	}
}

func testPhysicalPathSyntax(t *testing.T) {
	r := fixture(t)
	for _, path := range []string{"", "relative", r + "/", r + "//x", r + "/.", r + "/..", r + "/a\x00b"} {
		d, err := OpenPhysicalDir(path)
		if err == nil || d != nil {
			t.Fatalf("accepted malformed path %q", path)
		}
	}
}

func testLeafSymlink(t *testing.T) {
	r := fixture(t)
	write(t, filepath.Join(r, "referent"), "keep")
	must(t, os.Symlink("referent", filepath.Join(r, "source")))
	d := openDir(t, r)
	id := observe(t, d, "source")
	if id.Kind != unix.S_IFLNK {
		t.Fatalf("followed leaf: %+v", id)
	}
	must(t, RenameNoReplace(d, component(t, "source"), id, d, component(t, "target")))
	text, err := os.Readlink(filepath.Join(r, "target"))
	must(t, err)
	if text != "referent" {
		t.Fatalf("symlink text = %q", text)
	}
	content(t, filepath.Join(r, "referent"), "keep")
	absent(t, filepath.Join(r, "source"))
}

func testInvalidComponents(t *testing.T) {
	r := fixture(t)
	write(t, filepath.Join(r, "source"), "keep")
	d := openDir(t, r)
	id := observe(t, d, "source")
	for _, text := range []string{"", ".", "..", "a/b", "/a", "a/", "a\x00b"} {
		_, err := ParseComponent(text)
		_ = reason(t, err, ReasonInvalidComponent)
		bad := Component{name: text} // Includes the externally constructible zero value.
		_, err = d.Observe(bad)
		_ = reason(t, err, ReasonInvalidComponent)
		_ = reason(
			t,
			RenameNoReplace(d, bad, id, d, component(t, "target")),
			ReasonInvalidComponent,
		)
		_ = reason(
			t,
			RenameNoReplace(d, component(t, "source"), id, d, bad),
			ReasonInvalidComponent,
		)
	}
	write(t, filepath.Join(r, `a\b`), "backslash")
	observe(t, d, `a\b`)
	content(t, filepath.Join(r, "source"), "keep")
	absent(t, filepath.Join(r, "target"))
}

func testZeroIdentity(t *testing.T) {
	r := fixture(t)
	write(t, filepath.Join(r, "source"), "keep")
	d := openDir(t, r)
	_ = reason(
		t,
		RenameNoReplace(d, component(t, "source"), Identity{}, d, component(t, "target")),
		ReasonInvalidIdentity,
	)
	// Numeric zero is not the validity sentinel.
	zero := Identity{Valid: true}
	_ = reason(
		t,
		RenameNoReplace(d, component(t, "source"), zero, d, component(t, "target")),
		ReasonIdentityChanged,
	)
	content(t, filepath.Join(r, "source"), "keep")
}

func testMissingSource(t *testing.T) {
	r := fixture(t)
	write(t, filepath.Join(r, "source"), "keep")
	d := openDir(t, r)
	id := observe(t, d, "source")
	must(t, os.Remove(filepath.Join(r, "source")))
	err := RenameNoReplace(d, component(t, "source"), id, d, component(t, "target"))
	_ = reason(t, err, ReasonMissing)
	if !errors.Is(err, unix.ENOENT) {
		t.Fatal("lost ENOENT")
	}
	absent(t, filepath.Join(r, "target"))
}

func testAbsentDestination(t *testing.T) {
	r := fixture(t)
	mkdir(t, filepath.Join(r, "dst"))
	write(t, filepath.Join(r, "source"), "move")
	s, d := openDir(t, r), openDir(t, filepath.Join(r, "dst"))
	id := observe(t, s, "source")
	must(t, RenameNoReplace(s, component(t, "source"), id, d, component(t, "target")))
	if got := observe(t, d, "target"); got != id {
		t.Fatalf("identity changed: %+v -> %+v", id, got)
	}
	content(t, filepath.Join(r, "dst", "target"), "move")
	absent(t, filepath.Join(r, "source"))
}

func testAbsentDirectoryDestination(t *testing.T) {
	r := fixture(t)
	mkdir(t, filepath.Join(r, "source"))
	write(t, filepath.Join(r, "source", "child"), "keep")
	d := openDir(t, r)
	id := observe(t, d, "source")
	must(t, RenameNoReplace(d, component(t, "source"), id, d, component(t, "target")))
	if observe(t, d, "target") != id {
		t.Fatal("directory identity changed")
	}
	content(t, filepath.Join(r, "target", "child"), "keep")
	absent(t, filepath.Join(r, "source"))
}

func testExistingDestination(t *testing.T, kind string) {
	r := fixture(t)
	write(t, filepath.Join(r, "source"), "source")
	switch kind {
	case "file":
		write(t, filepath.Join(r, "target"), "target")
	case "directory":
		mkdir(t, filepath.Join(r, "target"))
		write(t, filepath.Join(r, "target", "child"), "target")
	case "symlink":
		must(t, os.Symlink("absent", filepath.Join(r, "target")))
	}
	d := openDir(t, r)
	source, target := observe(t, d, "source"), observe(t, d, "target")
	_ = reason(
		t,
		RenameNoReplace(d, component(t, "source"), source, d, component(t, "target")),
		ReasonExists,
	)
	if observe(t, d, "source") != source || observe(t, d, "target") != target {
		t.Fatal("collision changed identity")
	}
	content(t, filepath.Join(r, "source"), "source")
	switch kind {
	case "file":
		content(t, filepath.Join(r, "target"), "target")
	case "directory":
		content(t, filepath.Join(r, "target", "child"), "target")
	case "symlink":
		text, err := os.Readlink(filepath.Join(r, "target"))
		must(t, err)
		if text != "absent" {
			t.Fatal(text)
		}
	}
}

func testSwap(t *testing.T, kind string) {
	r := fixture(t)
	base := filepath.Join(r, "ancestor")
	mkdir(t, filepath.Join(base, "src"))
	mkdir(t, filepath.Join(base, "dst"))
	write(t, filepath.Join(base, "src", "source"), "original")
	s, d := openDir(t, filepath.Join(base, "src")), openDir(t, filepath.Join(base, "dst"))
	id := observe(t, s, "source")
	path := map[string]string{"source": filepath.Join(base, "src", "source"), "source_parent": filepath.Join(base, "src"), "destination_parent": filepath.Join(base, "dst"), "ancestor": base}[kind]
	must(t, os.Rename(path, path+"-saved")) // Interference BEFORE the final guard.
	if kind == "source" {
		write(t, path, "external")
	} else {
		mkdir(t, path)
	}
	err := RenameNoReplace(s, component(t, "source"), id, d, component(t, "target"))
	e := reason(t, err, ReasonIdentityChanged)
	if e.Path != path || !e.Expected.Valid || !e.Observed.Valid {
		t.Fatalf("incomplete mismatch: %+v", e)
	}
	if kind == "source" {
		content(t, path, "external")
		content(t, path+"-saved", "original")
	}
	if kind == "source_parent" {
		content(t, filepath.Join(path+"-saved", "source"), "original")
	}
	if kind == "ancestor" {
		content(t, filepath.Join(path+"-saved", "src", "source"), "original")
		absent(t, filepath.Join(path+"-saved", "dst", "target"))
	}
	if kind == "destination_parent" {
		absent(t, filepath.Join(path+"-saved", "target"))
	}
	absent(t, filepath.Join(base, "dst", "target"))
}

func testDirectoryTopology(t *testing.T, descendant bool) {
	r := fixture(t)
	mkdir(t, filepath.Join(r, "source", "child"))
	s := openDir(t, r)
	dst := filepath.Join(r, "source")
	if descendant {
		dst = filepath.Join(dst, "child")
	}
	d := openDir(t, dst)
	err := RenameNoReplace(
		s,
		component(t, "source"),
		observe(t, s, "source"),
		d,
		component(t, "target"),
	)
	_ = reason(t, err, ReasonTopology)
	absent(t, filepath.Join(dst, "target"))
}

func testClosed(t *testing.T) {
	d := openDir(t, fixture(t))
	must(t, d.Close())
	_, err := d.Identity()
	_ = reason(t, err, ReasonClosed)
	_, err = d.Observe(component(t, "name"))
	_ = reason(t, err, ReasonClosed)
	_ = reason(
		t,
		RenameNoReplace(
			d,
			component(t, "source"),
			Identity{Valid: true},
			d,
			component(t, "target"),
		),
		ReasonClosed,
	)
	var zero Dir
	_, err = zero.Identity()
	_ = reason(t, err, ReasonClosed)
	var nilDir *Dir
	_, err = nilDir.Identity()
	_ = reason(t, err, ReasonClosed)
}

func testConcurrentClose(t *testing.T) {
	d := openDir(t, fixture(t))
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if err := d.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	_, err := d.Identity()
	_ = reason(t, err, ReasonClosed)
}

func testActiveLease(t *testing.T) {
	d := openDir(t, fixture(t))
	release, err := acquire(d, d)
	must(t, err)
	releaseOnce := sync.OnceFunc(release)
	defer releaseOnce()
	lifecycleMu.Lock()
	active := d.state.active
	lifecycleMu.Unlock()
	if active != 1 {
		t.Fatalf("same handle acquired %d times", active)
	}
	done := make(chan error, 1)
	go func() { done <- d.Close() }()
	deadline := time.After(5 * time.Second)
	for {
		lifecycleMu.Lock()
		closing := d.state.closing
		lifecycleMu.Unlock()
		if closing {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Close did not block new leases")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	_, err = d.Identity()
	_ = reason(t, err, ReasonClosed)
	select {
	case err := <-done:
		t.Fatalf("closed active lease: %v", err)
	default:
	}
	var st unix.Stat_t
	must(t, unix.Fstat(d.state.nodes[0].fd, &st))
	releaseOnce()
	select {
	case err := <-done:
		must(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not finish")
	}
}

func testCopiedWrapper(t *testing.T) {
	d := openDir(t, fixture(t))
	copyDir := *d
	must(t, copyDir.Close())
	must(t, d.Close())
	_, err := copyDir.Identity()
	_ = reason(t, err, ReasonClosed)
}

func testOwnedAPIReuse(t *testing.T) {
	r := fixture(t)
	first := openDir(t, r)
	copyDir := *first
	must(t, first.Close())
	second := openDir(t, r)
	must(t, copyDir.Close())
	_, err := second.Identity()
	must(t, err)
}

func testTwoHandleAcquisition(t *testing.T) {
	r := fixture(t)
	a, b := openDir(t, r), openDir(t, r)
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			release, err := acquire(a, b)
			if err != nil {
				t.Error(err)
				return
			}
			release()
		})
		wg.Go(func() {
			release, err := acquire(b, a)
			if err != nil {
				t.Error(err)
				return
			}
			release()
		})
	}
	wg.Wait()
	must(t, b.Close())
	_, err := acquire(a, b)
	_ = reason(t, err, ReasonClosed)
	lifecycleMu.Lock()
	active := a.state.active
	lifecycleMu.Unlock()
	if active != 0 {
		t.Fatal("failed pair acquisition leaked a lease")
	}
}

func testNativeError(t *testing.T, errno error, want Reason) {
	r := fixture(t)
	write(t, filepath.Join(r, "source"), "keep")
	d := openDir(t, r)
	id := observe(t, d, "source")
	calls := 0
	err := renameNoReplace(
		d,
		component(t, "source"),
		id,
		d,
		component(t, "target"),
		func(sfd int, src string, dfd int, dst string) error {
			calls++
			if sfd != d.state.leaf().fd || dfd != sfd || src != "source" || dst != "target" {
				t.Fatal("wrong native arguments")
			}
			return errno
		},
	)
	e := reason(t, err, want)
	if !errors.Is(err, errno) || calls != 1 || e.Path != filepath.Join(r, "source") ||
		e.DestinationPath != filepath.Join(r, "target") {
		t.Fatalf("lost native context: %+v, calls=%d", e, calls)
	}
	content(t, filepath.Join(r, "source"), "keep")
	absent(t, filepath.Join(r, "target"))
}

func testAcquisitionFailureCleanup(t *testing.T) {
	r := fixture(t)
	mkdir(t, filepath.Join(r, "a", "b"))
	for failAt := 1; failAt <= len(splitPhysicalPathForTest(t, filepath.Join(r, "a", "b"))); failAt++ {
		ops := systemAcquisition()
		originalOpenat, originalClose := ops.openat, ops.close
		opened, closed := map[int]bool{}, map[int]int{}
		count := 0
		ops.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
			count++
			if count == failAt {
				return -1, unix.EACCES
			}
			got, err := originalOpenat(fd, name, flags, mode)
			if err == nil {
				opened[got] = true
			}
			return got, err
		}
		ops.close = func(fd int) error { closed[fd]++; return originalClose(fd) }
		d, err := openPhysicalDir(filepath.Join(r, "a", "b"), ops)
		if d != nil || !errors.Is(err, unix.EACCES) {
			t.Fatalf("bad injected acquisition: %v", err)
		}
		if len(closed) != len(opened)+1 {
			t.Fatalf("root or child leaked: opened=%v closed=%v", opened, closed)
		}
		for fd, n := range closed {
			if n != 1 {
				t.Fatalf("fd %d closed %d times", fd, n)
			}
			var st unix.Stat_t
			if !errors.Is(unix.Fstat(fd, &st), unix.EBADF) {
				t.Fatalf("fd %d still open", fd)
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join(r, "a", "b"))
	must(t, err)
	if len(entries) != 0 {
		t.Fatal("acquisition wrote files")
	}
}

func testAcquisitionCapabilities(t *testing.T) {
	r := fixture(t)
	for _, errno := range []error{unix.ENOSYS, unix.ENOTSUP, unix.EOPNOTSUPP, unix.EINVAL} {
		ops := systemAcquisition()
		closeFD := ops.close
		closed := 0
		ops.close = func(fd int) error { closed++; return closeFD(fd) }
		ops.openat = func(int, string, int, uint32) (int, error) { return -1, errno }
		d, err := openPhysicalDir(r, ops)
		if d != nil || !errors.Is(err, errno) || closed != 1 {
			t.Fatalf("acquisition capability failure: dir=%v err=%v closes=%d", d, err, closed)
		}
		want := ReasonUnsupported
		if errors.Is(errno, unix.EINVAL) {
			want = ReasonInvalidOperation
		}
		_ = reason(t, err, want)
	}
	entries, err := os.ReadDir(r)
	must(t, err)
	if len(entries) != 0 {
		t.Fatal("capability refusal wrote files")
	}
}

func splitPhysicalPathForTest(t *testing.T, path string) []Component {
	t.Helper()
	parts, err := physicalComponents(path)
	must(t, err)
	return parts
}

func testAcquisitionIdentitySwap(t *testing.T) {
	r := fixture(t)
	mkdir(t, filepath.Join(r, "victim"))
	ops := systemAcquisition()
	original := ops.openat
	ops.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
		if name == "victim" {
			must(t, os.Rename(filepath.Join(r, "victim"), filepath.Join(r, "saved")))
			mkdir(t, filepath.Join(r, "victim"))
		}
		return original(fd, name, flags, mode)
	}
	d, err := openPhysicalDir(filepath.Join(r, "victim"), ops)
	if d != nil {
		t.Fatal("opened swapped directory")
	}
	_ = reason(t, err, ReasonIdentityChanged)
}

func testCloseError(t *testing.T) {
	r := fixture(t)
	ops := systemAcquisition()
	closeFD := ops.close
	calls := map[int]int{}
	ops.close = func(fd int) error { calls[fd]++; must(t, closeFD(fd)); return unix.EINTR }
	d, err := openPhysicalDir(r, ops)
	must(t, err)
	copyDir := *d
	if !errors.Is(d.Close(), unix.EINTR) || !errors.Is(copyDir.Close(), unix.EINTR) {
		t.Fatal("lost close error")
	}
	for _, n := range calls {
		if n != 1 {
			t.Fatal("retried a possibly reused descriptor")
		}
	}
	_, err = d.Identity()
	_ = reason(t, err, ReasonClosed)
}

func testFDZero(t *testing.T) {
	// Simulate descriptor zero in the private ownership layer without closing
	// process stdin or otherwise touching an unowned process descriptor.
	closed := []int{}
	d := &Dir{
		state: &dirState{
			nodes:   []pinnedDir{{fd: 0}},
			closeFD: func(fd int) error { closed = append(closed, fd); return nil },
		},
	}
	release, err := acquire(d, d)
	must(t, err)
	release()
	must(t, d.Close())
	must(t, d.Close())
	if !reflect.DeepEqual(closed, []int{0}) {
		t.Fatalf("descriptor zero not owned: %v", closed)
	}
}

func testNoUnsafeFallback(t *testing.T) {
	// Every injected native failure must return after exactly one native call,
	// retaining the source and leaving the destination absent.
	testNativeError(t, unix.EIO, ReasonIO)
}

func testPrivateAPI(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[Dir](), reflect.TypeFor[Component]()} {
		for i := range typ.NumField() {
			field := typ.Field(i)
			if field.IsExported() {
				t.Fatalf("exported ownership field %s.%s", typ.Name(), field.Name)
			}
		}
	}
	want := map[string]bool{"Close": true, "Identity": true, "Observe": true}
	typ := reflect.TypeFor[*Dir]()
	if typ.NumMethod() != len(want) {
		t.Fatalf("unexpected Dir API: %v", typ)
	}
	for i := range typ.NumMethod() {
		if !want[typ.Method(i).Name] {
			t.Fatalf("unexpected method %s", typ.Method(i).Name)
		}
	}
}
