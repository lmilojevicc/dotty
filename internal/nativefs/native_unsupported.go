//go:build !linux && !darwin

package nativefs

// No descriptor or filesystem work is available on unsupported platforms.
type dirState struct{}

func unsupported(op string) error {
	return &Error{Op: op, Reason: ReasonUnsupported}
}

func OpenPhysicalDir(abs string) (*Dir, error) {
	return nil, &Error{Op: "open-directory", Path: abs, Reason: ReasonUnsupported}
}

func (d *Dir) Identity() (Identity, error) {
	return Identity{}, unsupported("identity")
}

func (d *Dir) Observe(name Component) (Identity, error) {
	return Identity{}, &Error{Op: "observe", Component: name.name, Reason: ReasonUnsupported}
}

func (d *Dir) Close() error { return unsupported("close-directory") }

func RenameNoReplace(
	src *Dir,
	name Component,
	expected Identity,
	dst *Dir,
	target Component,
) error {
	return &Error{
		Op:             "rename-no-replace",
		Component:      name.name,
		OtherComponent: target.name,
		Reason:         ReasonUnsupported,
	}
}
