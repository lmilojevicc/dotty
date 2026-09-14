//go:build !linux && !darwin

package nativefs

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// Compiled, never executed as a foreign binary by TestUnsupportedBuildTargets.
// Runtime refusal assertions are available when this suite runs on that host.
func TestRegularObservationUnavailable(t *testing.T) {
	var operation func(*Dir, context.Context, Component) (RegularObservation, error) = (*Dir).ObserveRegular
	for _, d := range []*Dir{nil, {}} {
		for _, ctx := range []context.Context{nil, t.Context()} {
			got, err := operation(d, ctx, Component{})
			var e *RegularObservationError
			if got != (RegularObservation{}) || !errors.As(err, &e) ||
				e.Reason != ReasonUnsupported ||
				e.Remediation == "" {
				t.Fatalf("foreign operation did not fail closed: %+v %v", got, err)
			}
		}
	}
	typ := reflect.TypeFor[RegularObservation]()
	for i := range typ.NumField() {
		if typ.Field(i).IsExported() {
			t.Fatalf("foreign value exposes mutable field: %s", typ.Field(i).Name)
		}
	}
	if (RegularObservation{}).Valid() {
		t.Fatal("foreign zero observation valid")
	}
}
