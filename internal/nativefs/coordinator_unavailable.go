//go:build (!linux && !darwin) || (darwin && !cgo)

package nativefs

import "context"

type coordinatorState struct{}

// AcquireUser refuses before filesystem work without native authority support.
// Darwin's cgo-disabled release policy is unchanged by this unused coordinator.
func AcquireUser(context.Context) (*UserLease, error) {
	return nil, coordinatorError("", "", nil, &Error{Op: "acquire-user", Reason: ReasonUnsupported})
}

func (u *UserLease) LockRepositories(context.Context, []string) error {
	return coordinatorError("", "", nil, &Error{Op: "repository-batch", Reason: ReasonUnsupported})
}

func (u *UserLease) Release() error                          { return nil }
func (u *UserLease) CreationRecords() []CreationRecord       { return nil }
func (u *UserLease) RepositoryBindings() []RepositoryBinding { return nil }
