//go:build darwin

package nativefs

import "golang.org/x/sys/unix"

func nativeRename(srcFD int, source string, dstFD int, target string) error {
	// RENAME_EXCL from Darwin's renameatx_np ABI (not uniformly exported by x/sys).
	const renameExcl = 0x4
	return unix.RenameatxNp(srcFD, source, dstFD, target, renameExcl)
}

func statIdentity(st *unix.Stat_t) Identity {
	return Identity{
		Device: uint64(uint32(st.Dev)),
		Inode:  st.Ino,
		Kind:   uint32(st.Mode) & unix.S_IFMT,
		Valid:  true,
	}
}
