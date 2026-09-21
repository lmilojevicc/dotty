//go:build (linux || (darwin && cgo)) && !android && !ios

package nativefs

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPrivateMkdirTransition(t *testing.T) {
	t.Run("NativeParentCounts", func(t *testing.T) {
		_, d, calls := privateFixture(t)
		before, err := readAuthorityFacts(d.state.leaf().fd, systemAuthorityCalls())
		must(t, err)
		child, record, err := d.privateDir(component(t, "anchor"), true, calls)
		must(t, err)
		must(t, child.Close())
		after, err := readAuthorityFacts(d.state.leaf().fd, systemAuthorityCalls())
		must(t, err)
		t.Logf(
			"native directory nlink before=%d after=%d (no causal claim for entire delta)",
			before.LinkCount(),
			after.LinkCount(),
		)
		foundBefore, foundAfter := false, false
		for _, o := range record.Observations() {
			if o.Path() == d.state.leaf().path {
				foundBefore = foundBefore || o.Facts() == before
				foundAfter = foundAfter || o.Facts() == after
			}
		}
		if !foundBefore || !foundAfter {
			t.Fatal("actual transition observations lost")
		}
	})
	// Injected counter models exercise comparison only, not native support or
	// evidence that Dotty caused a particular increment (including saturation).
	for _, count := range []uint64{0, 1, 2, ^uint64(0)} {
		r, d, _ := privateFixture(t)
		before, err := observeAuthorityNodes(
			d.state.nodes[len(d.state.nodes)-1:],
			RoleSystemAncestor,
			systemAuthorityCalls(),
		)
		must(t, err)
		after := append([]AuthorityFacts(nil), before...)
		after[0].nlink = count
		nodes := d.state.nodes[len(d.state.nodes)-1:]
		must(t, compareMkdirTransition(nodes, before, after))
		if before[0].nlink != count &&
			compareAuthorityNodes(nodes, RoleSystemAncestor, before, after) == nil {
			t.Fatal("31a full comparison weakened")
		}
		t.Logf("injected counter model at %s: %d -> %d", r, before[0].nlink, count)
	}
	for _, field := range []string{"identity", "uid", "gid", "mode", "security", "filesystem", "mount", "higher-nlink"} {
		t.Run(field, func(t *testing.T) {
			r, d, calls := privateFixture(t)
			mkdir(t, filepath.Join(r, "parent"))
			parent := openDir(t, filepath.Join(r, "parent"))
			observe := calls.observe
			created := false
			calls.mkdir = func(fd int, name string, mode uint32) error {
				err := unix.Mkdirat(fd, name, mode)
				created = err == nil
				return err
			}
			calls.observe = func(nodes []pinnedDir, role AuthorityRole, readers authorityCalls) ([]AuthorityFacts, error) {
				facts, err := observe(nodes, role, readers)
				if err == nil && created {
					i := len(d.state.nodes)
					switch field {
					case "identity":
						facts[i].identity.Inode++
					case "uid":
						facts[i].uid++
					case "gid":
						facts[i].gid++
					case "mode":
						facts[i].mode ^= 0o100
					case "security":
						facts[i].security.mechanism += "-drift"
					case "filesystem":
						facts[i].filesystem.id += "-drift"
					case "mount":
						facts[i].filesystem.flags ^= 1
					case "higher-nlink":
						facts[i-1].nlink++
					}
				}
				return facts, err
			}
			child, record, err := parent.privateDir(component(t, "anchor"), true, calls)
			if child != nil {
				t.Fatal("forbidden transition accepted")
			}
			retainedCreation(t, record, err, filepath.Join(r, "parent", "anchor"))
		})
	}
	for _, phase := range []string{"precreation", "later", "open", "filecreate", "failedmkdir"} {
		t.Run("NarrowScope/"+phase, func(t *testing.T) {
			r, d, calls := privateFixture(t)
			parentFacts, readErr := readAuthorityFacts(d.state.leaf().fd, systemAuthorityCalls())
			must(t, readErr)
			apfsFileCreate := phase == "filecreate" &&
				parentFacts.Filesystem().State() == EvidencePresent &&
				parentFacts.Filesystem().Model() == "darwin-local-apfs-ownership-enforced"
			if phase == "open" {
				mkdir(t, filepath.Join(r, "anchor"))
			}
			observe := calls.observe
			observations, mutations := 0, 0
			var injectedFileParent AuthorityFacts
			calls.observe = func(nodes []pinnedDir, role AuthorityRole, readers authorityCalls) ([]AuthorityFacts, error) {
				facts, err := observe(nodes, role, readers)
				observations++
				if err == nil {
					inject := observations == 2
					if phase == "later" {
						inject = observations >= 4
					}
					if phase == "filecreate" || phase == "failedmkdir" {
						inject = mutations != 0
					}
					if inject {
						facts[len(d.state.nodes)-1].nlink++
						if phase == "filecreate" {
							injectedFileParent = facts[len(d.state.nodes)-1]
						}
					}
				}
				return facts, err
			}
			calls.mkdir = func(fd int, name string, mode uint32) error {
				mutations++
				if phase == "failedmkdir" {
					return unix.EEXIST
				}
				return unix.Mkdirat(fd, name, mode)
			}
			open := calls.acquisition.openat
			calls.acquisition.openat = func(fd int, name string, flags int, mode uint32) (int, error) {
				if flags&unix.O_CREAT != 0 {
					mutations++
				}
				return open(fd, name, flags, mode)
			}
			var record CreationRecord
			var err error
			if phase == "filecreate" {
				var f *File
				f, record, err = d.lockFile(component(t, "anchor"), true, calls)
				if apfsFileCreate {
					must(t, err)
					if f == nil {
						t.Fatal("APFS creation did not return a File")
					}
					must(t, f.Close())
					if !record.Created() || record.Path() != filepath.Join(r, "anchor") ||
						mutations != 1 || observations != 8 {
						t.Fatalf(
							"APFS successful-creation phase not reached: record=%+v mutations=%d observations=%d",
							record,
							mutations,
							observations,
						)
					}
					return
				}
				if f != nil {
					t.Fatalf("non-APFS file parent count exception (close: %v)", f.Close())
				}
				if mutations != 1 || observations != 4 {
					t.Fatalf(
						"strict file-create comparison not reached: mutations=%d observations=%d err=%v",
						mutations,
						observations,
						err,
					)
				}
				e := authorityReason(t, err, ReasonAuthorityChanged, r)
				if e.Role != RolePrivateAnchor || e.Err != nil || e.Expected != parentFacts ||
					e.Observed != injectedFileParent {
					t.Fatalf("wrong injected non-APFS file-create refusal: %+v", e)
				}
			} else {
				var child *Dir
				child, record, err = d.privateDir(component(t, "anchor"), phase != "open", calls)
				if child != nil {
					t.Fatal("out-of-scope count exception")
				}
			}
			if err == nil {
				t.Fatal("drift/failure accepted")
			}
			if phase == "later" || phase == "filecreate" {
				retainedCreation(t, record, err, filepath.Join(r, "anchor"))
			} else if record.Created() {
				t.Fatal("invented creation event")
			}
			if phase == "precreation" && mutations != 0 {
				t.Fatal("precreation drift wrote")
			}
		})
	}
}

func TestPrivatePhysicalBoundary(t *testing.T) {
	r := fixture(t)
	root := filepath.Join(r, "private")
	mkdir(t, root)
	d := openDir(t, root)
	_, err := fixtureAuthority(d, root, RolePrivateAnchor, systemAuthorityCalls())
	must(t, err)
	calls := privateFixtureCalls(root)
	// An identity swap above the private security root remains physically
	// guarded. Move only a private fixture; never mutate a real system ancestor.
	must(t, os.Rename(r, r+"-moved"))
	t.Cleanup(func() { must(t, os.RemoveAll(r+"-moved")) })
	mkdir(t, root)
	f, record, err := d.lockFile(component(t, "lock"), true, calls)
	if f != nil || record.Created() || err == nil {
		t.Fatal("physical guard cut with security root")
	}
	if _, err := os.Lstat(filepath.Join(root, "lock")); !os.IsNotExist(err) {
		t.Fatalf("replacement root written: %v", err)
	}
}
