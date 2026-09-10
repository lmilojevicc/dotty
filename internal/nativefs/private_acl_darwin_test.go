//go:build darwin && cgo && !ios

package nativefs

import "testing"

func setPrivateNativeACL(t *testing.T, root, path string, directory bool) {
	t.Helper()
	chmodPrivateACL(t, root, path, "everyone allow readattr")
}
