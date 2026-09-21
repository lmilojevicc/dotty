// Package nativefs owns a small descriptor-relative native filesystem boundary.
// Primitive APIs accept physical absolute directory paths, not logical aliases.
// The native coordinator alone resolves a logical repository batch and owns
// user/repository process coordination. Config/default selection, Manifest
// interpretation, copy fidelity, and transaction outcomes remain outside this
// boundary. No production command uses this package yet.
//
// Ownership is enforced at this package's API boundary: no descriptors or lease
// callbacks are exported. This is not containment against malicious code inside
// this package. Identity observations are not durable snapshots or inode CAS;
// uncooperative writers can still act between the final observation and syscall.
package nativefs

import (
	"fmt"
	"strings"
)

// Component is one Unix directory-entry name. Its zero value is invalid.
// Backslash is an ordinary filename character, not a separator.
type Component struct {
	name string
}

func ParseComponent(name string) (Component, error) {
	c := Component{name: name}
	if !c.valid() {
		return Component{}, &Error{
			Op:        "parse-component",
			Component: name,
			Reason:    ReasonInvalidComponent,
		}
	}
	return c, nil
}

func (c Component) valid() bool {
	return c.name != "" && c.name != "." && c.name != ".." && !strings.ContainsAny(c.name, "/\x00")
}

// Identity is device/inode/file-kind only, without content or metadata fidelity.
// Valid distinguishes an absent observation from legitimate numeric zero values.
// Kind contains the native Unix S_IFMT bits, not permission bits.
type Identity struct {
	Device uint64
	Inode  uint64
	Kind   uint32
	Valid  bool
}

// Dir owns all pinned ancestry descriptors through shared private state. Copying
// a Dir wrapper shares ownership; closing any copy permanently invalidates all
// copies. Call Close explicitly. There is no descriptor extraction or adoption.
type Dir struct {
	state *dirState
}

// Reason classifies a primitive failure, never a final transaction outcome.
type Reason string

const (
	ReasonInvalidComponent Reason = "invalid-component"
	ReasonInvalidPath      Reason = "invalid-physical-path"
	ReasonInvalidIdentity  Reason = "invalid-identity"
	ReasonIdentityChanged  Reason = "identity-changed"
	ReasonTopology         Reason = "unsafe-topology"
	ReasonClosed           Reason = "closed-handle"
	ReasonUnsupported      Reason = "unsupported-native-operation"
	ReasonInvalidOperation Reason = "invalid-native-operation"
	ReasonCrossDevice      Reason = "cross-device"
	ReasonExists           Reason = "destination-exists"
	ReasonMissing          Reason = "missing-entry"
	ReasonIO               Reason = "native-io"
)

// Error preserves the affected physical paths, components, identity evidence,
// and native cause. It makes no assertion about earlier transaction mutations.
// Path identifies the failing observation; rename also retains both endpoints.
type Error struct {
	Op              string
	Path            string
	SourcePath      string
	DestinationPath string
	Component       string
	OtherComponent  string
	Reason          Reason
	Expected        Identity
	Observed        Identity
	Err             error
}

func (e *Error) Error() string {
	text := fmt.Sprintf(
		"nativefs %s: %s: path=%q component=%q",
		e.Op,
		e.Reason,
		e.Path,
		e.Component,
	)
	if e.SourcePath != "" || e.DestinationPath != "" {
		text += fmt.Sprintf(
			" source=%q destination=%q target-component=%q",
			e.SourcePath,
			e.DestinationPath,
			e.OtherComponent,
		)
	}
	if e.Expected.Valid || e.Observed.Valid {
		text += fmt.Sprintf(" expected=%+v observed=%+v", e.Expected, e.Observed)
	}
	if e.Err != nil {
		text += ": " + e.Err.Error()
	}
	return text
}

func (e *Error) Unwrap() error { return e.Err }

func physicalComponents(path string) ([]Component, error) {
	if path == "/" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, &Error{Op: "open-directory", Path: path, Reason: ReasonInvalidPath}
	}
	var parts []Component
	for _, name := range strings.Split(path[1:], "/") {
		c, err := ParseComponent(name)
		if err != nil {
			return nil, &Error{
				Op:        "open-directory",
				Path:      path,
				Component: name,
				Reason:    ReasonInvalidPath,
				Err:       err,
			}
		}
		parts = append(parts, c)
	}
	return parts, nil
}

func entryPath(dir string, name Component) string {
	if dir == "/" {
		return dir + name.name
	}
	return dir + "/" + name.name
}
