//go:build linux || darwin

package nativefs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Independent of the implementation map, declared before any production edits.
var requiredRegularObservationCases = []string{
	"EmptyBinaryAndStreamingDigest",
	"MetadataNormalizationAndExactEquality",
	"GeometryOnlyPayloadPolicy",
	"InvalidComponentMissingAndWrongKind",
	"SpecialEntryAndSymlinkSwapRefusal",
	"PreOpenFDNameBinding",
	"FullAncestryAndLeafReplacement",
	"SameInodeContentAndMetadataDrift",
	"ShrinkGrowthAndEOFProbe",
	"ShortReadEINTRAndReadFailures",
	"StatGuardAndOpenFailures",
	"ContextAndParentCloseCheckpoints",
	"CopiedParentConcurrentObservationAndPublication",
	"CloseOnceJoinedErrorsAndZeroResults",
	"AtimeExclusionImmutableAPIAndUnavailableTargets",
}

func runRegularInventory(t *testing.T, required []string, cases map[string]func(*testing.T)) {
	t.Helper()
	if len(required) != len(cases) {
		t.Fatalf(
			"regular inventory mismatch: %d required, %d implemented",
			len(required),
			len(cases),
		)
	}
	seen := map[string]bool{}
	for _, name := range required {
		fn, ok := cases[name]
		if !ok || seen[name] {
			t.Fatalf("missing or duplicate required regular case %q", name)
		}
		seen[name] = true
		completed := false
		t.Run(name, func(t *testing.T) {
			t.Cleanup(func() {
				if t.Skipped() {
					t.Errorf("required regular case skipped: %s", name)
				}
			})
			fn(t)
			completed = true
		})
		if !completed {
			t.Errorf("required regular case missing, filtered, or incomplete: %s", name)
		}
	}
}

func TestRegularObservationBoundary(t *testing.T) {
	runRegularInventory(t, requiredRegularObservationCases, map[string]func(*testing.T){
		"EmptyBinaryAndStreamingDigest":                   testRegularDigests,
		"MetadataNormalizationAndExactEquality":           testRegularMetadata,
		"GeometryOnlyPayloadPolicy":                       testRegularGeometry,
		"InvalidComponentMissingAndWrongKind":             testRegularInvalid,
		"SpecialEntryAndSymlinkSwapRefusal":               testRegularSpecialSwap,
		"PreOpenFDNameBinding":                            testRegularBinding,
		"FullAncestryAndLeafReplacement":                  testRegularReplacement,
		"SameInodeContentAndMetadataDrift":                testRegularDrift,
		"ShrinkGrowthAndEOFProbe":                         testRegularByteBudget,
		"ShortReadEINTRAndReadFailures":                   testRegularReads,
		"StatGuardAndOpenFailures":                        testRegularFailureInventory,
		"ContextAndParentCloseCheckpoints":                testRegularCheckpoints,
		"CopiedParentConcurrentObservationAndPublication": testRegularConcurrent,
		"CloseOnceJoinedErrorsAndZeroResults":             testRegularCleanup,
		"AtimeExclusionImmutableAPIAndUnavailableTargets": testRegularAPI,
	})
}

func regularFixture(t *testing.T, data []byte) (string, *Dir, Component) {
	t.Helper()
	r := fixture(t)
	path := filepath.Join(r, "payload")
	must(t, os.WriteFile(path, data, 0o600))
	return path, openDir(t, r), component(t, "payload")
}

func regularFailed(t *testing.T, got RegularObservation, err error) *RegularObservationError {
	t.Helper()
	if got != (RegularObservation{}) || err == nil {
		t.Fatalf("failure must be wholly zero: %+v, %v", got, err)
	}
	var e *RegularObservationError
	if !errors.As(err, &e) || e.Remediation == "" {
		t.Fatalf("missing regular diagnostic/remediation: %v", err)
	}
	if e.Path != "" && !strings.Contains(err.Error(), fmt.Sprintf("%q", e.Path)) {
		t.Fatalf("unquoted path: %v", err)
	}
	return e
}

func testRegularDigests(t *testing.T) {
	runRegularInventory(t, []string{"empty", "binary", "streaming"}, map[string]func(*testing.T){
		"empty":     func(t *testing.T) { regularDigest(t, nil) },
		"binary":    func(t *testing.T) { regularDigest(t, []byte{0, 255, 10, 128, 0}) },
		"streaming": func(t *testing.T) { regularDigest(t, bytes.Repeat([]byte{0, 251, 7}, 100000)) },
	})
}

func regularDigest(t *testing.T, data []byte) {
	path, d, name := regularFixture(t, data)
	var st unix.Stat_t
	must(t, unix.Lstat(path, &st))
	want, err := regularStatMetadata(&st)
	must(t, err)
	calls := systemRegularCalls()
	open, read, closeFD, stat, statat := calls.openat, calls.read, calls.close, calls.stat, calls.statat
	unlocked := func() {
		if !lifecycleMu.TryLock() {
			t.Fatal("native I/O under lifecycle mutex")
		}
		lifecycleMu.Unlock()
	}
	calls.stat = func(fd int, st *unix.Stat_t) error { unlocked(); return stat(fd, st) }
	calls.statat = func(fd int, n string, st *unix.Stat_t, flags int) error { unlocked(); return statat(fd, n, st, flags) }
	opens, reads, closes := 0, 0, 0
	calls.openat = func(fd int, n string, flags int, mode uint32) (int, error) {
		unlocked()
		opens++
		if fd != d.state.leaf().fd || n != name.name ||
			flags != unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_NOCTTY|unix.O_CLOEXEC ||
			mode != 0 {
			t.Fatal("open flags/name/parent widened")
		}
		return open(fd, n, flags, mode)
	}
	calls.read = func(fd int, b []byte) (int, error) {
		unlocked()
		reads++
		if len(b) == 0 || len(b) > 32768 {
			t.Fatalf("unbounded/empty read buffer: %d", len(b))
		}
		return read(fd, b)
	}
	calls.close = func(fd int) error { unlocked(); closes++; return closeFD(fd) }
	got, err := d.observeRegular(t.Context(), name, calls)
	must(t, err)
	if !got.Valid() || got.metadata != want || got.SHA256() != sha256.Sum256(data) ||
		got.Size() != int64(len(data)) ||
		opens != 1 ||
		closes != 1 ||
		reads == 0 {
		t.Fatalf(
			"observation=%+v want=%+v open/read/close=%d/%d/%d",
			got,
			want,
			opens,
			reads,
			closes,
		)
	}
	content(t, path, string(data))
}

func regularMetadataBase(t *testing.T) regularMetadata {
	t.Helper()
	m, err := normalizeRegularMetadata(
		Identity{Device: 0, Inode: 0, Kind: unix.S_IFREG, Valid: true},
		0,
		0,
		0o7777,
		math.MaxUint64,
		math.MaxInt64,
		math.MinInt64,
		999999999,
		math.MaxInt64,
		0,
	)
	must(t, err)
	return m
}

func testRegularMetadata(t *testing.T) {
	runRegularInventory(
		t,
		[]string{"exact_fields", "malformed", "native_normalization"},
		map[string]func(*testing.T){
			"exact_fields": func(t *testing.T) {
				base := regularMetadataBase(t)
				changes := map[string]func(*regularMetadata){
					"device":         func(m *regularMetadata) { m.identity.Device++ },
					"inode":          func(m *regularMetadata) { m.identity.Inode++ },
					"kind":           func(m *regularMetadata) { m.identity.Kind = unix.S_IFDIR },
					"identity_valid": func(m *regularMetadata) { m.identity.Valid = false },
					"uid":            func(m *regularMetadata) { m.uid++ },
					"gid":            func(m *regularMetadata) { m.gid++ },
					"mode":           func(m *regularMetadata) { m.mode ^= 0o4000 },
					"nlink":          func(m *regularMetadata) { m.nlink-- },
					"size":           func(m *regularMetadata) { m.size-- },
					"mtime_sec":      func(m *regularMetadata) { m.mtimeSec++ },
					"mtime_nsec":     func(m *regularMetadata) { m.mtimeNsec-- },
					"ctime_sec":      func(m *regularMetadata) { m.ctimeSec-- },
					"ctime_nsec":     func(m *regularMetadata) { m.ctimeNsec++ },
				}
				cases := map[string]func(*testing.T){}
				for name, change := range changes {
					cases[name] = func(t *testing.T) {
						other := base
						change(&other)
						err := compareRegularMetadata(
							"/private/quoted\npath",
							Component{name: "payload"},
							base,
							other,
						)
						e := regularFailed(t, RegularObservation{}, err)
						want := ReasonMetadataChanged
						if base.identity != other.identity {
							want = ReasonIdentityChanged
						}
						if e.Reason != want || e.Expected.metadata != base ||
							e.Observed.metadata != other ||
							!strings.Contains(err.Error(), fmt.Sprintf("%+v", base)) ||
							!strings.Contains(err.Error(), fmt.Sprintf("%+v", other)) {
							t.Fatalf("lost exact metadata: %+v", e)
						}
					}
				}
				runRegularInventory(
					t,
					[]string{
						"device",
						"inode",
						"kind",
						"identity_valid",
						"uid",
						"gid",
						"mode",
						"nlink",
						"size",
						"mtime_sec",
						"mtime_nsec",
						"ctime_sec",
						"ctime_nsec",
					},
					cases,
				)
				must(t, compareRegularMetadata("path", Component{}, base, base))
			},
			"malformed": func(t *testing.T) {
				cases := map[string]func(*testing.T){}
				for _, name := range []string{"identity", "kind", "mode", "size", "mtime_negative", "mtime_billion", "ctime_negative", "ctime_billion"} {
					cases[name] = func(t *testing.T) {
						m := regularMetadataBase(t)
						switch name {
						case "identity":
							m.identity.Valid = false
						case "kind":
							m.identity.Kind = unix.S_IFIFO
						case "mode":
							m.mode = 0o10000
						case "size":
							m.size = -1
						case "mtime_negative":
							m.mtimeNsec = -1
						case "mtime_billion":
							m.mtimeNsec = 1000000000
						case "ctime_negative":
							m.ctimeNsec = -1
						case "ctime_billion":
							m.ctimeNsec = 1000000000
						}
						got, err := normalizeRegularMetadata(
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
						if err == nil || got != (regularMetadata{}) {
							t.Fatalf("accepted malformed %s: %+v %v", name, got, err)
						}
					}
				}
				runRegularInventory(
					t,
					[]string{
						"identity",
						"kind",
						"mode",
						"size",
						"mtime_negative",
						"mtime_billion",
						"ctime_negative",
						"ctime_billion",
					},
					cases,
				)
			},
			"native_normalization": func(t *testing.T) {
				path, _, _ := regularFixture(t, []byte("metadata"))
				var st unix.Stat_t
				must(t, unix.Lstat(path, &st))
				got, err := regularStatMetadata(&st)
				must(t, err)
				mode, nlink := normalizeStatModeAndLinks(st.Mode, st.Nlink)
				ms, mn := st.Mtim.Unix()
				cs, cn := st.Ctim.Unix()
				if got.identity != statIdentity(&st) || got.uid != st.Uid || got.gid != st.Gid ||
					got.mode != mode ||
					got.nlink != nlink ||
					got.size != st.Size ||
					got.mtimeSec != ms ||
					got.mtimeNsec != mn ||
					got.ctimeSec != cs ||
					got.ctimeNsec != cn {
					t.Fatalf("native normalization lost fields: %+v", got)
				}
				st.Size = -1
				if _, err := regularStatMetadata(&st); err == nil {
					t.Fatal("negative native size accepted")
				}
			},
		},
	)
}

func testRegularGeometry(t *testing.T) {
	runRegularInventory(
		t,
		[]string{"native_modes_hardlinks", "counterfactual_foreign_uid_special_bits"},
		map[string]func(*testing.T){
			"native_modes_hardlinks": func(t *testing.T) {
				path, d, name := regularFixture(t, []byte("shared"))
				must(t, os.Chmod(filepath.Dir(path), 0o755))
				must(t, unix.Chmod(path, 0o4644))
				must(t, os.Link(path, path+"-alias"))
				got, err := d.ObserveRegular(t.Context(), name)
				must(t, err)
				var st unix.Stat_t
				must(t, unix.Lstat(path, &st))
				if st.Mode&0o7777 != 0o4644 || got.Mode() != 0o4644 || st.Nlink != 2 ||
					got.LinkCount() != 2 ||
					got.UID() != st.Uid ||
					got.GID() != st.Gid {
					t.Fatalf("general payload policy lost native facts: %+v", got)
				}
				content(t, path+"-alias", "shared")
			},
			"counterfactual_foreign_uid_special_bits": func(t *testing.T) {
				// Synthetic metadata only: not native foreign-owner or authority evidence.
				path, d, name := regularFixture(t, []byte("counterfactual"))
				calls := systemRegularCalls()
				stat, statat := calls.stat, calls.statat
				var payloadID unix.Stat_t
				must(t, unix.Lstat(path, &payloadID))
				alter := func(st *unix.Stat_t) {
					if statIdentity(st) == statIdentity(&payloadID) {
						st.Uid ^= 1
						st.Gid ^= 1
						st.Mode |= 0o7777
						st.Nlink = 7
					}
				}
				calls.stat = func(fd int, st *unix.Stat_t) error {
					err := stat(fd, st)
					if err == nil {
						alter(st)
					}
					return err
				}
				calls.statat = func(fd int, n string, st *unix.Stat_t, flags int) error {
					err := statat(fd, n, st, flags)
					if err == nil {
						alter(st)
					}
					return err
				}
				got, err := d.observeRegular(t.Context(), name, calls)
				must(t, err)
				if got.UID() != payloadID.Uid^1 || got.GID() != payloadID.Gid^1 ||
					got.Mode() != 0o7777 ||
					got.LinkCount() != 7 {
					t.Fatalf("counterfactual policy incorrectly narrowed: %+v", got)
				}
			},
		},
	)
}

func testRegularInvalid(t *testing.T) {
	runRegularInventory(
		t,
		[]string{
			"component",
			"missing",
			"directory",
			"symlink",
			"fifo",
			"nil_parent",
			"zero_parent",
			"closed_parent",
		},
		map[string]func(*testing.T){
			"component": func(t *testing.T) {
				_, d, _ := regularFixture(t, nil)
				for _, text := range []string{"", ".", "..", "a/b", "a\x00b"} {
					calls := systemRegularCalls()
					calls.statat = func(int, string, *unix.Stat_t, int) error { t.Fatal("I/O on invalid component"); return nil }
					got, err := d.observeRegular(t.Context(), Component{name: text}, calls)
					if regularFailed(t, got, err).Reason != ReasonInvalidComponent {
						t.Fatal(err)
					}
				}
			},
			"missing":   func(t *testing.T) { regularWrongKind(t, "missing") },
			"directory": func(t *testing.T) { regularWrongKind(t, "directory") },
			"symlink":   func(t *testing.T) { regularWrongKind(t, "symlink") },
			"fifo":      func(t *testing.T) { regularWrongKind(t, "fifo") },
			"nil_parent": func(t *testing.T) {
				var d *Dir
				got, err := d.ObserveRegular(t.Context(), component(t, "payload"))
				_ = regularFailed(t, got, err)
			},
			"zero_parent": func(t *testing.T) {
				var d Dir
				got, err := d.ObserveRegular(t.Context(), component(t, "payload"))
				_ = regularFailed(t, got, err)
			},
			"closed_parent": func(t *testing.T) {
				_, d, n := regularFixture(t, nil)
				must(t, d.Close())
				got, err := d.ObserveRegular(t.Context(), n)
				if regularFailed(t, got, err).Reason != ReasonClosed {
					t.Fatal(err)
				}
			},
		},
	)
}

func regularWrongKind(t *testing.T, kind string) {
	path, d, name := regularFixture(t, []byte("referent"))
	must(t, os.Rename(path, path+"-saved"))
	switch kind {
	case "directory":
		mkdir(t, path)
	case "symlink":
		must(t, os.Symlink(path+"-saved", path))
	case "fifo":
		must(t, unix.Mkfifo(path, 0o600))
	}
	calls := systemRegularCalls()
	calls.openat = func(int, string, int, uint32) (int, error) {
		t.Fatal("opened known nonregular/missing entry")
		return -1, unix.EIO
	}
	calls.read = func(int, []byte) (int, error) { t.Fatal("hashed known wrong kind"); return 0, unix.EIO }
	got, err := d.observeRegular(t.Context(), name, calls)
	e := regularFailed(t, got, err)
	if e.Path != path {
		t.Fatal(err)
	}
	if kind == "missing" && !errors.Is(err, unix.ENOENT) {
		t.Fatal("lost ENOENT")
	}
	content(t, path+"-saved", "referent")
}

func testRegularSpecialSwap(t *testing.T) {
	runRegularInventory(
		t,
		[]string{"symlink_before_open", "fifo_before_open", "counterfactual_device_fd"},
		map[string]func(*testing.T){
			"symlink_before_open": func(t *testing.T) { regularOpenSwap(t, "symlink") },
			"fifo_before_open":    func(t *testing.T) { regularOpenSwap(t, "fifo") },
			"counterfactual_device_fd": func(t *testing.T) {
				// No device is opened. A real private regular FD supplies the negative stat seam.
				_, d, name := regularFixture(t, []byte("never hash"))
				calls := systemRegularCalls()
				open, stat, closeFD := calls.openat, calls.stat, calls.close
				owned, hits, closes := -1, 0, 0
				calls.openat = func(fd int, n string, f int, m uint32) (int, error) {
					var err error
					owned, err = open(fd, n, f, m)
					return owned, err
				}
				calls.stat = func(fd int, st *unix.Stat_t) error {
					err := stat(fd, st)
					if fd == owned && err == nil {
						hits++
						st.Mode = (st.Mode & 0o7777) | unix.S_IFCHR
					}
					return err
				}
				calls.read = func(int, []byte) (int, error) { t.Fatal("special FD reached hash"); return 0, unix.EIO }
				calls.close = func(fd int) error { closes++; return closeFD(fd) }
				got, err := d.observeRegular(t.Context(), name, calls)
				_ = regularFailed(t, got, err)
				if hits != 1 || closes != 1 {
					t.Fatalf("wrong-kind seam/cleanup=%d/%d", hits, closes)
				}
			},
		},
	)
}

func regularOpenSwap(t *testing.T, kind string) {
	path, d, name := regularFixture(t, []byte("unchanged"))
	calls := systemRegularCalls()
	open, closeFD := calls.openat, calls.close
	opens, closes := 0, 0
	calls.openat = func(fd int, n string, f int, m uint32) (int, error) {
		opens++
		must(t, os.Rename(path, path+"-saved"))
		if kind == "symlink" {
			must(t, os.Symlink(path+"-saved", path))
		} else {
			must(t, unix.Mkfifo(path, 0o600))
		}
		return open(fd, n, f, m)
	}
	calls.read = func(int, []byte) (int, error) { t.Fatal("swapped special entry reached hash"); return 0, unix.EIO }
	calls.close = func(fd int) error { closes++; return closeFD(fd) }
	got, err := d.observeRegular(t.Context(), name, calls)
	_ = regularFailed(t, got, err)
	wantCloses := 1
	if kind == "symlink" {
		wantCloses = 0
		if !errors.Is(err, unix.ELOOP) {
			t.Fatal(err)
		}
	}
	if opens != 1 || closes != wantCloses {
		t.Fatalf("open/close=%d/%d", opens, closes)
	}
	content(t, path+"-saved", "unchanged")
}

func testRegularBinding(t *testing.T) {
	runRegularInventory(
		t,
		[]string{"opened_fd_replacement", "name_after_open"},
		map[string]func(*testing.T){
			"opened_fd_replacement": func(t *testing.T) { regularBindingSwap(t) },
			"name_after_open":       testRegularNameBinding,
		},
	)
}

func testRegularNameBinding(t *testing.T) {
	path, d, name := regularFixture(t, []byte("bound FD"))
	calls := systemRegularCalls()
	statat, closeFD := calls.statat, calls.close
	names, hits, closes := 0, 0, 0
	var expected, observed regularMetadata
	calls.statat = func(fd int, n string, st *unix.Stat_t, flags int) error {
		err := statat(fd, n, st, flags)
		if err == nil && n == name.name {
			names++
			if names == 1 {
				expected, err = regularStatMetadata(st)
			}
			if names == 2 {
				hits++
				st.Ino++
				observed, err = regularStatMetadata(st)
			}
		}
		return err
	}
	calls.read = func(int, []byte) (int, error) { t.Fatal("name mismatch reached hashing"); return 0, unix.EIO }
	calls.close = func(fd int) error { closes++; return closeFD(fd) }
	got, err := d.observeRegular(t.Context(), name, calls)
	e := regularFailed(t, got, err)
	if hits != 1 || names != 2 || closes != 1 || e.Path != path ||
		e.Reason != ReasonIdentityChanged ||
		e.Expected.metadata != expected ||
		e.Observed.metadata != observed {
		t.Fatalf("name binding seam/evidence: %d/%d/%d %+v", hits, names, closes, e)
	}
}

func regularBindingSwap(t *testing.T) {
	path, d, name := regularFixture(t, []byte("original"))
	calls := systemRegularCalls()
	open, closeFD := calls.openat, calls.close
	opens, closes := 0, 0
	swap := func() { must(t, os.Rename(path, path+"-saved")); write(t, path, "replacement") }
	calls.openat = func(fd int, n string, f int, m uint32) (int, error) {
		opens++
		swap()
		return open(fd, n, f, m)
	}
	calls.read = func(int, []byte) (int, error) { t.Fatal("unbound FD reached hash"); return 0, unix.EIO }
	calls.close = func(fd int) error { closes++; return closeFD(fd) }
	got, err := d.observeRegular(t.Context(), name, calls)
	e := regularFailed(t, got, err)
	if e.Path != path || e.Reason != ReasonIdentityChanged || opens != 1 || closes != 1 {
		t.Fatalf("binding failure: %+v opens/closes=%d/%d", e, opens, closes)
	}
	content(t, path, "replacement")
	content(t, path+"-saved", "original")
}

func testRegularReplacement(t *testing.T) {
	runRegularInventory(
		t,
		[]string{
			"ancestor_before_open",
			"ancestor_after_read",
			"parent_after_read",
			"leaf_after_read",
			"root_edge_injection",
			"every_ancestry_edge",
		},
		map[string]func(*testing.T){
			"ancestor_before_open": func(t *testing.T) { regularReplacement(t, "ancestor", false) },
			"ancestor_after_read":  func(t *testing.T) { regularReplacement(t, "ancestor", true) },
			"parent_after_read":    func(t *testing.T) { regularReplacement(t, "parent", true) },
			"leaf_after_read":      func(t *testing.T) { regularReplacement(t, "leaf", true) },
			"root_edge_injection": func(t *testing.T) {
				_, d, name := regularFixture(t, nil)
				calls := systemRegularCalls()
				statat := calls.statat
				hits := 0
				calls.statat = func(fd int, n string, st *unix.Stat_t, flags int) error {
					err := statat(fd, n, st, flags)
					if n == "/" && err == nil {
						hits++
						st.Ino++
					}
					return err
				}
				calls.openat = func(int, string, int, uint32) (int, error) { t.Fatal("bad root reached open"); return -1, unix.EIO }
				got, err := d.observeRegular(t.Context(), name, calls)
				e := regularFailed(t, got, err)
				if hits != 1 || e.Path != "/" || e.Reason != ReasonIdentityChanged {
					t.Fatal(err)
				}
			},
			"every_ancestry_edge": func(t *testing.T) {
				_, d, name := regularFixture(t, []byte("guard"))
				calls := systemRegularCalls()
				stat, statat := calls.stat, calls.statat
				fdHits, edgeHits := map[int]int{}, map[string]int{}
				calls.stat = func(fd int, st *unix.Stat_t) error { fdHits[fd]++; return stat(fd, st) }
				calls.statat = func(fd int, n string, st *unix.Stat_t, flags int) error {
					if flags != unix.AT_SYMLINK_NOFOLLOW {
						t.Fatal("followed edge")
					}
					edgeHits[fmt.Sprintf("%d:%s", fd, n)]++
					return statat(fd, n, st, flags)
				}
				_, err := d.observeRegular(t.Context(), name, calls)
				must(t, err)
				for i, node := range d.state.nodes {
					fd, n := node.fd, "/"
					if i > 0 {
						fd, n = d.state.nodes[i-1].fd, node.name.name
					}
					if fdHits[node.fd] < 3 || edgeHits[fmt.Sprintf("%d:%s", fd, n)] < 3 {
						t.Fatalf(
							"incomplete repeated full ancestry: %q fd=%d edge=%d",
							node.path,
							fdHits[node.fd],
							edgeHits[fmt.Sprintf("%d:%s", fd, n)],
						)
					}
				}
			},
		},
	)
}

// An assertion inside an open seam must not strand a successful native FD
// before the operation receives ownership. Failed hooks unwind it immediately.
func regularOpenHook(t *testing.T, fd int, hook func()) {
	t.Helper()
	completed := false
	defer func() {
		if !completed {
			must(t, unix.Close(fd))
		}
	}()
	hook()
	completed = true
}

func regularReplacement(t *testing.T, which string, afterRead bool) {
	r := fixture(t)
	ancestor := filepath.Join(r, "ancestor")
	parent := filepath.Join(ancestor, "parent")
	mkdir(t, parent)
	path := filepath.Join(parent, "payload")
	write(t, path, "original")
	d := openDir(t, parent)
	name := component(t, "payload")
	target := map[string]string{"ancestor": ancestor, "parent": parent, "leaf": path}[which]
	swap := func() {
		must(t, os.Rename(target, target+"-saved"))
		if which == "leaf" {
			write(t, target, "external")
		} else {
			mkdir(t, target)
		}
	}
	calls := systemRegularCalls()
	open, read, closeFD := calls.openat, calls.read, calls.close
	hits, closes := 0, 0
	if afterRead {
		calls.read = func(fd int, b []byte) (int, error) {
			n, err := read(fd, b)
			if hits == 0 {
				hits++
				swap()
			}
			return n, err
		}
	} else {
		calls.openat = func(fd int, n string, f int, m uint32) (int, error) {
			got, err := open(fd, n, f, m)
			if err == nil {
				regularOpenHook(t, got, func() { hits++; swap() })
			}
			return got, err
		}
		calls.read = func(int, []byte) (int, error) { t.Fatal("stale ancestry hashed"); return 0, unix.EIO }
	}
	calls.close = func(fd int) error { closes++; return closeFD(fd) }
	got, err := d.observeRegular(t.Context(), name, calls)
	e := regularFailed(t, got, err)
	if hits != 1 || closes != 1 || e.Path != target ||
		(e.Reason != ReasonIdentityChanged && e.Reason != ReasonMetadataChanged) {
		t.Fatalf("replacement hook/cleanup/path: %d/%d %+v", hits, closes, e)
	}
}

func testRegularDrift(t *testing.T) {
	runRegularInventory(
		t,
		[]string{"native_content", "native_mode", "native_nlink", "counterfactual_all_fields"},
		map[string]func(*testing.T){
			"native_content": func(t *testing.T) { regularNativeDrift(t, "content") },
			"native_mode":    func(t *testing.T) { regularNativeDrift(t, "mode") },
			"native_nlink":   func(t *testing.T) { regularNativeDrift(t, "nlink") },
			"counterfactual_all_fields": func(t *testing.T) {
				cases := map[string]func(*testing.T){}
				for _, field := range []string{"uid", "gid", "mode", "nlink", "size", "mtime_sec", "mtime_nsec", "ctime_sec", "ctime_nsec"} {
					cases[field] = func(t *testing.T) {
						_, d, name := regularFixture(t, []byte("drift"))
						calls := systemRegularCalls()
						open, stat, read := calls.openat, calls.stat, calls.read
						owned, hits, reads := -1, 0, 0
						calls.openat = func(fd int, n string, f int, m uint32) (int, error) {
							var err error
							owned, err = open(fd, n, f, m)
							return owned, err
						}
						calls.read = func(fd int, b []byte) (int, error) { reads++; return read(fd, b) }
						calls.stat = func(fd int, st *unix.Stat_t) error {
							err := stat(fd, st)
							if err == nil && fd == owned && reads > 0 {
								hits++
								switch field {
								case "uid":
									st.Uid ^= 1
								case "gid":
									st.Gid ^= 1
								case "mode":
									st.Mode ^= 0o100
								case "nlink":
									st.Nlink++
								case "size":
									st.Size++
								case "mtime_sec":
									st.Mtim.Sec++
								case "mtime_nsec":
									st.Mtim.Nsec = (st.Mtim.Nsec + 1) % 1000000000
								case "ctime_sec":
									st.Ctim.Sec++
								case "ctime_nsec":
									st.Ctim.Nsec = (st.Ctim.Nsec + 1) % 1000000000
								}
							}
							return err
						}
						got, err := d.observeRegular(t.Context(), name, calls)
						e := regularFailed(t, got, err)
						if hits != 1 || reads < 2 || e.Reason != ReasonMetadataChanged ||
							e.Expected.Identity() != e.Observed.Identity() ||
							e.Expected.metadata == e.Observed.metadata {
							t.Fatalf("drift seam failed: hits=%d reads=%d %+v", hits, reads, e)
						}
					}
				}
				runRegularInventory(
					t,
					[]string{
						"uid",
						"gid",
						"mode",
						"nlink",
						"size",
						"mtime_sec",
						"mtime_nsec",
						"ctime_sec",
						"ctime_nsec",
					},
					cases,
				)
			},
		},
	)
}

func regularNativeDrift(t *testing.T, field string) {
	path, d, name := regularFixture(t, []byte("before"))
	calls := systemRegularCalls()
	read := calls.read
	hits := 0
	calls.read = func(fd int, b []byte) (int, error) {
		n, err := read(fd, b)
		if hits == 0 {
			hits++
			switch field {
			case "content":
				write(t, path, "after!")
				// Explicitly change mtime too: no timestamp-resolution timing assumption.
				must(t, os.Chtimes(path, time.Unix(1, 0), time.Unix(2, 0)))
			case "mode":
				must(t, os.Chmod(path, 0o640))
			case "nlink":
				must(t, os.Link(path, path+"-alias"))
			}
		}
		return n, err
	}
	got, err := d.observeRegular(t.Context(), name, calls)
	e := regularFailed(t, got, err)
	if hits != 1 || e.Reason != ReasonMetadataChanged ||
		e.Expected.Identity() != e.Observed.Identity() {
		t.Fatalf("same-inode drift: %+v", e)
	}
}

func testRegularByteBudget(t *testing.T) {
	runRegularInventory(
		t,
		[]string{
			"native_shrink",
			"native_growth",
			"empty_probe_growth",
			"maximum_signed_size",
			"exact_probe",
		},
		map[string]func(*testing.T){
			"native_shrink":      func(t *testing.T) { regularSizeChange(t, false, false) },
			"native_growth":      func(t *testing.T) { regularSizeChange(t, true, false) },
			"empty_probe_growth": func(t *testing.T) { regularSizeChange(t, true, true) },
			"maximum_signed_size": func(t *testing.T) {
				_, d, name := regularFixture(t, nil)
				calls := systemRegularCalls()
				stat, statat := calls.stat, calls.statat
				reads := 0
				calls.stat = func(fd int, st *unix.Stat_t) error {
					err := stat(fd, st)
					if err == nil && statIdentity(st).Kind == unix.S_IFREG {
						st.Size = math.MaxInt64
					}
					return err
				}
				calls.statat = func(fd int, n string, st *unix.Stat_t, f int) error {
					err := statat(fd, n, st, f)
					if err == nil && n == name.name {
						st.Size = math.MaxInt64
					}
					return err
				}
				calls.read = func(_ int, b []byte) (int, error) {
					reads++
					if len(b) != 32768 {
						t.Fatalf("overflowed size/buffer: %d", len(b))
					}
					return 0, nil
				}
				got, err := d.observeRegular(t.Context(), name, calls)
				_ = regularFailed(t, got, err)
				if reads != 1 {
					t.Fatalf("unexpected maximum-size reads: %d", reads)
				}
			},
			"exact_probe": func(t *testing.T) {
				_, d, name := regularFixture(t, []byte("abc"))
				calls := systemRegularCalls()
				read := calls.read
				lengths := []int{}
				calls.read = func(fd int, b []byte) (int, error) { lengths = append(lengths, len(b)); return read(fd, b) }
				_, err := d.observeRegular(t.Context(), name, calls)
				must(t, err)
				if !reflect.DeepEqual(lengths, []int{3, 1}) {
					t.Fatalf("budget/probe=%v", lengths)
				}
			},
		},
	)
}

func regularSizeChange(t *testing.T, grow, empty bool) {
	data := []byte("abc")
	if empty {
		data = nil
	}
	path, d, name := regularFixture(t, data)
	calls := systemRegularCalls()
	read := calls.read
	hits := 0
	calls.read = func(fd int, b []byte) (int, error) {
		if hits == 0 {
			hits++
			if grow {
				must(t, os.WriteFile(path, append(data, 'x'), 0o600))
			} else {
				must(t, os.Truncate(path, 0))
			}
		}
		return read(fd, b)
	}
	got, err := d.observeRegular(t.Context(), name, calls)
	e := regularFailed(t, got, err)
	if hits != 1 || e.Reason != ReasonMetadataChanged {
		t.Fatalf("byte-budget drift: %+v", e)
	}
}

func testRegularReads(t *testing.T) {
	cases := map[string]func(*testing.T){}
	for _, kind := range []string{"short_reads", "eintr", "eintr_after_progress", "probe_eintr", "read_eio", "probe_eio", "negative_count", "oversized_count", "eintr_positive_count", "error_positive_count", "repeated_eintr_cancel"} {
		cases[kind] = func(t *testing.T) {
			_, d, name := regularFixture(t, []byte("abcdef"))
			calls := systemRegularCalls()
			read, closeFD := calls.read, calls.close
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			hits, injected, closes := 0, 0, 0
			calls.read = func(fd int, b []byte) (int, error) {
				hits++
				if kind == "short_reads" {
					return read(fd, b[:1])
				}
				if kind == "repeated_eintr_cancel" {
					injected++
					if injected == 3 {
						cancel()
					}
					return -1, unix.EINTR
				}
				if injected == 0 &&
					((kind == "probe_eintr" || kind == "probe_eio") && hits == 2 || kind == "eintr_after_progress" && hits == 2 || kind != "probe_eintr" && kind != "probe_eio" && kind != "eintr_after_progress") {
					injected++
					switch kind {
					case "eintr", "eintr_after_progress", "probe_eintr":
						return -1, unix.EINTR
					case "read_eio", "probe_eio":
						return -1, unix.EIO
					case "negative_count":
						return -1, nil
					case "oversized_count":
						return len(b) + 1, nil
					case "eintr_positive_count":
						return 1, unix.EINTR
					case "error_positive_count":
						return 1, unix.EIO
					}
				}
				if kind == "eintr_after_progress" {
					return read(fd, b[:1])
				}
				return read(fd, b)
			}
			calls.close = func(fd int) error { closes++; return closeFD(fd) }
			got, err := d.observeRegular(ctx, name, calls)
			positive := kind == "short_reads" || kind == "eintr" ||
				kind == "eintr_after_progress" ||
				kind == "probe_eintr"
			if positive {
				must(t, err)
				if got.SHA256() != sha256.Sum256([]byte("abcdef")) {
					t.Fatal("lost short-read/EINTR progress")
				}
			} else {
				_ = regularFailed(t, got, err)
			}
			if closes != 1 || (kind != "short_reads" && injected == 0) {
				t.Fatalf("read seam/close not reached: %d/%d", injected, closes)
			}
			if strings.Contains(kind, "eio") || kind == "error_positive_count" {
				if !errors.Is(err, unix.EIO) {
					t.Fatal("lost EIO")
				}
			}
			if kind == "eintr_positive_count" && !errors.Is(err, unix.EINTR) {
				t.Fatal("lost invalid-count EINTR")
			}
			if kind == "repeated_eintr_cancel" && (!errors.Is(err, context.Canceled) || hits != 3) {
				t.Fatalf("unchecked retry: hits=%d %v", hits, err)
			}
		}
	}
	runRegularInventory(
		t,
		[]string{
			"short_reads",
			"eintr",
			"eintr_after_progress",
			"probe_eintr",
			"read_eio",
			"probe_eio",
			"negative_count",
			"oversized_count",
			"eintr_positive_count",
			"error_positive_count",
			"repeated_eintr_cancel",
		},
		cases,
	)
}

func testRegularFailureInventory(t *testing.T) {
	runRegularInventory(
		t,
		[]string{"phases", "native_open_causes", "invalid_open_count"},
		map[string]func(*testing.T){
			"phases": testRegularNativeFailures,
			"native_open_causes": func(t *testing.T) {
				for _, cause := range []error{unix.ENOSYS, unix.ENOTSUP, unix.EOPNOTSUPP, unix.EINVAL, unix.EINTR, unix.EIO} {
					_, d, name := regularFixture(t, nil)
					calls := systemRegularCalls()
					hits := 0
					calls.openat = func(int, string, int, uint32) (int, error) { hits++; return -1, cause }
					calls.close = func(int) error { t.Fatal("closed failed-open descriptor"); return nil }
					got, err := d.observeRegular(t.Context(), name, calls)
					e := regularFailed(t, got, err)
					want := ReasonIO
					if errors.Is(cause, unix.EINVAL) {
						want = ReasonInvalidOperation
					} else if errors.Is(cause, unix.ENOSYS) || errors.Is(cause, unix.ENOTSUP) || errors.Is(cause, unix.EOPNOTSUPP) {
						want = ReasonUnsupported
					}
					if hits != 1 || !errors.Is(err, cause) || e.Reason != want {
						t.Fatalf("native cause/fallback: %d %v", hits, err)
					}
				}
			},
			"invalid_open_count": func(t *testing.T) {
				_, d, name := regularFixture(t, nil)
				calls := systemRegularCalls()
				hits := 0
				calls.openat = func(int, string, int, uint32) (int, error) { hits++; return -1, nil }
				calls.close = func(int) error { t.Fatal("closed invalid negative descriptor"); return nil }
				got, err := d.observeRegular(t.Context(), name, calls)
				_ = regularFailed(t, got, err)
				if hits != 1 {
					t.Fatal("retried invalid open")
				}
			},
		},
	)
}

func testRegularNativeFailures(t *testing.T) {
	cases := map[string]func(*testing.T){}
	for _, phase := range []string{"guard_fd", "guard_edge", "baseline", "open", "fd_before_read", "name_before_read", "fd_after_read", "name_after_read", "final_guard", "malformed_baseline", "malformed_fd"} {
		cases[phase] = func(t *testing.T) {
			path, d, name := regularFixture(t, []byte("failure"))
			calls := systemRegularCalls()
			stat, statat, open, read, closeFD := calls.stat, calls.statat, calls.openat, calls.read, calls.close
			owned, hits, opens, reads, closes, names := -1, 0, 0, 0, 0, 0
			calls.openat = func(fd int, n string, f int, m uint32) (int, error) {
				opens++
				if phase == "open" {
					hits++
					return -1, unix.EACCES
				}
				var err error
				owned, err = open(fd, n, f, m)
				return owned, err
			}
			calls.read = func(fd int, b []byte) (int, error) { reads++; return read(fd, b) }
			calls.stat = func(fd int, st *unix.Stat_t) error {
				if hits == 0 &&
					(phase == "guard_fd" && fd == d.state.nodes[0].fd || phase == "fd_before_read" && fd == owned || phase == "fd_after_read" && fd == owned && reads > 0 || phase == "final_guard" && fd == d.state.nodes[0].fd && reads > 0) {
					hits++
					return unix.EIO
				}
				err := stat(fd, st)
				if err == nil && phase == "malformed_fd" && fd == owned {
					hits++
					st.Size = -1
				}
				return err
			}
			calls.statat = func(fd int, n string, st *unix.Stat_t, f int) error {
				if n == name.name {
					names++
				}
				if hits == 0 &&
					(phase == "guard_edge" && n == "/" || phase == "baseline" && n == name.name || phase == "name_before_read" && n == name.name && names == 2 || phase == "name_after_read" && n == name.name && reads > 0) {
					hits++
					return unix.EIO
				}
				err := statat(fd, n, st, f)
				if err == nil && phase == "malformed_baseline" && n == name.name {
					hits++
					st.Size = -1
				}
				return err
			}
			calls.close = func(fd int) error { closes++; return closeFD(fd) }
			got, err := d.observeRegular(t.Context(), name, calls)
			e := regularFailed(t, got, err)
			if hits != 1 || closes != regularOwnedCount(owned) {
				t.Fatalf(
					"wrong seam or FD ownership: hits=%d open=%d closes=%d owned=%d: %v",
					hits,
					opens,
					closes,
					owned,
					err,
				)
			}
			if phase == "open" {
				if !errors.Is(err, unix.EACCES) || opens != 1 {
					t.Fatal(err)
				}
			} else if !strings.HasPrefix(phase, "malformed") && !errors.Is(err, unix.EIO) {
				t.Fatal("lost stat cause")
			}
			if (strings.Contains(phase, "before_read") || phase == "malformed_fd") && reads != 0 {
				t.Fatal("read before binding")
			}
			if phase != "guard_fd" && phase != "guard_edge" && phase != "final_guard" &&
				e.Path != path {
				t.Fatalf("lost payload path: %v", err)
			}
		}
	}
	runRegularInventory(
		t,
		[]string{
			"guard_fd",
			"guard_edge",
			"baseline",
			"open",
			"fd_before_read",
			"name_before_read",
			"fd_after_read",
			"name_after_read",
			"final_guard",
			"malformed_baseline",
			"malformed_fd",
		},
		cases,
	)
}

func regularOwnedCount(fd int) int {
	if fd >= 0 {
		return 1
	}
	return 0
}

func regularWait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("bounded regular-observation handshake timed out")
		var zero T
		return zero
	}
}

func testRegularCheckpoints(t *testing.T) {
	cases := map[string]func(*testing.T){
		"nil_context": func(t *testing.T) {
			_, d, name := regularFixture(t, nil)
			//nolint:staticcheck // SA1012: exercise the required nil-context refusal.
			got, err := d.ObserveRegular(nil, name)
			if regularFailed(t, got, err).Reason != ReasonInvalidOperation {
				t.Fatal(err)
			}
		},
		"pre_cancelled": func(t *testing.T) {
			_, d, name := regularFixture(t, nil)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			calls := systemRegularCalls()
			calls.stat = func(int, *unix.Stat_t) error { t.Fatal("I/O after initial cancellation"); return nil }
			got, err := d.observeRegular(ctx, name, calls)
			_ = regularFailed(t, got, err)
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		},
		"deadline": func(t *testing.T) {
			_, d, name := regularFixture(t, nil)
			ctx, cancel := context.WithDeadline(t.Context(), time.Unix(1, 0))
			defer cancel()
			got, err := d.ObserveRegular(ctx, name)
			_ = regularFailed(t, got, err)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
		},
	}
	for _, phase := range []string{"guard", "baseline", "open", "fd", "name", "read", "probe", "final_stat", "final_guard", "close"} {
		for _, stop := range []string{"cancel", "parent_close"} {
			cases[phase+"_"+stop] = func(t *testing.T) { regularCheckpoint(t, phase, stop == "parent_close") }
		}
	}
	runRegularInventory(
		t,
		[]string{
			"nil_context",
			"pre_cancelled",
			"deadline",
			"guard_cancel",
			"guard_parent_close",
			"baseline_cancel",
			"baseline_parent_close",
			"open_cancel",
			"open_parent_close",
			"fd_cancel",
			"fd_parent_close",
			"name_cancel",
			"name_parent_close",
			"read_cancel",
			"read_parent_close",
			"probe_cancel",
			"probe_parent_close",
			"final_stat_cancel",
			"final_stat_parent_close",
			"final_guard_cancel",
			"final_guard_parent_close",
			"close_cancel",
			"close_parent_close",
		},
		cases,
	)
}

func regularCheckpoint(t *testing.T, phase string, parentClose bool) {
	_, d, name := regularFixture(t, []byte("checkpoint"))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := systemRegularCalls()
	stat, statat, open, read, closeFD := calls.stat, calls.statat, calls.openat, calls.read, calls.close
	owned, hits, reads, names, closes := -1, 0, 0, 0, 0
	stopped := false
	var closeDone chan error
	assertRunning := func(op string) {
		t.Helper()
		if stopped {
			t.Fatalf("%s started after %s checkpoint stop (parentClose=%t)", op, phase, parentClose)
		}
	}
	stop := func() {
		if stopped {
			t.Fatal("checkpoint stop invoked twice")
		}
		stopped = true
		hits++
		if parentClose {
			closeDone = make(chan error, 1)
			signal := closingSignal(d.state)
			go func() { closeDone <- d.Close() }()
			regularWait(t, signal)
		} else {
			cancel()
		}
	}
	calls.openat = func(fd int, n string, f int, m uint32) (int, error) {
		assertRunning("openat")
		var err error
		owned, err = open(fd, n, f, m)
		if phase == "open" && err == nil {
			regularOpenHook(t, owned, stop)
		}
		return owned, err
	}
	calls.stat = func(fd int, st *unix.Stat_t) error {
		assertRunning("fstat")
		err := stat(fd, st)
		if hits == 0 &&
			(phase == "guard" && fd == d.state.nodes[0].fd || phase == "fd" && fd == owned || phase == "final_stat" && fd == owned && reads > 0 || phase == "final_guard" && fd == d.state.nodes[0].fd && reads > 0) {
			stop()
		}
		return err
	}
	calls.statat = func(fd int, n string, st *unix.Stat_t, f int) error {
		assertRunning("fstatat")
		err := statat(fd, n, st, f)
		if n == name.name {
			names++
			if hits == 0 && (phase == "baseline" && names == 1 || phase == "name" && names == 2) {
				stop()
			}
		}
		return err
	}
	calls.read = func(fd int, b []byte) (int, error) {
		assertRunning("read")
		reads++
		n, err := read(fd, b)
		if hits == 0 && (phase == "read" || phase == "probe" && n == 0) {
			stop()
		}
		return n, err
	}
	calls.close = func(fd int) error {
		closes++
		err := closeFD(fd)
		if phase == "close" {
			stop()
		}
		lifecycleMu.Lock()
		active := d.state.active
		lifecycleMu.Unlock()
		if active != 1 {
			t.Errorf("parent lease dropped before cleanup: %d", active)
		}
		return err
	}
	// If an assertion aborts inside a seam, unwind owned resources before fixture cleanup.
	t.Cleanup(func() {
		if closeDone != nil {
			must(t, regularWait(t, closeDone))
		}
	})
	got, err := d.observeRegular(ctx, name, calls)
	e := regularFailed(t, got, err)
	if hits != 1 || closes != regularOwnedCount(owned) {
		t.Fatalf("checkpoint not reached/cleanup wrong: %s %d/%d", phase, hits, closes)
	}
	if parentClose {
		if e.Reason != ReasonClosed {
			t.Fatal(err)
		}
	} else if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func testRegularConcurrent(t *testing.T) {
	runRegularInventory(
		t,
		[]string{
			"independent_offsets",
			"copied_parent_close",
			"close_during_cleanup",
			"publication_mutex",
			"publication_wins",
		},
		map[string]func(*testing.T){
			"independent_offsets":  testRegularIndependentOffsets,
			"publication_mutex":    testRegularPublicationMutex,
			"copied_parent_close":  func(t *testing.T) { regularConcurrentClose(t, false) },
			"close_during_cleanup": func(t *testing.T) { regularConcurrentClose(t, true) },
			"publication_wins": func(t *testing.T) {
				_, d, name := regularFixture(t, []byte("published"))
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				got, err := d.ObserveRegular(ctx, name)
				must(t, err)
				saved := got
				cancel()
				copyDir := *d
				must(t, copyDir.Close())
				if !got.Valid() || got != saved {
					t.Fatal("later cancellation revoked published value")
				}
			},
		},
	)
}

// This context checks only the final publication Err call, after owned close.
// It performs no I/O or waits, and it never changes a global function hook.
type regularPublicationContext struct {
	context.Context
	afterClose bool
	checked    bool
	held       bool
}

func (c *regularPublicationContext) Err() error {
	if c.afterClose {
		c.checked = true
		c.held = !lifecycleMu.TryLock()
		if !c.held {
			lifecycleMu.Unlock()
		}
	}
	return c.Context.Err()
}

func testRegularPublicationMutex(t *testing.T) {
	_, d, name := regularFixture(t, []byte("serialized"))
	calls := systemRegularCalls()
	closeFD := calls.close
	ctx := &regularPublicationContext{Context: t.Context()}
	calls.close = func(fd int) error { err := closeFD(fd); ctx.afterClose = true; return err }
	got, err := d.observeRegular(ctx, name, calls)
	must(t, err)
	if !got.Valid() || !ctx.checked || !ctx.held {
		t.Fatalf("final context/publication not serialized: %+v", ctx)
	}
}

func testRegularIndependentOffsets(t *testing.T) {
	data := bytes.Repeat([]byte("different offsets"), 5000)
	_, d, name := regularFixture(t, data)
	copyDir := *d
	entered := make(chan int, 2)
	proceed := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(proceed) })
	type outcome struct {
		got RegularObservation
		err error
	}
	done := make(chan outcome, 2)
	// Register unblock and join before starting either goroutine. Also join before
	// returning, since testing cancels t.Context before running cleanup callbacks.
	joined := 0
	join := func() {
		unblock()
		for joined < 2 {
			o := regularWait(t, done)
			joined++
			if o.err != nil {
				t.Error(o.err)
			}
			if o.err == nil && !o.got.Valid() {
				t.Error("concurrent result invalid")
			}
		}
	}
	t.Cleanup(join)
	for _, parent := range []*Dir{d, &copyDir} {
		calls := systemRegularCalls()
		read := calls.read
		first := true
		calls.read = func(fd int, b []byte) (int, error) {
			if first {
				first = false
				entered <- fd
				<-proceed
			}
			return read(fd, b[:min(len(b), 7)])
		}
		go func() {
			got, err := parent.observeRegular(t.Context(), name, calls)
			if err == nil && got.SHA256() != sha256.Sum256(data) {
				err = errors.New("shared read offset or corrupt digest")
			}
			done <- outcome{got, err}
		}()
	}
	a, b := regularWait(t, entered), regularWait(t, entered)
	if a == b {
		t.Fatal("concurrent calls did not open distinct descriptors")
	}
	lifecycleMu.Lock()
	active := d.state.active
	lifecycleMu.Unlock()
	if active != 2 {
		t.Fatalf("expected exactly one lease per call: %d", active)
	}
	join()
}

func regularConcurrentClose(t *testing.T, atCleanup bool) {
	_, d, name := regularFixture(t, []byte("blocked"))
	copyDir := *d
	calls := systemRegularCalls()
	read, closeFD := calls.read, calls.close
	entered := make(chan struct{})
	proceed := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(proceed) })
	obsDone := make(chan error, 1)
	closeDone := make(chan error, 1)
	closes := 0
	block := func() { close(entered); <-proceed }
	if atCleanup {
		calls.close = func(fd int) error { closes++; block(); return closeFD(fd) }
	} else {
		first := true
		calls.read = func(fd int, b []byte) (int, error) {
			if first {
				first = false
				block()
			}
			return read(fd, b)
		}
		calls.close = func(fd int) error { closes++; return closeFD(fd) }
	}
	startedClose, joinedObservation, joinedClose := false, false, false
	join := func() {
		unblock()
		if !joinedObservation {
			err := regularWait(t, obsDone)
			joinedObservation = true
			var e *RegularObservationError
			if !errors.As(err, &e) || e.Reason != ReasonClosed {
				t.Errorf("publication beat already-closing parent: %v", err)
			}
		}
		if startedClose && !joinedClose {
			err := regularWait(t, closeDone)
			joinedClose = true
			must(t, err)
		}
		if closes != 1 {
			t.Errorf("owned cleanup count=%d", closes)
		}
	}
	t.Cleanup(join)
	go func() {
		got, err := d.observeRegular(t.Context(), name, calls)
		if got != (RegularObservation{}) {
			err = errors.New("published while parent closing")
		}
		obsDone <- err
	}()
	regularWait(t, entered)
	signal := closingSignal(d.state)
	startedClose = true
	go func() { closeDone <- copyDir.Close() }()
	regularWait(t, signal)
	select {
	case err := <-closeDone:
		joinedClose = true
		t.Fatalf("Close finished before raw FD cleanup: %v", err)
	default:
	}
	lifecycleMu.Lock()
	active := d.state.active
	lifecycleMu.Unlock()
	if active != 1 {
		t.Fatalf("lease not retained during synchronous work/cleanup: %d", active)
	}
	var st unix.Stat_t
	must(t, unix.Fstat(d.state.leaf().fd, &st))
	join()
}

func testRegularCleanup(t *testing.T) {
	runRegularInventory(
		t,
		[]string{
			"close_failure",
			"joined_read_close",
			"joined_context_close",
			"fd_zero",
			"open_failure_no_close",
			"reused_number_not_reclosed",
		},
		map[string]func(*testing.T){
			"close_failure":        func(t *testing.T) { regularCloseFailure(t, "close") },
			"joined_read_close":    func(t *testing.T) { regularCloseFailure(t, "read") },
			"joined_context_close": func(t *testing.T) { regularCloseFailure(t, "context") },
			"fd_zero":              testRegularFDZero,
			"open_failure_no_close": func(t *testing.T) {
				_, d, name := regularFixture(t, nil)
				calls := systemRegularCalls()
				opens := 0
				calls.openat = func(int, string, int, uint32) (int, error) { opens++; return -1, unix.EIO }
				calls.close = func(int) error { t.Fatal("closed unowned FD"); return nil }
				got, err := d.observeRegular(t.Context(), name, calls)
				_ = regularFailed(t, got, err)
				if opens != 1 || !errors.Is(err, unix.EIO) {
					t.Fatal(err)
				}
			},
			"reused_number_not_reclosed": func(t *testing.T) {
				path, d, name := regularFixture(t, []byte("original"))
				calls := systemRegularCalls()
				closeFD := calls.close
				closes, replacement := 0, -1
				t.Cleanup(func() {
					if replacement >= 0 {
						must(t, unix.Close(replacement))
					}
				})
				calls.close = func(fd int) error {
					closes++
					if closes != 1 {
						t.Fatal("retry could close reused descriptor")
					}
					must(t, closeFD(fd))
					var err error
					replacement, err = unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
					must(t, err)
					return unix.EINTR
				}
				got, err := d.observeRegular(t.Context(), name, calls)
				_ = regularFailed(t, got, err)
				if closes != 1 || !errors.Is(err, unix.EINTR) {
					t.Fatal(err)
				}
				var st unix.Stat_t
				must(t, unix.Fstat(replacement, &st))
			},
		},
	)
}

func regularCloseFailure(t *testing.T, which string) {
	path, d, name := regularFixture(t, []byte("cleanup"))
	calls := systemRegularCalls()
	closeFD, read := calls.close, calls.read
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	closes := 0
	if which == "read" {
		calls.read = func(int, []byte) (int, error) { return -1, unix.EIO }
	}
	if which == "context" {
		calls.read = func(fd int, b []byte) (int, error) { n, err := read(fd, b); cancel(); return n, err }
	}
	calls.close = func(fd int) error {
		closes++
		lifecycleMu.Lock()
		active := d.state.active
		lifecycleMu.Unlock()
		if active != 1 {
			t.Errorf("cleanup lost parent lease: %d", active)
		}
		must(t, closeFD(fd))
		return unix.EINTR
	}
	got, err := d.observeRegular(ctx, name, calls)
	e := regularFailed(t, got, err)
	if closes != 1 || !errors.Is(err, unix.EINTR) || e.Path != path {
		t.Fatalf("close cause/count/path lost: %d %v", closes, err)
	}
	if which == "read" && !errors.Is(err, unix.EIO) ||
		which == "context" && !errors.Is(err, context.Canceled) {
		t.Fatal("lost joined primary cause")
	}
	lifecycleMu.Lock()
	active := d.state.active
	lifecycleMu.Unlock()
	if active != 0 {
		t.Fatalf("parent lease leaked: %d", active)
	}
	_, err = d.Identity()
	must(t, err)
}

func testRegularFDZero(t *testing.T) {
	path, d, name := regularFixture(t, nil)
	calls := systemRegularCalls()
	stat := calls.stat
	var payload unix.Stat_t
	must(t, unix.Lstat(path, &payload))
	reads, closes := 0, 0
	calls.openat = func(int, string, int, uint32) (int, error) { return 0, nil }
	// Every possible operation on simulated zero is intercepted; stdin is untouched.
	calls.stat = func(fd int, st *unix.Stat_t) error {
		if fd == 0 {
			*st = payload
			return nil
		}
		return stat(fd, st)
	}
	calls.read = func(fd int, b []byte) (int, error) {
		if fd != 0 || len(b) != 1 {
			t.Fatal("wrong simulated read")
		}
		reads++
		return 0, nil
	}
	calls.close = func(fd int) error {
		if fd != 0 {
			t.Fatal("wrong simulated close")
		}
		closes++
		return nil
	}
	got, err := d.observeRegular(t.Context(), name, calls)
	must(t, err)
	if !got.Valid() || reads != 1 || closes != 1 {
		t.Fatalf("zero FD ownership=%d/%d %+v", reads, closes, got)
	}
}

func testRegularAPI(t *testing.T) {
	runRegularInventory(
		t,
		[]string{"atime_excluded", "immutable_values", "exact_api", "unavailable_compile_contract"},
		map[string]func(*testing.T){
			"atime_excluded": func(t *testing.T) {
				path, d, name := regularFixture(t, []byte("atime"))
				calls := systemRegularCalls()
				stat, statat := calls.stat, calls.statat
				hits := 0
				var st unix.Stat_t
				must(t, unix.Lstat(path, &st))
				base, err := regularStatMetadata(&st)
				must(t, err)
				st.Atim.Sec++
				st.Atim.Nsec = -1 // Excluded even when a counterfactual atime is malformed.
				other, err := regularStatMetadata(&st)
				must(t, err)
				if base != other {
					t.Fatal("atime entered metadata")
				}
				alter := func(st *unix.Stat_t) { hits++; st.Atim.Sec++ }
				calls.stat = func(fd int, st *unix.Stat_t) error {
					err := stat(fd, st)
					if err == nil {
						alter(st)
					}
					return err
				}
				calls.statat = func(fd int, n string, st *unix.Stat_t, f int) error {
					err := statat(fd, n, st, f)
					if err == nil {
						alter(st)
					}
					return err
				}
				got, err := d.observeRegular(t.Context(), name, calls)
				must(t, err)
				if !got.Valid() || hits == 0 {
					t.Fatal("atime seam not reached")
				}
			},
			"immutable_values": func(t *testing.T) {
				_, d, name := regularFixture(t, []byte("immutable"))
				got, err := d.ObserveRegular(t.Context(), name)
				must(t, err)
				saved := got
				id, digest := got.Identity(), got.SHA256()
				id.Inode++
				digest[0]++
				if got != saved || got.Identity() == id || got.SHA256() == digest ||
					(RegularObservation{}).Valid() {
					t.Fatal("mutable or valid-zero observation")
				}
				ms, mn := got.Mtime()
				cs, cn := got.Ctime()
				if ms != got.metadata.mtimeSec || mn != got.metadata.mtimeNsec ||
					cs != got.metadata.ctimeSec ||
					cn != got.metadata.ctimeNsec {
					t.Fatal("timestamp getter changed exact pairs")
				}
			},
			"exact_api": func(t *testing.T) {
				typ := reflect.TypeFor[RegularObservation]()
				for i := range typ.NumField() {
					if typ.Field(i).IsExported() {
						t.Fatalf("exported mutable field: %s", typ.Field(i).Name)
					}
				}
				want := map[string]bool{
					"Valid":     true,
					"Identity":  true,
					"UID":       true,
					"GID":       true,
					"Mode":      true,
					"LinkCount": true,
					"Size":      true,
					"Mtime":     true,
					"Ctime":     true,
					"SHA256":    true,
				}
				if typ.NumMethod() != len(want) {
					t.Fatalf("unexpected observation API: %v", typ)
				}
				for i := range typ.NumMethod() {
					if !want[typ.Method(i).Name] {
						t.Fatal("unexpected observation method")
					}
				}
				method, ok := reflect.TypeFor[*Dir]().MethodByName("ObserveRegular")
				if !ok ||
					method.Type != reflect.TypeOf(
						func(*Dir, context.Context, Component) (RegularObservation, error) { return RegularObservation{}, nil },
					) {
					t.Fatal("unexpected operation API")
				}
			},
			"unavailable_compile_contract": func(t *testing.T) {
				// This common value/signature also compiles in regular_observation_unavailable_test.go.
				// The existing TestUnsupportedBuildTargets compiles that test for FreeBSD/Windows;
				// this leaf does not launch tools or mistake compilation for native execution.
				//nolint:staticcheck // QF1011: the explicit type checks the cross-target API signature.
				var observeRegular func(*Dir, context.Context, Component) (RegularObservation, error) = (*Dir).ObserveRegular
				if observeRegular == nil ||
					reflect.TypeFor[RegularObservation]().Kind() != reflect.Struct {
					t.Fatal("missing cross-target value API")
				}
			},
		},
	)
}
