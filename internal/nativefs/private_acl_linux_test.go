//go:build linux && !android

package nativefs

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func setPrivateNativeACL(t *testing.T, root, path string, directory bool) {
	t.Helper()
	relative, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsAbs(root) || !filepath.IsAbs(path) || !filepath.IsLocal(relative) {
		t.Fatal("ACL fixture path not private")
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	must(t, err)
	defer func() { must(t, unix.Close(fd)) }()
	// Real public Linux POSIX ACL xattr: an ineffective named-user entry
	// preserves 0600/0700 while still requiring ACL-presence refusal.
	owner := uint16(6)
	if directory {
		owner = 7
	}
	entries := []struct {
		tag, perm uint16
		id        uint32
	}{
		{0x01, owner, ^uint32(0)},
		{0x02, 4, uint32(os.Geteuid()) + 1},
		{0x04, 0, ^uint32(0)},
		{0x10, 0, ^uint32(0)},
		{0x20, 0, ^uint32(0)},
	}
	data := make([]byte, 4+8*len(entries))
	binary.LittleEndian.PutUint32(data, 2)
	for i, entry := range entries {
		offset := 4 + 8*i
		binary.LittleEndian.PutUint16(data[offset:], entry.tag)
		binary.LittleEndian.PutUint16(data[offset+2:], entry.perm)
		binary.LittleEndian.PutUint32(data[offset+4:], entry.id)
	}
	must(t, unix.Fsetxattr(fd, "system.posix_acl_access", data, unix.XATTR_CREATE))
}
