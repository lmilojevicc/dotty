//go:build darwin && cgo

package nativefs

import (
	"fmt"

	"golang.org/x/sys/unix"
)

const systemTemporaryRoot = "/private/tmp"

func nativeFilesystem(fd int) (FilesystemFacts, error) {
	var st unix.Statfs_t
	if err := unix.Fstatfs(fd, &st); err != nil {
		return FilesystemFacts{state: EvidenceFailed}, err
	}
	return darwinFilesystemFacts(&st), nil
}

func darwinFilesystemFacts(st *unix.Statfs_t) FilesystemFacts {
	name := unix.ByteSliceToString(st.Fstypename[:])
	facts := FilesystemFacts{
		state: EvidenceUnsupported, kind: uint64(st.Type), flags: uint64(st.Flags),
		id: fmt.Sprint(st.Fsid), model: name,
	}
	if name == "apfs" && st.Flags&unix.MNT_LOCAL != 0 &&
		st.Flags&(unix.MNT_IGNORE_OWNERSHIP|unix.MNT_UNION) == 0 {
		facts.state, facts.model = EvidencePresent, "darwin-local-apfs-ownership-enforced"
	}
	return facts
}

func nativeSecurity(fd int, _ bool) (SecurityEvidence, error) {
	return readDarwinSecurity(fd, systemDarwinACLCalls())
}
