//go:build linux && amd64

package dotty

import "syscall"

func statDev(stat *syscall.Stat_t) uint64 {
	return stat.Dev
}

func statNlink(stat *syscall.Stat_t) uint64 {
	return stat.Nlink
}
