//go:build (!linux && !darwin) || (darwin && !cgo)

package nativefs

func privateUnavailable(name Component, role AuthorityRole) error {
	return &AuthorityError{
		Op:          "private-create-open",
		Role:        role,
		Reason:      ReasonUnsupported,
		Detail:      "descriptor filesystem/security authority backend unavailable for component " + name.name,
		Remediation: "Use a supported native security backend before creating or opening private authority objects. Darwin requires cgo; production integration remains blocked by release build policy.",
	}
}

// OpenPrivateDir refuses without filesystem work on unavailable backends.
func (d *Dir) OpenPrivateDir(name Component) (*Dir, error) {
	return nil, privateUnavailable(name, RolePrivateAnchor)
}

// CreatePrivateDir refuses without filesystem work on unavailable backends.
func (d *Dir) CreatePrivateDir(name Component) (*Dir, CreationRecord, error) {
	return nil, CreationRecord{}, privateUnavailable(name, RolePrivateAnchor)
}

// OpenLockFile refuses without filesystem work on unavailable backends.
func (d *Dir) OpenLockFile(name Component) (*File, error) {
	return nil, privateUnavailable(name, RoleLockFile)
}

// CreateLockFile refuses without filesystem work on unavailable backends.
func (d *Dir) CreateLockFile(name Component) (*File, CreationRecord, error) {
	return nil, CreationRecord{}, privateUnavailable(name, RoleLockFile)
}
