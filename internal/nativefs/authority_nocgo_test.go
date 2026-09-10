//go:build darwin && !cgo

package nativefs

import (
	"errors"
	"testing"
)

func TestDarwinNoCgoUnsupported(t *testing.T) {
	completed := false
	t.Run("DarwinNoCgoUnsupported", func(t *testing.T) {
		t.Cleanup(func() {
			if t.Skipped() {
				t.Error("required no-cgo capability case skipped")
			}
		})
		var d Dir
		_, err := d.Authority(RolePrivateAnchor)
		var e *AuthorityError
		if !errors.As(err, &e) || e.Reason != ReasonUnsupported || e.Remediation == "" {
			t.Fatalf("Darwin !cgo must refuse before descriptor work: %v", err)
		}
		completed = true
	})
	if !completed {
		t.Error("DarwinNoCgoUnsupported missing, filtered, or incomplete")
	}
}
