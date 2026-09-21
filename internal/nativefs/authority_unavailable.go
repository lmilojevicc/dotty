//go:build (!linux && !darwin) || (darwin && !cgo)

package nativefs

// Authority refuses before filesystem work when descriptor ACL evidence is not
// available. In particular, Darwin release builds currently disable cgo: native
// cgo validation does not establish support for those production binaries.
func (d *Dir) Authority(role AuthorityRole) (AuthorityFacts, error) {
	return AuthorityFacts{}, &AuthorityError{
		Op:          "authority",
		Role:        role,
		Reason:      ReasonUnsupported,
		Detail:      "descriptor filesystem/security authority backend unavailable in this build",
		Remediation: "Use a supported native security backend. Darwin requires cgo; production Darwin integration remains blocked until release build policy is resolved.",
	}
}
