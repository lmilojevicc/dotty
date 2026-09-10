//go:build (linux || (darwin && cgo)) && !android && !ios

package nativefs

import (
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

// Slice 31b's exact twelve required roots, written before production private
// create/open code. Execution is a parent-owned gate; source ordering is not a
// red-run claim. Nested cases supplement these roots, never replace them.
var requiredPrivateCreateOpenCases = []string{
	"ExclusiveFileModeAfterSecurityBinding",
	"ExistingObjectNeverRepairedTruncatedRemoved",
	"RawOpenFlags",
	"ExistingSymlinkWrongTypeOwnerModeSpecialRefused",
	"LockHardlinkRefused",
	"CreateCollisionWinnerUntouched",
	"MkdirCreationNotInodeOwnership",
	"PostCreateReplacementRefused",
	"RestrictiveUmaskDirectoryRetained",
	"CreationRecordNoDeleteAuthority",
	"ExactRecoveryPaths",
	"SystemAncestorsValidationOnly",
}

func TestPrivateCreateOpenBoundary(t *testing.T) {
	runAuthorityInventory(t, requiredPrivateCreateOpenCases, map[string]func(*testing.T){
		"ExclusiveFileModeAfterSecurityBinding":       testPrivateFileBinding,
		"ExistingObjectNeverRepairedTruncatedRemoved": testPrivateExistingUntouched,
		"RawOpenFlags": testPrivateFlags,
		"ExistingSymlinkWrongTypeOwnerModeSpecialRefused": testPrivateBadObjects,
		"LockHardlinkRefused":                             testPrivateHardlink,
		"CreateCollisionWinnerUntouched":                  testPrivateCollision,
		"MkdirCreationNotInodeOwnership":                  testPrivateMkdirEvent,
		"PostCreateReplacementRefused":                    testPrivateReplacement,
		"RestrictiveUmaskDirectoryRetained":               testPrivateUmask,
		"CreationRecordNoDeleteAuthority":                 testPrivateRecord,
		"ExactRecoveryPaths":                              testPrivateRecovery,
		"SystemAncestorsValidationOnly":                   testPrivateAncestors,
	})
}

// This adapter alone cuts security ancestry above the private fixture. Omitted
// facts stay invalid; it never synthesizes positive authority. Production native
// identity/edge guards still traverse every node, including the omitted prefix.
func privateFixtureCalls(root string) privateCalls {
	calls := systemPrivateCalls()
	calls.observe = func(nodes []pinnedDir, role AuthorityRole, readers authorityCalls) ([]AuthorityFacts, error) {
		for i, node := range nodes {
			if node.path == root {
				facts, err := observeAuthorityNodes(nodes[i:], role, readers)
				return append(make([]AuthorityFacts, i), facts...), err
			}
		}
		return nil, errors.New("private security root absent")
	}
	return calls
}

func privateFixture(t *testing.T) (string, *Dir, privateCalls) {
	t.Helper()
	r, d, _ := authorityFixture(t)
	return r, d, privateFixtureCalls(r)
}

func retainedCreation(t *testing.T, record CreationRecord, err error, path string) {
	t.Helper()
	var creation *CreationError
	if !record.Created() || record.Path() != path || !errors.As(err, &creation) ||
		!reflect.DeepEqual(creation.Record(), record) || !strings.Contains(err.Error(), path) ||
		!strings.Contains(err.Error(), "Inspect") || strings.Contains(err.Error(), "restored") {
		t.Fatalf("lost retained creation at %q: %+v / %v", path, record, err)
	}
}

func testPrivateFileBinding(t *testing.T) {
	r, d, calls := privateFixture(t)
	reads, chmods := 0, 0
	security := calls.authority.security
	calls.authority.security = func(fd int, directory bool) (SecurityEvidence, error) {
		if !directory {
			reads++
		}
		return security(fd, directory)
	}
	calls.chmod = func(fd int, mode uint32) error {
		chmods++
		if reads < 2 || mode != 0o600 {
			t.Fatal("chmod before repeated native security binding")
		}
		id, err := statFD(fd)
		must(t, err)
		bound, err := statEntry(d.state.leaf().fd, component(t, "lock"))
		must(t, err)
		if id != bound {
			t.Fatal("chmod on unbound descriptor")
		}
		return unix.Fchmod(fd, mode)
	}
	f, record, err := d.lockFile(component(t, "lock"), true, calls)
	must(t, err)
	must(t, f.Close())
	if chmods != 1 || !record.Created() || record.Path() != filepath.Join(r, "lock") {
		t.Fatal("missing creation")
	}
	for _, bad := range []string{"security", "owner", "hardlink", "excess-mode", "security-read"} {
		t.Run(bad, func(t *testing.T) {
			r, d, calls := privateFixture(t)
			if bad == "hardlink" {
				mkdir(t, filepath.Join(r, "aliases"))
			}
			injected := false
			calls.chmod = func(int, uint32) error { t.Fatal("unsafe prebinding chmod"); return nil }
			open := calls.acquisition.openat
			calls.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
				opened, err := open(fd, name, flags, mode)
				if err == nil && flags&unix.O_CREAT != 0 {
					if bad == "hardlink" {
						must(
							t,
							os.Link(filepath.Join(r, name), filepath.Join(r, "aliases", "alias")),
						)
						injected = true
					}
					if bad == "excess-mode" {
						must(t, unix.Fchmod(opened, 0o640))
						injected = true
					}
				}
				return opened, err
			}
			stat := calls.authority.stat
			calls.authority.stat = func(fd int, st *unix.Stat_t) error {
				err := stat(fd, st)
				if err == nil && statIdentity(st).Kind == unix.S_IFREG && bad == "owner" {
					st.Uid++ // Negative injected owner evidence, not native chown coverage.
					injected = true
				}
				return err
			}
			security := calls.authority.security
			calls.authority.security = func(fd int, directory bool) (SecurityEvidence, error) {
				f, err := security(fd, directory)
				if err == nil && !directory && bad == "security" {
					f.state = EvidencePresent // Negative injected ACL evidence.
					injected = true
				}
				if err == nil && !directory && bad == "security-read" {
					injected = true
					return f, unix.EIO
				}
				return f, err
			}
			f, record, err := d.lockFile(component(t, "lock"), true, calls)
			if f != nil {
				t.Fatalf("unsafe created file published: %v (close: %v)", err, f.Close())
			}
			if !injected {
				t.Fatalf("prebinding injection not reached: %v", err)
			}
			path := filepath.Join(r, "lock")
			retainedCreation(t, record, err, path)
			wantReason := ReasonAuthorityPolicy
			if bad == "security-read" {
				wantReason = ReasonIO
			}
			e := authorityReason(t, err, wantReason, path)
			if e.Role != RoleLockFile || e.Op != "authority" {
				t.Fatalf("wrong prebinding refusal: %+v", e)
			}
			if bad == "security-read" {
				if !errors.Is(e.Err, unix.EIO) ||
					e.Detail != "descriptor security evidence could not be read; ACL absence is unproven" {
					t.Fatalf("lost injected security-read cause: %+v", e)
				}
			} else if e.Err != nil || e.Detail != "exclusive-created file requires supported security, effective UID, regular type, one link and only umask-reduced 0600 bits before chmod" {
				t.Fatalf("wrong prebinding policy cause: %+v", e)
			}
			if bad == "owner" && e.Observed.UID() != uint32(os.Geteuid())+1 ||
				bad == "security" && e.Observed.Security().State() != EvidencePresent ||
				bad == "hardlink" && e.Observed.LinkCount() != 2 ||
				bad == "excess-mode" && e.Observed.Mode() != 0o640 {
				t.Fatalf("lost prebinding injected facts: %+v", e.Observed)
			}
		})
	}
}

func testPrivateExistingUntouched(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "directory"}[directory], func(t *testing.T) {
			r, d, calls := privateFixture(t)
			path := filepath.Join(r, "existing")
			if directory {
				mkdir(t, path)
			} else {
				write(t, path, "do not truncate")
			}
			before, err := os.Lstat(path)
			must(t, err)
			calls.chmod = func(int, uint32) error { t.Fatal("existing chmod"); return nil }
			if directory {
				child, _, err := d.privateDir(component(t, "existing"), false, calls)
				must(t, err)
				must(t, child.Close())
			} else {
				f, _, err := d.lockFile(component(t, "existing"), false, calls)
				must(t, err)
				must(t, f.Close())
				data, err := os.ReadFile(path)
				must(t, err)
				if string(data) != "do not truncate" {
					t.Fatal("existing content changed")
				}
			}
			after, err := os.Lstat(path)
			must(t, err)
			if !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("existing identity/mode changed")
			}
		})
	}
}

func testPrivateFlags(t *testing.T) {
	for _, create := range []bool{false, true} {
		for _, directory := range []bool{false, true} {
			r, d, calls := privateFixture(t)
			if !create {
				if directory {
					mkdir(t, filepath.Join(r, "entry"))
				} else {
					write(t, filepath.Join(r, "entry"), "bytes")
				}
			}
			opened, made := 0, 0
			open := calls.acquisition.openat
			calls.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
				if name == "entry" {
					opened++
					want, wantMode := unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOCTTY, uint32(
						0,
					)
					if directory {
						want = directoryFlags
					} else if create {
						want |= unix.O_CREAT | unix.O_EXCL
						wantMode = 0o600
					}
					if flags != want || mode != wantMode {
						t.Fatalf(
							"raw open flags/mode %#x/%#o want %#x/%#o",
							flags,
							mode,
							want,
							wantMode,
						)
					}
				}
				return open(fd, name, flags, mode)
			}
			calls.mkdir = func(fd int, name string, mode uint32) error {
				made++
				if !directory || !create || mode != 0o700 {
					t.Fatal("wrong mkdir")
				}
				return unix.Mkdirat(fd, name, mode)
			}
			if directory {
				child, _, err := d.privateDir(component(t, "entry"), create, calls)
				must(t, err)
				must(t, child.Close())
			} else {
				f, _, err := d.lockFile(component(t, "entry"), create, calls)
				must(t, err)
				must(t, f.Close())
			}
			if opened != 1 || (directory && create && made != 1) {
				t.Fatal("unexpected native operation count")
			}
		}
	}
}

func testPrivateBadObjects(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "fifo", "mode", "special", "owner-injected"} {
		t.Run(kind, func(t *testing.T) {
			r, d, calls := privateFixture(t)
			path := filepath.Join(r, "bad")
			switch kind {
			case "symlink":
				must(t, os.Symlink("missing", path))
			case "directory":
				mkdir(t, path)
			case "fifo":
				must(t, unix.Mkfifo(path, 0o600))
			default:
				write(t, path, "retained")
				if kind == "mode" {
					must(t, os.Chmod(path, 0o640))
				}
				if kind == "special" {
					must(t, os.Chmod(path, 0o600|os.ModeSetuid))
				}
			}
			before, err := os.Lstat(path)
			must(t, err)
			open := calls.acquisition.openat
			calls.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
				if kind == "symlink" || kind == "directory" || kind == "fifo" {
					t.Fatal("nonregular preobservation reached open")
				}
				return open(fd, name, flags, mode)
			}
			stat := calls.authority.stat
			calls.authority.stat = func(fd int, st *unix.Stat_t) error {
				err := stat(fd, st)
				if err == nil && kind == "owner-injected" && statIdentity(st).Kind == unix.S_IFREG {
					st.Uid++
				}
				return err
			}
			calls.chmod = func(int, uint32) error { t.Fatal("existing object repaired"); return nil }
			f, record, err := d.lockFile(component(t, "bad"), false, calls)
			if f != nil || err == nil || record.Created() {
				t.Fatalf("accepted %s: %v", kind, err)
			}
			after, err := os.Lstat(path)
			must(t, err)
			if !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("bad object changed")
			}
		})
	}
}

func testPrivateHardlink(t *testing.T) {
	r, d, calls := privateFixture(t)
	path := filepath.Join(r, "lock")
	write(t, path, "retained")
	must(t, os.Link(path, filepath.Join(r, "alias")))
	f, _, err := d.lockFile(component(t, "lock"), false, calls)
	if f != nil {
		t.Fatal("hardlink accepted")
	}
	e := authorityReason(t, err, ReasonAuthorityPolicy, path)
	if e.Observed.LinkCount() != 2 {
		t.Fatal("missing native hardlink observation")
	}
}

func testPrivateCollision(t *testing.T) {
	for _, directory := range []bool{false, true} {
		r, d, calls := privateFixture(t)
		path := filepath.Join(r, "winner")
		var before os.FileInfo
		if directory {
			calls.mkdir = func(fd int, name string, mode uint32) error {
				mkdir(t, path)
				var err error
				before, err = os.Lstat(path)
				must(t, err)
				return unix.Mkdirat(fd, name, mode)
			}
			child, record, err := d.privateDir(component(t, "winner"), true, calls)
			if child != nil || record.Created() || !errors.Is(err, unix.EEXIST) {
				t.Fatalf("collision adopted: %v", err)
			}
		} else {
			open := calls.acquisition.openat
			calls.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
				write(t, path, "race winner")
				var err error
				before, err = os.Lstat(path)
				must(t, err)
				return open(fd, name, flags, mode)
			}
			calls.chmod = func(int, uint32) error { t.Fatal("winner chmod"); return nil }
			f, record, err := d.lockFile(component(t, "winner"), true, calls)
			if f != nil || record.Created() || !errors.Is(err, unix.EEXIST) {
				t.Fatalf("collision adopted: %v", err)
			}
			data, err := os.ReadFile(path)
			must(t, err)
			if string(data) != "race winner" {
				t.Fatal("winner truncated")
			}
		}
		after, err := os.Lstat(path)
		must(t, err)
		if !os.SameFile(before, after) || before.Mode() != after.Mode() {
			t.Fatal("winner replaced/repaired")
		}
	}
}

func testPrivateMkdirEvent(t *testing.T) {
	r, d, calls := privateFixture(t)
	var created, winner Identity
	calls.mkdir = func(fd int, name string, mode uint32) error {
		if err := unix.Mkdirat(fd, name, mode); err != nil {
			return err
		}
		var err error
		created, err = statEntry(fd, component(t, name))
		must(t, err)
		must(t, os.Rename(filepath.Join(r, name), filepath.Join(r, "retained-original")))
		mkdir(t, filepath.Join(r, name))
		winner, err = statEntry(fd, component(t, name))
		must(t, err)
		return nil
	}
	child, record, err := d.privateDir(component(t, "anchor"), true, calls)
	must(t, err)
	defer func() { must(t, child.Close()) }()
	id, err := child.Identity()
	must(t, err)
	if id != winner || id == created || !record.Created() {
		t.Fatal("mkdir event confused with creator identity")
	}
	for _, observation := range record.Observations() {
		if observation.Path() == filepath.Join(r, "anchor") && observation.Identity() == created {
			t.Fatal("invented creator observation")
		}
	}
}

func testPrivateReplacement(t *testing.T) {
	for _, phase := range []string{"directory-repin", "file-before-bind", "file-after-chmod", "parent-repin"} {
		t.Run(phase, func(t *testing.T) {
			r, d, calls := privateFixture(t)
			path := filepath.Join(r, "entry")
			open := calls.acquisition.openat
			injections, chmods, successfulChmods := 0, 0, 0
			refusalPath, refusalOp := path, "bind-lock-file"
			if phase == "directory-repin" {
				refusalOp = "open-directory"
			}
			if phase == "parent-repin" {
				refusalPath, refusalOp = r, "repin-private-directory"
			}
			var original, winner Identity
			var winnerInfo, winnerEntryInfo os.FileInfo
			swap := func() {
				var st unix.Stat_t
				must(t, unix.Lstat(refusalPath, &st))
				original = statIdentity(&st)
				if phase == "parent-repin" {
					must(t, os.Rename(r, r+"-saved"))
					t.Cleanup(func() { must(t, os.RemoveAll(r+"-saved")) })
					mkdir(t, path)
				} else {
					must(t, os.Rename(path, filepath.Join(r, "saved")))
					if phase == "directory-repin" {
						mkdir(t, path)
					} else {
						write(t, path, "winner")
					}
				}
				must(t, unix.Lstat(refusalPath, &st))
				winner = statIdentity(&st)
				var err error
				winnerInfo, err = os.Lstat(refusalPath)
				must(t, err)
				winnerEntryInfo, err = os.Lstat(path)
				must(t, err)
				injections++
			}
			calls.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
				if phase == "directory-repin" && name == "entry" {
					swap()
				}
				if phase == "parent-repin" && injections == 0 {
					swap()
				}
				opened, err := open(fd, name, flags, mode)
				if err == nil && phase == "file-before-bind" {
					swap()
				}
				return opened, err
			}
			calls.chmod = func(fd int, mode uint32) error {
				chmods++
				if mode != 0o600 {
					t.Fatalf("unexpected chmod mode: %#o", mode)
				}
				if err := unix.Fchmod(fd, mode); err != nil {
					return err
				}
				successfulChmods++
				if phase == "file-after-chmod" {
					swap()
				}
				return nil
			}
			var record CreationRecord
			var err error
			if phase == "directory-repin" || phase == "parent-repin" {
				var child *Dir
				child, record, err = d.privateDir(component(t, "entry"), true, calls)
				if child != nil {
					t.Fatalf("replacement published: %v (close: %v)", err, child.Close())
				}
			} else {
				var f *File
				f, record, err = d.lockFile(component(t, "entry"), true, calls)
				if f != nil {
					t.Fatalf("replacement published: %v (close: %v)", err, f.Close())
				}
			}
			wantChmods := 0
			if phase == "file-after-chmod" {
				wantChmods = 1
			}
			if injections != 1 || chmods != wantChmods || successfulChmods != wantChmods {
				t.Fatalf(
					"replacement hook successes=%d chmod calls/successes=%d/%d: %v",
					injections,
					chmods,
					successfulChmods,
					err,
				)
			}
			retainedCreation(t, record, err, path)
			e := reason(t, err, ReasonIdentityChanged)
			if e.Path != refusalPath || e.Op != refusalOp || e.Err != nil ||
				!original.Valid || !winner.Valid || original == winner || e.Expected != original || e.Observed != winner {
				t.Fatalf("wrong replacement diagnostic: %+v", e)
			}
			foundExpected, foundObserved := false, false
			for _, observation := range record.Observations() {
				if observation.Path() == refusalPath {
					foundExpected = foundExpected ||
						observation.Phase() == "failure-expected" &&
							observation.Identity() == original
					foundObserved = foundObserved ||
						observation.Phase() == "failure-observed" &&
							observation.Identity() == winner
				}
			}
			if !foundExpected || !foundObserved {
				t.Fatal("replacement failure identities absent from retained record")
			}
			after, statErr := os.Lstat(refusalPath)
			must(t, statErr)
			if !os.SameFile(winnerInfo, after) || winnerInfo.Mode() != after.Mode() {
				t.Fatal("replacement winner changed")
			}
			entryAfter, statErr := os.Lstat(path)
			must(t, statErr)
			if !os.SameFile(winnerEntryInfo, entryAfter) ||
				winnerEntryInfo.Mode() != entryAfter.Mode() {
				t.Fatal("replacement entry changed")
			}
			if phase == "parent-repin" {
				entries, readErr := os.ReadDir(r)
				must(t, readErr)
				if len(entries) != 1 || entries[0].Name() != "entry" {
					t.Fatal("replacement parent content changed")
				}
			}
			if phase == "file-before-bind" || phase == "file-after-chmod" {
				content(t, path, "winner")
			} else {
				entries, readErr := os.ReadDir(path)
				must(t, readErr)
				if len(entries) != 0 {
					t.Fatal("replacement directory content changed")
				}
			}
			if strings.Contains(err.Error(), "-saved") ||
				strings.Contains(err.Error(), filepath.Join(r, "saved")) {
				t.Fatal("invented moved path")
			}
		})
	}
}

func testPrivateRecord(t *testing.T) {
	for _, typ := range []reflect.Type{reflect.TypeFor[File](), reflect.TypeFor[CreationRecord](), reflect.TypeFor[CreationObservation](), reflect.TypeFor[CreationError]()} {
		for i := range typ.NumField() {
			if typ.Field(i).IsExported() {
				t.Fatalf("mutable/exposed field %s.%s", typ, typ.Field(i).Name)
			}
		}
		for i := range reflect.PointerTo(typ).NumMethod() {
			name := reflect.PointerTo(typ).Method(i).Name
			if strings.Contains(name, "Delete") || strings.Contains(name, "Remove") ||
				strings.Contains(name, "FD") {
				t.Fatalf("authority escape %s", name)
			}
		}
	}
	if (CreationRecord{}).Created() {
		t.Fatal("zero creation event")
	}
	_, d, calls := privateFixture(t)
	f, record, err := d.lockFile(component(t, "lock"), true, calls)
	must(t, err)
	must(t, f.Close())
	observations := record.Observations()
	if len(observations) == 0 {
		t.Fatal("missing observations")
	}
	observations[0] = CreationObservation{}
	if record.Observations()[0] == (CreationObservation{}) {
		t.Fatal("mutable record alias")
	}
}

func testPrivateRecovery(t *testing.T) {
	t.Run("ParentAuthorityFailure", func(t *testing.T) {
		r, d, calls := privateFixture(t)
		calls.mkdir = func(fd int, name string, mode uint32) error {
			if err := unix.Mkdirat(fd, name, mode); err != nil {
				return err
			}
			return os.Chmod(r, 0o755) // adversarial postcreation drift, never repaired
		}
		child, record, err := d.privateDir(component(t, "created"), true, calls)
		if child != nil {
			t.Fatal("parent drift published")
		}
		retainedCreation(t, record, err, filepath.Join(r, "created"))
		e := authorityReason(t, err, ReasonAuthorityChanged, r)
		if e.Expected.Mode() != 0o700 || e.Observed.Mode() != 0o755 {
			t.Fatal("parent expected/observed facts lost")
		}
		if len(record.Observations()) == 0 {
			t.Fatal("recovery observations absent")
		}
		_, statErr := os.Lstat(filepath.Join(r, "created"))
		must(t, statErr)
	})
	for _, directory := range []bool{false, true} {
		r, d, calls := privateFixture(t)
		path := filepath.Join(r, "created")
		closeFD := calls.acquisition.close
		calls.acquisition.close = func(fd int) error { return errors.Join(closeFD(fd), unix.EINTR) }
		if directory {
			open := calls.acquisition.openat
			calls.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
				if name == "created" {
					return -1, unix.EACCES
				}
				return open(fd, name, flags, mode)
			}
			child, record, err := d.privateDir(component(t, "created"), true, calls)
			if child != nil {
				t.Fatal("failed repin published")
			}
			retainedCreation(t, record, err, path)
			if !errors.Is(err, unix.EACCES) || !errors.Is(err, unix.EINTR) {
				t.Fatalf("lost repin/cleanup cause: %v", err)
			}
			if strings.Count(err.Error(), "nativefs close-directory:") != len(d.state.nodes) {
				t.Fatalf("not every descriptor cleanup cause retained: %v", err)
			}
		} else {
			calls.chmod = func(int, uint32) error { return unix.EACCES }
			f, record, err := d.lockFile(component(t, "created"), true, calls)
			if f != nil {
				t.Fatal("failed chmod published")
			}
			retainedCreation(t, record, err, path)
			if !errors.Is(err, unix.EACCES) || !errors.Is(err, unix.EINTR) {
				t.Fatalf("lost chmod/cleanup cause: %v", err)
			}
		}
		_, err := os.Lstat(path)
		must(t, err)
	}
}

func testPrivateAncestors(t *testing.T) {
	r, d, calls := privateFixture(t)
	must(t, os.Chmod(r, 0o755)) // fixture setup, never repair after the operation
	child, _, err := d.privateDir(component(t, "anchor"), true, calls)
	must(t, err)
	must(t, child.Close())
	info, err := os.Stat(r)
	must(t, err)
	if info.Mode().Perm() != 0o755 {
		t.Fatal("system ancestor imposed 0700")
	}
	f, record, err := d.lockFile(component(t, "lock"), true, calls)
	if f != nil || record.Created() {
		t.Fatal("lock parent did not require private anchor")
	}
	_ = authorityReason(t, err, ReasonAuthorityPolicy, r)
	// Negative production track: no test boundary, injected unsupported native
	// filesystem observation at /. This never accesses a canonical user anchor.
	production := systemPrivateCalls()
	production.authority.filesystem = func(int) (FilesystemFacts, error) { return FilesystemFacts{state: EvidenceUnsupported}, nil }
	child, record, err = d.privateDir(component(t, "production-refused"), true, production)
	if child != nil || record.Created() {
		t.Fatal("production ancestry bypass")
	}
	_ = authorityReason(t, err, ReasonUnsupported, "/")
	actual, actualErr := d.OpenPrivateDir(component(t, "anchor"))
	if actual != nil {
		must(t, actual.Close())
	}
	t.Logf("unmodified production security ancestry (read-only private path): %v", actualErr)
}

func TestPrivateFileCloseLease(t *testing.T) {
	_, d, calls := privateFixture(t)
	closes := 0
	closeFD := calls.acquisition.close
	calls.acquisition.close = func(fd int) error { closes++; return errors.Join(closeFD(fd), unix.EINTR) }
	f, _, err := d.lockFile(component(t, "lock"), true, calls)
	must(t, err)
	defer func() { _ = f.Close() }()
	copyFile := *f
	parentDone := make(chan error, 1)
	go func() { parentDone <- d.Close() }()
	// Observe the lifecycle condition, not a timing-based assertion that Close
	// probably started. Test deadline bounds a broken implementation.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		lifecycleMu.Lock()
		closing := d.state.closing
		lifecycleMu.Unlock()
		if closing {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("parent close did not start")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if _, err := acquire(d); err == nil {
		t.Fatal("closing parent reacquired")
	}
	select {
	case err := <-parentDone:
		t.Fatalf("parent failed to wait: %v", err)
	default:
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if !errors.Is(copyFile.Close(), unix.EINTR) {
				t.Error("lost shared close error")
			}
		})
	}
	wg.Wait()
	if !errors.Is(f.Close(), unix.EINTR) || closes != 1 {
		t.Fatal("close retried/lost cause")
	}
	select {
	case err := <-parentDone:
		must(t, err)
	case <-deadline.C:
		t.Fatal("old parent lease not released on close error")
	}
	must(t, (*File)(nil).Close())
	must(t, (&File{}).Close())
}
