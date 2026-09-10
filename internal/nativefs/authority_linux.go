//go:build linux

package nativefs

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

const systemTemporaryRoot = "/tmp"

func nativeFilesystem(fd int) (FilesystemFacts, error) {
	var st unix.Statfs_t
	if err := unix.Fstatfs(fd, &st); err != nil {
		return FilesystemFacts{state: EvidenceFailed}, err
	}
	return linuxFilesystemFacts(&st), nil
}

func linuxFilesystemFacts(st *unix.Statfs_t) FilesystemFacts {
	facts := FilesystemFacts{
		state: EvidenceUnsupported, kind: uint64(st.Type), flags: uint64(st.Flags),
		id: fmt.Sprint(st.Fsid), model: "unsupported-linux-filesystem",
	}
	// This is intentionally a small allowlist, not an inference that every
	// filesystem exposing modes or xattrs implements the POSIX ACL model.
	switch st.Type {
	case unix.EXT4_SUPER_MAGIC:
		facts.state, facts.model = EvidencePresent, "linux-ext-posix-acl"
	case unix.TMPFS_MAGIC:
		facts.state, facts.model = EvidencePresent, "linux-tmpfs-posix-acl"
	}
	return facts
}

func nativeSecurity(fd int, directory bool) (SecurityEvidence, error) {
	return linuxSecurity(fd, directory, unix.Fgetxattr)
}

func linuxSecurity(
	fd int,
	directory bool,
	get func(int, string, []byte) (int, error),
) (SecurityEvidence, error) {
	evidence := SecurityEvidence{state: EvidenceAbsent, mechanism: "fgetxattr:posix-access"}
	names := []string{"system.posix_acl_access"}
	if directory {
		names = append(names, "system.posix_acl_default")
		evidence.mechanism += "+default"
	}
	for _, name := range names {
		// The descriptor's approved filesystem model is checked by the caller.
		// Size-only retrieval is sufficient: any present value, even length 0,
		// refuses. We never interpret an ACL as harmless based on its contents.
		_, err := get(fd, name, nil)
		switch {
		case err == nil:
			evidence.state = EvidencePresent
		case errors.Is(err, unix.ENODATA):
			// Only ENODATA establishes absence on this approved native model.
		case errors.Is(err, unix.EOPNOTSUPP):
			evidence.state = EvidenceUnsupported
			return evidence, fmt.Errorf("read %s: %w", name, err)
		default:
			evidence.state = EvidenceFailed
			return evidence, fmt.Errorf("read %s: %w", name, err)
		}
	}
	return evidence, nil
}
