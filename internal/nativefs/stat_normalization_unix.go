//go:build linux || darwin

package nativefs

// Stat_t uses 16-bit mode/link fields on Darwin, 32-bit modes on Linux,
// and 32- or 64-bit link counts depending on the Linux architecture.
func normalizeStatModeAndLinks[M ~uint16 | ~uint32, N ~uint16 | ~uint32 | ~uint64](
	mode M,
	nlink N,
) (uint32, uint64) {
	return uint32(mode) & 0o7777, uint64(nlink)
}
