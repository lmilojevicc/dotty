//go:build !linux && !darwin

package nativefs

import "context"

// Foreign targets refuse before any descriptor or filesystem work. Compilation
// of this data API is not foreign runtime, authority, or release evidence.
func (d *Dir) ObserveRegular(_ context.Context, name Component) (RegularObservation, error) {
	return RegularObservation{}, regularError(
		"",
		name,
		ReasonUnsupported,
		"regular observation requires the Linux or Darwin data backend",
		regularMetadata{},
		regularMetadata{},
		nil,
	)
}
