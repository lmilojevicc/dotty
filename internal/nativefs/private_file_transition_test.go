//go:build (linux || (darwin && cgo)) && !android && !ios

package nativefs

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// These regressions precede the APFS file-create production transition. Model
// injection below tests comparison only; only this native case proves support.
func TestPrivateFileCreateTransitionNative(t *testing.T) {
	r, d, calls := privateFixture(t)
	before, err := privateParent(d.state, RolePrivateAnchor, calls)
	must(t, err)
	parentBefore := before[len(before)-1]
	switch parentBefore.Filesystem().Model() {
	case "darwin-local-apfs-ownership-enforced", "linux-ext-posix-acl", "linux-tmpfs-posix-acl":
	default:
		t.Fatalf("unexpected native model: %+v", parentBefore)
	}
	if parentBefore.Filesystem().State() != EvidencePresent {
		t.Fatal("native filesystem evidence unverified")
	}
	open := calls.acquisition.openat
	opens, creates, chmods, fileReads := 0, 0, 0, 0
	var created Identity
	calls.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
		opens++
		if fd != d.state.leaf().fd || name != "lock" ||
			flags != lockFileFlags|unix.O_CREAT|unix.O_EXCL || mode != 0o600 {
			t.Fatalf("not exact exclusive file creation: %d %q %#x %#o", fd, name, flags, mode)
		}
		opened, err := open(fd, name, flags, mode)
		if err == nil {
			creates++
			created, err = statFD(opened)
			must(t, err)
			bound, statErr := statEntry(fd, component(t, name))
			must(t, statErr)
			if created.Kind != unix.S_IFREG || bound != created {
				t.Fatal("created descriptor not bound regular file")
			}
		}
		return opened, err
	}
	security := calls.authority.security
	calls.authority.security = func(fd int, directory bool) (SecurityEvidence, error) {
		if !directory {
			fileReads++
		}
		return security(fd, directory)
	}
	calls.chmod = func(fd int, mode uint32) error {
		chmods++
		if fileReads != 2 || mode != 0o600 {
			t.Fatalf("pre-chmod file observations=%d mode=%#o", fileReads, mode)
		}
		id, err := statFD(fd)
		must(t, err)
		if id != created {
			t.Fatal("chmod not on exclusive FD")
		}
		return unix.Fchmod(fd, mode)
	}
	f, record, err := d.lockFile(component(t, "lock"), true, calls)
	must(t, err)
	if f == nil {
		t.Fatal("successful creation did not return a File")
	}
	must(t, f.Close())
	after, err := privateParent(d.state, RolePrivateAnchor, calls)
	must(t, err)
	parentAfter := after[len(after)-1]
	t.Logf(
		"native exclusive regular-file create model=%q evidence=%d parent identity=%+v before=%+v after=%+v nlink=%d->%d; observation window, NOT causal proof",
		parentBefore.Filesystem().Model(),
		parentBefore.Filesystem().State(),
		parentBefore.Identity(),
		parentBefore,
		parentAfter,
		parentBefore.LinkCount(),
		parentAfter.LinkCount(),
	)
	must(t, compareFileCreateTransition(d.state.nodes, before, after))
	if parentBefore.Filesystem().Model() != "darwin-local-apfs-ownership-enforced" {
		must(t, compareAuthorityNodes(d.state.nodes, RolePrivateAnchor, before, after))
	}
	if opens != 1 || creates != 1 || chmods != 1 || fileReads != 4 ||
		!record.Created() || record.Path() != filepath.Join(r, "lock") || record.Kind() != unix.S_IFREG {
		t.Fatalf(
			"creation/open/chmod/read counts=%d/%d/%d/%d record=%+v",
			creates,
			opens,
			chmods,
			fileReads,
			record,
		)
	}
	entries, err := os.ReadDir(r)
	must(t, err)
	if len(entries) != 1 || entries[0].Name() != "lock" || !entries[0].Type().IsRegular() {
		t.Fatal("not exactly one regular file creation")
	}
	foundBefore, foundAfter, foundFile := 0, 0, 0
	for _, o := range record.Observations() {
		if o.Path() == r && o.Phase() == "before-file-create" && o.Facts() == parentBefore {
			foundBefore++
		}
		if o.Path() == r && o.Phase() == "after-file-create" && o.Facts() == parentAfter {
			foundAfter++
		}
		if o.Path() == record.Path() && o.Phase() == "opened-file" && o.Identity() == created {
			foundFile++
		}
	}
	if foundBefore != 1 || foundAfter != 1 || foundFile != 1 {
		t.Fatalf(
			"actual creation observations before/after/file=%d/%d/%d",
			foundBefore,
			foundAfter,
			foundFile,
		)
	}
}

func TestPrivateFileCreateTransitionComparison(t *testing.T) {
	r, _, calls := privateFixture(t)
	mkdir(t, filepath.Join(r, "parent"))
	d := openDir(t, filepath.Join(r, "parent"))
	native, err := privateParent(d.state, RolePrivateAnchor, calls)
	must(t, err)
	last := len(native) - 1
	// Explicitly synthetic model selection, never positive native evidence.
	for _, model := range []string{"darwin-local-apfs-ownership-enforced", "linux-ext-posix-acl", "linux-tmpfs-posix-acl", "unknown", ""} {
		for _, state := range []EvidenceState{EvidenceInvalid, EvidenceAbsent, EvidencePresent, EvidenceUnsupported, EvidenceFailed} {
			before := append([]AuthorityFacts(nil), native...)
			before[last].filesystem.model, before[last].filesystem.state = model, state
			for _, count := range []uint64{0, 1, before[last].nlink + 7, ^uint64(0)} {
				after := append([]AuthorityFacts(nil), before...)
				after[last].nlink = count
				originalBefore, originalAfter := append(
					[]AuthorityFacts(nil),
					before...), append(
					[]AuthorityFacts(nil),
					after...)
				err := compareFileCreateTransition(d.state.nodes, before, after)
				allowed := count == before[last].nlink ||
					state == EvidencePresent && model == "darwin-local-apfs-ownership-enforced"
				if allowed {
					must(t, err)
				} else {
					e := authorityReason(t, err, ReasonAuthorityChanged, d.state.leaf().path)
					if e.Role != RolePrivateAnchor || e.Err != nil || e.Expected != before[last] ||
						e.Observed != after[last] {
						t.Fatalf("wrong model refusal: %+v", e)
					}
				}
				if !reflect.DeepEqual(before, originalBefore) ||
					!reflect.DeepEqual(after, originalAfter) {
					t.Fatal("transition rewrote actual observations")
				}
				if count != before[last].nlink {
					_ = authorityReason(
						t,
						compareAuthorityNodes(d.state.nodes, RolePrivateAnchor, before, after),
						ReasonAuthorityChanged,
						d.state.leaf().path,
					)
				}
				t.Logf(
					"injected comparison model=%q state=%d nlink=%d->%d allowed=%t; not native/causal evidence",
					model,
					state,
					before[last].nlink,
					count,
					allowed,
				)
			}
		}
	}
	t.Run("NonDirectoryParent", func(t *testing.T) {
		before := append([]AuthorityFacts(nil), native...)
		before[last].filesystem.model = "darwin-local-apfs-ownership-enforced"
		before[last].filesystem.state = EvidencePresent
		before[last].identity.Kind = unix.S_IFREG
		after := append([]AuthorityFacts(nil), before...)
		after[last].nlink++
		e := authorityReason(
			t,
			compareFileCreateTransition(d.state.nodes, before, after),
			ReasonAuthorityChanged,
			d.state.leaf().path,
		)
		if e.Role != RolePrivateAnchor || e.Expected != before[last] || e.Observed != after[last] {
			t.Fatalf("non-directory count exception: %+v", e)
		}
	})
	fields := map[string]func(*AuthorityFacts){
		"identity-valid":     func(f *AuthorityFacts) { f.identity.Valid = !f.identity.Valid },
		"identity-device":    func(f *AuthorityFacts) { f.identity.Device++ },
		"identity-inode":     func(f *AuthorityFacts) { f.identity.Inode++ },
		"identity-kind":      func(f *AuthorityFacts) { f.identity.Kind = unix.S_IFREG },
		"uid":                func(f *AuthorityFacts) { f.uid++ },
		"gid":                func(f *AuthorityFacts) { f.gid++ },
		"mode":               func(f *AuthorityFacts) { f.mode ^= 0o100 },
		"filesystem-state":   func(f *AuthorityFacts) { f.filesystem.state = EvidenceUnsupported },
		"filesystem-model":   func(f *AuthorityFacts) { f.filesystem.model += "-drift" },
		"filesystem-kind":    func(f *AuthorityFacts) { f.filesystem.kind++ },
		"filesystem-id":      func(f *AuthorityFacts) { f.filesystem.id += "-drift" },
		"mount-flags":        func(f *AuthorityFacts) { f.filesystem.flags ^= 1 },
		"security-state":     func(f *AuthorityFacts) { f.security.state = EvidencePresent },
		"security-mechanism": func(f *AuthorityFacts) { f.security.mechanism += "-drift" },
		"nlink":              func(f *AuthorityFacts) { f.nlink++ },
	}
	for _, scope := range []string{"parent", "higher-ancestor"} {
		for field, drift := range fields {
			if scope == "parent" && field == "nlink" {
				continue // The only allowed field, covered with noncausal deltas above.
			}
			t.Run(scope+"/"+field, func(t *testing.T) {
				before := append([]AuthorityFacts(nil), native...)
				before[last].filesystem.model = "darwin-local-apfs-ownership-enforced"
				before[last].filesystem.state = EvidencePresent
				after := append([]AuthorityFacts(nil), before...)
				after[last].nlink += 7
				i, role := last, RolePrivateAnchor
				if scope == "higher-ancestor" {
					i, role = last-1, RoleSystemAncestor
				}
				drift(&after[i])
				e := authorityReason(
					t,
					compareFileCreateTransition(d.state.nodes, before, after),
					ReasonAuthorityChanged,
					d.state.nodes[i].path,
				)
				if e.Role != role || e.Err != nil || e.Expected != before[i] ||
					e.Observed != after[i] {
					t.Fatalf("lost exact drift facts/role: %+v", e)
				}
				// Also exercise the operation on its real native model. Mutate only
				// returned negative facts, consistently in both postcreation reads.
				root, _, calls := privateFixture(t)
				mkdir(t, filepath.Join(root, "parent"))
				parent := openDir(t, filepath.Join(root, "parent"))
				index := len(parent.state.nodes) - 1
				if scope == "higher-ancestor" {
					index--
				}
				observe := calls.observe
				observations, injections, creations, chmods := 0, 0, 0, 0
				var expected, observed AuthorityFacts
				calls.observe = func(nodes []pinnedDir, role AuthorityRole, readers authorityCalls) ([]AuthorityFacts, error) {
					facts, err := observe(nodes, role, readers)
					observations++
					if err == nil {
						if observations == 2 {
							expected = facts[index]
						}
						if observations == 3 || observations == 4 {
							drift(&facts[index])
							observed = facts[index]
							injections++
						}
					}
					return facts, err
				}
				open := calls.acquisition.openat
				calls.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
					opened, err := open(fd, name, flags, mode)
					if err == nil && flags&unix.O_EXCL != 0 {
						creations++
					}
					return opened, err
				}
				calls.chmod = func(int, uint32) error { chmods++; return unix.EIO }
				f, record, err := parent.lockFile(component(t, "lock"), true, calls)
				if f != nil {
					t.Fatalf("cross-creation field drift accepted (close: %v)", f.Close())
				}
				if observations != 4 || injections != 2 || creations != 1 || chmods != 0 {
					t.Fatalf(
						"cross-creation hook counts observations/injections/creations/chmods=%d/%d/%d/%d: %v",
						observations,
						injections,
						creations,
						chmods,
						err,
					)
				}
				retainedCreation(t, record, err, filepath.Join(root, "parent", "lock"))
				e = authorityReason(t, err, ReasonAuthorityChanged, parent.state.nodes[index].path)
				if e.Role != role || e.Op != "authority" || e.Err != nil ||
					e.Expected != expected ||
					e.Observed != observed {
					t.Fatalf("wrong operation field-drift diagnostic: %+v", e)
				}
			})
		}
	}
}

func TestPrivateFileCreateTransitionGuards(t *testing.T) {
	for _, phase := range []string{"nonregular-fd", "name-after-parent", "parent-during-observation"} {
		t.Run(phase, func(t *testing.T) {
			r, d, calls := privateFixture(t)
			path := filepath.Join(r, "lock")
			opens, creations, observations, injections, chmods := 0, 0, 0, 0, 0
			var original, winner Identity
			open := calls.acquisition.openat
			calls.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
				opens++
				opened, err := open(fd, name, flags, mode)
				if err == nil {
					creations++
					original, err = statFD(opened)
					must(t, err)
					if phase == "nonregular-fd" {
						// Negative syscall-result injection, not a native O_EXCL outcome.
						must(t, unix.Close(opened))
						opened, err = unix.Openat(fd, ".", directoryFlags, 0)
						must(t, err)
						winner, err = statFD(opened)
						must(t, err)
						injections++
					}
				}
				return opened, err
			}
			observe := calls.observe
			calls.observe = func(nodes []pinnedDir, role AuthorityRole, readers authorityCalls) ([]AuthorityFacts, error) {
				facts, err := observe(nodes, role, readers)
				observations++
				if err == nil && phase == "name-after-parent" && observations == 4 {
					must(t, os.Rename(path, filepath.Join(r, "saved")))
					write(t, path, "winner")
					winner, err = statEntry(d.state.leaf().fd, component(t, "lock"))
					must(t, err)
					injections++
				}
				if err == nil && phase == "parent-during-observation" && observations == 3 {
					original = d.state.leaf().identity
					must(t, os.Rename(r, r+"-saved"))
					t.Cleanup(func() { must(t, os.RemoveAll(r+"-saved")) })
					mkdir(t, r)
					var st unix.Stat_t
					must(t, unix.Lstat(r, &st))
					winner = statIdentity(&st)
					injections++
				}
				return facts, err
			}
			calls.chmod = func(int, uint32) error { chmods++; return unix.EIO }
			f, record, err := d.lockFile(component(t, "lock"), true, calls)
			if f != nil {
				t.Fatalf("creation guard bypassed (close: %v)", f.Close())
			}
			wantObservations, wantPath, wantOp, wantReason := 4, path, "bind-lock-file", ReasonIdentityChanged
			if phase == "nonregular-fd" {
				wantObservations, wantOp, wantReason = 2, "open-lock-file", ReasonTopology
			}
			if phase == "parent-during-observation" {
				wantObservations, wantPath, wantOp = 3, r, "private-parent"
			}
			if opens != 1 || creations != 1 || injections != 1 || chmods != 0 ||
				observations != wantObservations {
				t.Fatalf(
					"guard hook reachability opens/creations/injections/chmods/observations=%d/%d/%d/%d/%d: %v",
					opens,
					creations,
					injections,
					chmods,
					observations,
					err,
				)
			}
			retainedCreation(t, record, err, path)
			e := reason(t, err, wantReason)
			if e.Path != wantPath || e.Op != wantOp || e.Err != nil || e.Observed != winner ||
				phase != "nonregular-fd" && e.Expected != original {
				t.Fatalf("wrong creation guard diagnostic: %+v", e)
			}
			switch phase {
			case "name-after-parent":
				content(t, path, "winner")
				var st unix.Stat_t
				must(t, unix.Lstat(filepath.Join(r, "saved"), &st))
				if statIdentity(&st) != original {
					t.Fatal("original file not retained")
				}
			case "parent-during-observation":
				entries, readErr := os.ReadDir(r)
				must(t, readErr)
				if len(entries) != 0 {
					t.Fatal("replacement parent written")
				}
				var st unix.Stat_t
				must(t, unix.Lstat(filepath.Join(r+"-saved", "lock"), &st))
				if statIdentity(&st).Kind != unix.S_IFREG {
					t.Fatal("created file not retained under moved parent")
				}
			default:
				id, statErr := statEntry(d.state.leaf().fd, component(t, "lock"))
				must(t, statErr)
				if id != original {
					t.Fatal("created file changed after wrong-FD refusal")
				}
			}
		})
	}
}

func TestPrivateFileCreateTransitionPhases(t *testing.T) {
	for _, phase := range []string{"precreation", "postcreation-unstable", "later-prechmod", "later-postchmod", "open", "failedcreate"} {
		t.Run(phase, func(t *testing.T) {
			r, d, calls := privateFixture(t)
			path := filepath.Join(r, "lock")
			if phase == "open" || phase == "failedcreate" {
				write(t, path, "retained")
			}
			observe := calls.observe
			observations, injections, opens, creations, chmods := 0, 0, 0, 0, 0
			var expected, observed AuthorityFacts
			injectAt := 2
			switch phase {
			case "postcreation-unstable":
				injectAt = 4
			case "later-prechmod":
				injectAt = 5
			case "later-postchmod":
				injectAt = 7
			case "open":
				injectAt = 3
			case "failedcreate":
				injectAt = 0 // There must be no post-failure comparison or adoption.
			}
			calls.observe = func(nodes []pinnedDir, role AuthorityRole, readers authorityCalls) ([]AuthorityFacts, error) {
				facts, err := observe(nodes, role, readers)
				observations++
				if err == nil && observations == injectAt {
					if role != RolePrivateAnchor {
						t.Fatal("wrong parent role")
					}
					i := len(facts) - 1
					expected = facts[i]
					facts[i].nlink += 7
					observed = facts[i]
					injections++
				}
				return facts, err
			}
			open := calls.acquisition.openat
			calls.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
				opens++
				opened, err := open(fd, name, flags, mode)
				if err == nil && flags&unix.O_EXCL != 0 {
					creations++
				}
				return opened, err
			}
			calls.chmod = func(fd int, mode uint32) error { chmods++; return unix.Fchmod(fd, mode) }
			f, record, err := d.lockFile(component(t, "lock"), phase != "open", calls)
			if f != nil {
				t.Fatalf("out-of-phase drift accepted (close: %v)", f.Close())
			}
			wantOpens, wantCreations, wantChmods := 1, 1, 0
			if phase == "precreation" {
				wantOpens, wantCreations = 0, 0
			}
			if phase == "open" || phase == "failedcreate" {
				wantCreations = 0
				content(t, path, "retained")
			}
			if phase == "later-postchmod" {
				wantChmods = 1
			}
			if opens != wantOpens || creations != wantCreations || chmods != wantChmods ||
				record.Created() != (wantCreations == 1) {
				t.Fatalf(
					"wrong phase reachability: opens/creations/chmods=%d/%d/%d record=%+v err=%v",
					opens,
					creations,
					chmods,
					record,
					err,
				)
			}
			if phase == "failedcreate" {
				if observations != 2 || injections != 0 || !errors.Is(err, unix.EEXIST) {
					t.Fatalf(
						"failed create entered transition: observations=%d injections=%d err=%v",
						observations,
						injections,
						err,
					)
				}
				e := reason(t, err, ReasonExists)
				if e.Path != path || e.Op != "open-lock-file" || !errors.Is(e.Err, unix.EEXIST) {
					t.Fatalf("wrong failed-create diagnostic: %+v", e)
				}
				return
			}
			if observations != injectAt || injections != 1 {
				t.Fatalf(
					"phase injection not reached exactly: observations=%d injections=%d err=%v",
					observations,
					injections,
					err,
				)
			}
			e := authorityReason(t, err, ReasonAuthorityChanged, r)
			if e.Role != RolePrivateAnchor || e.Op != "authority" || e.Err != nil ||
				e.Expected != expected ||
				e.Observed != observed {
				t.Fatalf("wrong phase drift diagnostic: %+v", e)
			}
			if wantCreations == 1 {
				retainedCreation(t, record, err, path)
			} else if phase == "precreation" {
				if _, err := os.Lstat(path); !os.IsNotExist(err) {
					t.Fatalf("precreation refusal wrote: %v", err)
				}
			}
		})
	}
}
