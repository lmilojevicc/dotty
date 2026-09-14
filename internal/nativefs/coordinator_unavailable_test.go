//go:build (!linux && !darwin) || (darwin && !cgo)

package nativefs

import (
	"context"
	"errors"
	"testing"
)

func TestCoordinatorUnavailable(t *testing.T) {
	lease, err := AcquireUser(context.Background())
	if lease != nil {
		_ = lease.Release()
		t.Fatal("unsupported user lease published")
	}
	var native *Error
	if !errors.As(err, &native) || native.Reason != ReasonUnsupported {
		t.Fatalf("want unsupported before filesystem work: %v", err)
	}
	var report *CoordinatorError
	if !errors.As(err, &report) || len(report.CreationRecords()) != 0 {
		t.Fatal("unexpected unsupported creation evidence")
	}
	var empty *UserLease
	if err := empty.Release(); err != nil {
		t.Fatal(err)
	}
	if err := (&UserLease{}).LockRepositories(context.Background(), nil); err == nil {
		t.Fatal("unsupported batch accepted")
	}
	if len(empty.CreationRecords()) != 0 || len(empty.RepositoryBindings()) != 0 {
		t.Fatal("zero wrapper evidence")
	}
}
