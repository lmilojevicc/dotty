//go:build (linux || (darwin && cgo)) && !android && !ios

package nativefs

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPrivateNativeACLRefusal(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(map[bool]string{false: "File", true: "Directory"}[directory], func(t *testing.T) {
			r, d, calls := privateFixture(t)
			path := filepath.Join(r, "acl")
			if directory {
				mkdir(t, path)
			} else {
				write(t, path, "retained")
			}
			setPrivateNativeACL(t, r, path, directory)
			before, err := os.Lstat(path)
			must(t, err)
			calls.chmod = func(int, uint32) error { t.Fatal("ACL-bearing object repaired"); return nil }
			if directory {
				child, _, err := d.privateDir(component(t, "acl"), false, calls)
				if child != nil {
					t.Fatal("ACL directory returned")
				}
				e := authorityReason(t, err, ReasonAuthorityPolicy, path)
				if e.Observed.Security().State() != EvidencePresent {
					t.Fatal("native ACL facts lost")
				}
			} else {
				f, _, err := d.lockFile(component(t, "acl"), false, calls)
				if f != nil {
					t.Fatal("ACL file returned")
				}
				e := authorityReason(t, err, ReasonAuthorityPolicy, path)
				if e.Observed.Security().State() != EvidencePresent {
					t.Fatal("native ACL facts lost")
				}
			}
			after, err := os.Lstat(path)
			must(t, err)
			if !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("ACL fixture changed by open")
			}
		})
	}
	t.Run("ExclusiveCreatedFileBeforeChmod", func(t *testing.T) {
		r, d, calls := privateFixture(t)
		open := calls.acquisition.openat
		calls.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
			opened, err := open(fd, name, flags, mode)
			if err == nil {
				setPrivateNativeACL(t, r, filepath.Join(r, name), false)
			}
			return opened, err
		}
		calls.chmod = func(int, uint32) error { t.Fatal("created ACL-bearing FD chmodded"); return nil }
		f, record, err := d.lockFile(component(t, "acl"), true, calls)
		if f != nil {
			t.Fatal("created ACL-bearing file returned")
		}
		retainedCreation(t, record, err, filepath.Join(r, "acl"))
		e := authorityReason(t, err, ReasonAuthorityPolicy, filepath.Join(r, "acl"))
		if e.Observed.Security().State() != EvidencePresent {
			t.Fatal("native ACL facts lost")
		}
	})
}

func TestPrivateDirectoryRefusals(t *testing.T) {
	for _, kind := range []string{"symlink", "file", "mode", "special", "owner-injected"} {
		t.Run(kind, func(t *testing.T) {
			r, d, calls := privateFixture(t)
			path := filepath.Join(r, "bad")
			switch kind {
			case "symlink":
				must(t, os.Symlink("missing", path))
			case "file":
				write(t, path, "retained")
			default:
				mkdir(t, path)
				if kind == "mode" {
					must(t, os.Chmod(path, 0o755))
				}
				if kind == "special" {
					must(t, os.Chmod(path, 0o700|os.ModeSticky))
				}
			}
			before, err := os.Lstat(path)
			must(t, err)
			id, err := statEntry(d.state.leaf().fd, component(t, "bad"))
			must(t, err)
			stat := calls.authority.stat
			calls.authority.stat = func(fd int, st *unix.Stat_t) error {
				err := stat(fd, st)
				if err == nil && kind == "owner-injected" && statIdentity(st) == id {
					st.Uid++
				}
				return err
			}
			child, record, err := d.privateDir(component(t, "bad"), false, calls)
			if child != nil || record.Created() || err == nil {
				t.Fatal("bad directory accepted")
			}
			after, err := os.Lstat(path)
			must(t, err)
			if !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatal("bad directory repaired")
			}
		})
	}
}

func TestPrivateExistingOpenSwap(t *testing.T) {
	for _, directory := range []bool{false, true} {
		r, d, calls := privateFixture(t)
		path := filepath.Join(r, "entry")
		if directory {
			mkdir(t, path)
		} else {
			write(t, path, "original")
		}
		original, err := statEntry(d.state.leaf().fd, component(t, "entry"))
		must(t, err)
		injections := 0
		open := calls.acquisition.openat
		calls.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
			if name == "entry" {
				must(t, os.Rename(path, filepath.Join(r, "saved")))
				if directory {
					mkdir(t, path)
				} else {
					write(t, path, "winner")
				}
				injections++
			}
			return open(fd, name, flags, mode)
		}
		var record CreationRecord
		if directory {
			var child *Dir
			child, record, err = d.privateDir(component(t, "entry"), false, calls)
			if child != nil {
				t.Fatalf("existing directory swap adopted: %v (close: %v)", err, child.Close())
			}
		} else {
			var f *File
			f, record, err = d.lockFile(component(t, "entry"), false, calls)
			if f != nil {
				t.Fatalf("existing file swap adopted: %v (close: %v)", err, f.Close())
			}
			content(t, path, "winner")
		}
		if injections != 1 || record.Created() {
			t.Fatalf("existing swap hook successes=%d record=%+v: %v", injections, record, err)
		}
		e := reason(t, err, ReasonIdentityChanged)
		winner, statErr := statEntry(d.state.leaf().fd, component(t, "entry"))
		must(t, statErr)
		if e.Path != path || e.Err != nil || e.Expected != original || e.Observed != winner ||
			original == winner {
			t.Fatalf("wrong existing swap diagnostic: %+v", e)
		}
	}
}

func TestPrivateFilePostChmodDrift(t *testing.T) {
	for _, field := range []string{"gid-injected", "owner-injected", "security-injected", "mount-injected", "nlink", "mode", "parent-mode"} {
		t.Run(field, func(t *testing.T) {
			r, d, calls := privateFixture(t)
			path := filepath.Join(r, "lock")
			if field == "nlink" {
				// Establish the alias directory before the operation's parent baseline.
				mkdir(t, filepath.Join(r, "aliases"))
			}
			changed := false
			chmods, successfulChmods, injections := 0, 0, 0
			var beforeChmod, before, parentBefore AuthorityFacts
			var parentEntries []os.DirEntry
			calls.chmod = func(fd int, mode uint32) error {
				chmods++
				if mode != 0o600 {
					t.Fatalf("unexpected chmod mode: %#o", mode)
				}
				var err error
				beforeChmod, err = readAuthorityFacts(fd, systemAuthorityCalls())
				must(t, err)
				if err := unix.Fchmod(fd, mode); err != nil {
					return err
				}
				successfulChmods++
				before, err = readAuthorityFacts(fd, systemAuthorityCalls())
				must(t, err)
				if before.Mode() != 0o600 || before.LinkCount() != 1 {
					t.Fatalf("native chmod baseline: %+v", before)
				}
				switch field {
				case "nlink":
					parentBefore, err = readAuthorityFacts(
						d.state.leaf().fd,
						systemAuthorityCalls(),
					)
					must(t, err)
					parentEntries, err = os.ReadDir(r)
					must(t, err)
					must(t, os.Link(path, filepath.Join(r, "aliases", "alias")))
				case "mode":
					must(t, unix.Fchmod(fd, 0o400))
				case "parent-mode":
					must(t, os.Chmod(r, 0o755))
				}
				changed = true
				injections++
				return nil
			}
			stat := calls.authority.stat
			calls.authority.stat = func(fd int, st *unix.Stat_t) error {
				err := stat(fd, st)
				if err == nil && changed && statIdentity(st).Kind == unix.S_IFREG {
					if field == "gid-injected" {
						st.Gid++
					}
					if field == "owner-injected" {
						st.Uid++
					}
				}
				return err
			}
			security := calls.authority.security
			calls.authority.security = func(fd int, directory bool) (SecurityEvidence, error) {
				facts, err := security(fd, directory)
				if changed && !directory && field == "security-injected" {
					facts.state = EvidencePresent
				}
				return facts, err
			}
			filesystem := calls.authority.filesystem
			calls.authority.filesystem = func(fd int) (FilesystemFacts, error) {
				facts, err := filesystem(fd)
				id, statErr := statFD(fd)
				if statErr != nil {
					return facts, errors.Join(err, statErr)
				}
				if changed && id.Kind == unix.S_IFREG && field == "mount-injected" {
					facts.flags ^= 1
				}
				return facts, err
			}
			f, record, err := d.lockFile(component(t, "lock"), true, calls)
			if f != nil {
				t.Fatalf("post-chmod drift accepted: %v (close: %v)", err, f.Close())
			}
			if !changed || injections != 1 || chmods != 1 || successfulChmods != 1 {
				t.Fatalf(
					"post-chmod hook successes=%d chmod calls/successes=%d/%d: %v",
					injections,
					chmods,
					successfulChmods,
					err,
				)
			}
			retainedCreation(t, record, err, path)
			wantReason, wantPath, wantRole := ReasonAuthorityPolicy, path, RoleLockFile
			wantOp := "authority"
			wantDetail := "lock file requires effective UID, regular type, exact 0600, no special bits, and one link"
			switch field {
			case "gid-injected", "mount-injected":
				wantReason, wantOp = ReasonAuthorityChanged, "establish-lock-mode"
				wantDetail = "facts other than the exclusive-created file's mode changed, or exact 0600 was not established"
			case "security-injected":
				wantDetail = "an ACL is present; this boundary supports verified absence only"
			case "parent-mode":
				wantPath, wantRole = r, RolePrivateAnchor
				wantDetail = "private anchor requires effective UID and exact 0700 without special bits"
			}
			e := authorityReason(t, err, wantReason, wantPath)
			if e.Op != wantOp || e.Role != wantRole || e.Detail != wantDetail || e.Err != nil {
				t.Fatalf("wrong post-chmod refusal cause: %+v", e)
			}
			wantObserved := before
			switch field {
			case "gid-injected":
				wantObserved.gid++
			case "owner-injected":
				wantObserved.uid++
			case "security-injected":
				wantObserved.security.state = EvidencePresent
			case "mount-injected":
				wantObserved.filesystem.flags ^= 1
			case "nlink":
				wantObserved.nlink = 2
				var st unix.Stat_t
				must(t, unix.Lstat(path, &st))
				if statIdentity(&st) != before.Identity() || st.Nlink != 2 {
					t.Fatalf("native hardlink observation: %+v", st)
				}
				must(t, unix.Lstat(filepath.Join(r, "aliases", "alias"), &st))
				if statIdentity(&st) != before.Identity() || st.Nlink != 2 {
					t.Fatal("native alias not retained")
				}
				parentAfter, readErr := readAuthorityFacts(
					d.state.leaf().fd,
					systemAuthorityCalls(),
				)
				must(t, readErr)
				entries, readErr := os.ReadDir(r)
				must(t, readErr)
				if parentAfter != parentBefore || len(entries) != len(parentEntries) {
					t.Fatal(
						"hardlink injection changed direct parent identity, authority or entry count",
					)
				}
				for i, entry := range entries {
					if entry.Name() != parentEntries[i].Name() {
						t.Fatal("hardlink injection changed direct parent entries")
					}
				}
			case "mode":
				wantObserved.mode = 0o400
			case "parent-mode":
				var readErr error
				wantObserved, readErr = readAuthorityFacts(
					d.state.leaf().fd,
					systemAuthorityCalls(),
				)
				must(t, readErr)
				if wantObserved.Identity() != d.state.leaf().identity ||
					wantObserved.Mode() != 0o755 {
					t.Fatal("native parent-mode drift not retained")
				}
			}
			if e.Observed != wantObserved {
				t.Fatalf(
					"wrong post-chmod observed facts: got %+v want %+v",
					e.Observed,
					wantObserved,
				)
			}
			if wantReason == ReasonAuthorityChanged && e.Expected != beforeChmod {
				t.Fatalf(
					"wrong pre-chmod expected facts: got %+v want %+v",
					e.Expected,
					beforeChmod,
				)
			}
			found := false
			for _, observation := range record.Observations() {
				found = found ||
					observation.Path() == wantPath && observation.Phase() == "failure-observed" &&
						observation.Identity() == e.Observed.Identity() && observation.Facts() == e.Observed
			}
			if !found {
				t.Fatal("post-chmod failure facts absent from retained record")
			}
		})
	}
}

func TestPrivateHandleOwnership(t *testing.T) {
	t.Run("IndependentFileDescriptions", func(t *testing.T) {
		r, d, calls := privateFixture(t)
		write(t, filepath.Join(r, "lock"), "retained")
		first, _, err := d.lockFile(component(t, "lock"), false, calls)
		must(t, err)
		defer func() { must(t, first.Close()) }()
		second, _, err := d.lockFile(component(t, "lock"), false, calls)
		must(t, err)
		defer func() { must(t, second.Close()) }()
		if first.state.fd == second.state.fd {
			t.Fatal("same descriptor reused")
		}
		// File offsets expose shared open descriptions without exporting an FD
		// or implementing or inferring flock. Each open starts independently.
		_, err = unix.Seek(first.state.fd, 3, io.SeekStart)
		must(t, err)
		offset, err := unix.Seek(second.state.fd, 0, io.SeekCurrent)
		must(t, err)
		if offset != 0 {
			t.Fatal("shared file description")
		}
	})
	t.Run("IndependentDirectoryAncestry", func(t *testing.T) {
		_, d, calls := privateFixture(t)
		child, _, err := d.privateDir(component(t, "anchor"), true, calls)
		must(t, err)
		defer func() { must(t, child.Close()) }()
		for i, parent := range d.state.nodes {
			if child.state.nodes[i].fd == parent.fd {
				t.Fatal("owned parent FD copied")
			}
		}
		must(t, d.Close())
		_, err = child.Identity()
		must(t, err)
	})
	t.Run("FailureReleasesParentLease", func(t *testing.T) {
		r, d, calls := privateFixture(t)
		chmods := 0
		calls.chmod = func(int, uint32) error { chmods++; return unix.EIO }
		f, record, err := d.lockFile(component(t, "lock"), true, calls)
		if f != nil {
			t.Fatalf("expected postcreate failure: %v (close: %v)", err, f.Close())
		}
		if chmods != 1 || !errors.Is(err, unix.EIO) {
			t.Fatalf("expected injected chmod failure, calls=%d: %v", chmods, err)
		}
		path := filepath.Join(r, "lock")
		retainedCreation(t, record, err, path)
		e := reason(t, err, ReasonIO)
		if e.Op != "establish-lock-mode" || e.Path != path || !errors.Is(e.Err, unix.EIO) {
			t.Fatalf("wrong chmod failure: %+v", e)
		}
		lifecycleMu.Lock()
		active := d.state.active
		lifecycleMu.Unlock()
		if active != 0 {
			t.Fatalf("failure retained %d parent leases", active)
		}
		must(t, d.Close())
	})
}

func TestPrivateClosedAndInvalid(t *testing.T) {
	_, d, calls := privateFixture(t)
	for _, create := range []bool{false, true} {
		child, record, err := d.privateDir(Component{}, create, calls)
		if child != nil || record.Created() {
			t.Fatal("invalid directory component")
		}
		_ = reason(t, err, ReasonInvalidComponent)
		f, record, err := d.lockFile(Component{}, create, calls)
		if f != nil || record.Created() {
			t.Fatal("invalid file component")
		}
		_ = reason(t, err, ReasonInvalidComponent)
	}
	must(t, d.Close())
	for _, parent := range []*Dir{d, nil, {}} {
		for _, create := range []bool{false, true} {
			child, record, err := parent.privateDir(component(t, "entry"), create, calls)
			if child != nil || record.Created() {
				t.Fatal("closed directory accepted")
			}
			_ = reason(t, err, ReasonClosed)
			f, record, err := parent.lockFile(component(t, "entry"), create, calls)
			if f != nil || record.Created() {
				t.Fatal("closed file parent accepted")
			}
			_ = reason(t, err, ReasonClosed)
		}
	}
}
