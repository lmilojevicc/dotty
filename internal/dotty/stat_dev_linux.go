//go:build linux

package dotty

import "syscall"

func statDev(stat *syscall.Stat_t) uint64 {
	return stat.Dev
}
