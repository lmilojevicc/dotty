//go:build linux

package nativefs

import (
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

var requiredLinuxAuthorityCases = []string{
	"PosixAccessENODATA",
	"DirectoryDefaultENODATA",
	"AccessACLPresentRefused",
	"DefaultACLPresentRefused",
	"EOPNOTSUPPNotAbsence",
	"DescriptorExtTmpfsVerified",
	"NfsCifsFuseOverlayUnknownRefused",
	"ReadonlyOverlayStillRefused",
}

func TestLinuxAuthority(t *testing.T) {
	runAuthorityInventory(t, requiredLinuxAuthorityCases, map[string]func(*testing.T){
		"PosixAccessENODATA":               func(t *testing.T) { testLinuxACLAbsent(t, false) },
		"DirectoryDefaultENODATA":          func(t *testing.T) { testLinuxACLAbsent(t, true) },
		"AccessACLPresentRefused":          func(t *testing.T) { testLinuxACLPresent(t, "system.posix_acl_access") },
		"DefaultACLPresentRefused":         func(t *testing.T) { testLinuxACLPresent(t, "system.posix_acl_default") },
		"EOPNOTSUPPNotAbsence":             testLinuxACLUnsupported,
		"DescriptorExtTmpfsVerified":       testLinuxFilesystem,
		"NfsCifsFuseOverlayUnknownRefused": testLinuxUnsupportedFilesystems,
		"ReadonlyOverlayStillRefused":      testLinuxReadonlyOverlay,
	})
}

func testLinuxACLAbsent(t *testing.T, directory bool) {
	_, d, _ := authorityFixture(t)
	var names []string
	security, err := linuxSecurity(
		d.state.leaf().fd,
		directory,
		func(fd int, name string, dest []byte) (int, error) {
			names = append(names, name)
			n, err := unix.Fgetxattr(fd, name, dest)
			if !errors.Is(err, unix.ENODATA) {
				t.Fatalf("native %s absence requires ENODATA, got %d, %v", name, n, err)
			}
			return n, err
		},
	)
	must(t, err)
	want := 1
	if directory {
		want = 2
	}
	if security.State() != EvidenceAbsent || len(names) != want ||
		names[0] != "system.posix_acl_access" ||
		(directory && names[1] != "system.posix_acl_default") {
		t.Fatalf("wrong descriptor ACL observations: %+v %v", security, names)
	}
}

func testLinuxACLPresent(t *testing.T, name string) {
	r, d, _ := authorityFixture(t)
	// Linux's public POSIX ACL xattr format: version followed by tag/perm/id.
	// A named-user entry makes this nontrivial even with an ineffective mask.
	entries := []struct {
		tag  uint16
		perm uint16
		id   uint32
	}{
		{0x01, 7, ^uint32(0)},
		{0x02, 4, uint32(os.Geteuid()) + 1},
		{0x04, 0, ^uint32(0)},
		{0x10, 0, ^uint32(0)},
		{0x20, 0, ^uint32(0)},
	}
	data := make([]byte, 4+8*len(entries))
	binary.LittleEndian.PutUint32(data, 2)
	for i, e := range entries {
		offset := 4 + i*8
		binary.LittleEndian.PutUint16(data[offset:], e.tag)
		binary.LittleEndian.PutUint16(data[offset+2:], e.perm)
		binary.LittleEndian.PutUint32(data[offset+4:], e.id)
	}
	must(t, unix.Fsetxattr(d.state.leaf().fd, name, data, unix.XATTR_CREATE))
	_, err := fixtureAuthority(d, r, RolePrivateAnchor, systemAuthorityCalls())
	e := authorityReason(t, err, ReasonAuthorityPolicy, r)
	if e.Observed.Security().State() != EvidencePresent || e.Observed.Mode() != 0o700 {
		t.Fatalf("native ACL present refusal did not preserve exact mode/evidence: %+v", e)
	}
	t.Logf("real descriptor %s ACL; ineffective named allow still refuses", name)
}

func testLinuxACLUnsupported(t *testing.T) {
	_, d, _ := authorityFixture(t)
	for _, cause := range []error{unix.EOPNOTSUPP, unix.EACCES, unix.EINVAL, unix.EIO} {
		security, err := linuxSecurity(
			d.state.leaf().fd,
			true,
			func(int, string, []byte) (int, error) {
				return -1, cause
			},
		)
		want := EvidenceFailed
		if errors.Is(cause, unix.EOPNOTSUPP) {
			want = EvidenceUnsupported
		}
		if !errors.Is(err, cause) || security.State() != want {
			t.Fatalf("injected %v became %v, %v", cause, security, err)
		}
	}
	security, err := linuxSecurity(
		d.state.leaf().fd,
		true,
		func(int, string, []byte) (int, error) { return 0, nil },
	)
	must(t, err)
	if security.State() != EvidencePresent {
		t.Fatal("zero-length but present ACL became absence")
	}
}

func testLinuxFilesystem(t *testing.T) {
	_, d, f := authorityFixture(t)
	var st unix.Statfs_t
	must(t, unix.Fstatfs(d.state.leaf().fd, &st))
	if st.Type != unix.EXT4_SUPER_MAGIC && st.Type != unix.TMPFS_MAGIC {
		t.Fatalf("positive fixture must actually be ext/tmpfs: %+v", st)
	}
	if f.Filesystem().Type() != uint64(st.Type) || f.Filesystem().Flags() != uint64(st.Flags) ||
		f.Filesystem().State() != EvidencePresent {
		t.Fatalf("filesystem observation mismatch: %+v / %+v", f.Filesystem(), st)
	}
	t.Logf("actual descriptor filesystem: %+v", f.Filesystem())
}

func testLinuxUnsupportedFilesystems(t *testing.T) {
	_, d, _ := authorityFixture(t)
	var st unix.Statfs_t
	must(t, unix.Fstatfs(d.state.leaf().fd, &st))
	for _, magic := range []int64{unix.NFS_SUPER_MAGIC, unix.CIFS_SUPER_MAGIC, unix.FUSE_SUPER_MAGIC, unix.OVERLAYFS_SUPER_MAGIC, 0x12345678} {
		st.Type = magic
		if f := linuxFilesystemFacts(&st); f.State() != EvidenceUnsupported {
			t.Fatalf("injected unsupported filesystem %#x accepted: %+v", magic, f)
		}
	}
}

func testLinuxReadonlyOverlay(t *testing.T) {
	r, d, _ := authorityFixture(t)
	var st unix.Statfs_t
	must(t, unix.Fstatfs(d.state.leaf().fd, &st))
	st.Type, st.Flags = unix.OVERLAYFS_SUPER_MAGIC, unix.ST_RDONLY
	calls := systemAuthorityCalls()
	calls.filesystem = func(int) (FilesystemFacts, error) { return linuxFilesystemFacts(&st), nil }
	_, err := d.authority(RolePrivateAnchor, calls)
	_ = authorityReason(t, err, ReasonUnsupported, "/")
	t.Logf("injected read-only overlay refused by unbounded production policy; fixture=%s", r)
}
