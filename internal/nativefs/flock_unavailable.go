//go:build (!linux && !darwin) || (darwin && !cgo)

package nativefs

import "context"

func flockUnavailable(role AuthorityRole) error {
	return &AuthorityError{
		Op:          "flock",
		Role:        role,
		Reason:      ReasonUnsupported,
		Detail:      "native flock authority backend unavailable",
		Remediation: "Use a supported native filesystem/security backend before locking. Darwin requires cgo; production integration remains blocked by release build policy.",
	}
}

// LockFile refuses without filesystem work on unavailable backends.
func (d *Dir) LockFile(context.Context, Component) (*Lease, error) {
	return nil, flockUnavailable(RoleLockFile)
}

// LockDirectory refuses without filesystem work on unavailable backends.
func (d *Dir) LockDirectory(context.Context) (*Lease, error) {
	return nil, flockUnavailable(RoleRepositoryDirectory)
}
