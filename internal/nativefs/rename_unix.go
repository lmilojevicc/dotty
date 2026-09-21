//go:build linux || darwin

package nativefs

import "golang.org/x/sys/unix"

// RenameNoReplace moves an entry using only the native exclusive rename. Both
// directory leases span final ancestry/source observations through the syscall.
// A symlink source moves the link itself, never its referent. There is no
// replacement, check-then-rename, copy, or deletion fallback, including on EXDEV.
// Expected identity narrows observable races; it is NOT an atomic inode CAS.
func RenameNoReplace(
	src *Dir,
	name Component,
	expected Identity,
	dst *Dir,
	target Component,
) error {
	return renameNoReplace(src, name, expected, dst, target, nativeRename)
}

// The per-call native seam permits errno injection, not a hook for interference
// between the final source observation and syscall. Interference tests change
// fixtures before entering this function. Production uses only nativeRename.
func renameNoReplace(
	src *Dir,
	name Component,
	expected Identity,
	dst *Dir,
	target Component,
	call func(int, string, int, string) error,
) error {
	if !name.valid() {
		return &Error{
			Op:             "rename-no-replace",
			Component:      name.name,
			OtherComponent: target.name,
			Reason:         ReasonInvalidComponent,
		}
	}
	if !target.valid() {
		return &Error{
			Op:        "rename-no-replace",
			Component: target.name,
			Reason:    ReasonInvalidComponent,
		}
	}
	if !expected.Valid {
		return &Error{
			Op:             "rename-no-replace",
			Component:      name.name,
			OtherComponent: target.name,
			Reason:         ReasonInvalidIdentity,
		}
	}
	release, err := acquire(src, dst)
	if err != nil {
		return err
	}
	defer release()
	s, d := src.state.leaf(), dst.state.leaf()
	sourcePath, destinationPath := entryPath(s.path, name), entryPath(d.path, target)
	withEndpoints := func(e *Error) error {
		e.SourcePath = sourcePath
		e.DestinationPath = destinationPath
		e.OtherComponent = target.name
		if !e.Expected.Valid {
			e.Expected = expected
		}
		return e
	}
	if err := src.state.guard("rename-no-replace"); err != nil {
		return renameGuardError(err, sourcePath, destinationPath, target)
	}
	if dst.state != src.state {
		if err := dst.state.guard("rename-no-replace"); err != nil {
			return renameGuardError(err, sourcePath, destinationPath, target)
		}
	}
	// A directory cannot be installed into itself or any pinned descendant.
	// Do not mislabel the kernel's EINVAL for this topology as flag support.
	if expected.Kind == unix.S_IFDIR {
		for _, ancestor := range dst.state.nodes {
			if ancestor.identity == expected {
				return withEndpoints(
					&Error{
						Op:        "rename-no-replace",
						Path:      sourcePath,
						Component: name.name,
						Reason:    ReasonTopology,
						Expected:  expected,
						Observed:  ancestor.identity,
					},
				)
			}
		}
	}
	observed, err := statEntry(s.fd, name)
	if err != nil {
		return withEndpoints(nativeError("rename-no-replace", sourcePath, name, err))
	}
	if observed != expected {
		return withEndpoints(mismatch("rename-no-replace", sourcePath, name, expected, observed))
	}
	if err := call(s.fd, name.name, d.fd, target.name); err != nil {
		e := nativeError("rename-no-replace", sourcePath, name, err)
		e.Observed = observed
		return withEndpoints(e)
	}
	return nil
}

func renameGuardError(e *Error, source, destination string, target Component) error {
	e.SourcePath = source
	e.DestinationPath = destination
	e.OtherComponent = target.name
	return e
}
