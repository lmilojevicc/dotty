//go:build !linux && !darwin

package nativefs

import (
	"errors"
	"testing"
)

func TestAuthorityUnsupportedPlatform(t *testing.T) {
	var d Dir
	_, err := d.Authority(RolePrivateAnchor)
	var e *AuthorityError
	if !errors.As(err, &e) || e.Reason != ReasonUnsupported {
		t.Fatalf("unsupported authority operation: %v", err)
	}
}
