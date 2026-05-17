//go:build darwin

package dotty

import "syscall"

func statDev(stat *syscall.Stat_t) uint64 {
	return uint64(stat.Dev)
}
