//go:build linux

package nativefs

import "golang.org/x/sys/unix"

func nativeRename(srcFD int, source string, dstFD int, target string) error {
	return unix.Renameat2(srcFD, source, dstFD, target, unix.RENAME_NOREPLACE)
}

func statIdentity(st *unix.Stat_t) Identity {
	return Identity{Device: st.Dev, Inode: st.Ino, Kind: st.Mode & unix.S_IFMT, Valid: true}
}
