package nativefs

import (
	"errors"
	"fmt"
	"slices"
)

// UserLease owns native user coordination and exactly one repository batch.
// Copies share lifetime. Release explicitly; no descriptor or mutation authority
// is exported. Callers must read config/default selection while holding this
// lease, before LockRepositories. That discipline is not type-enforced here.
type UserLease struct {
	state *coordinatorState
}

// RepositoryBinding is a published logical selection and its pinned physical
// identity. It is evidence only, not durable authorization for later mutations.
type RepositoryBinding struct {
	logical  string
	physical string
	identity Identity
}

func (b RepositoryBinding) LogicalPath() string  { return b.logical }
func (b RepositoryBinding) PhysicalPath() string { return b.physical }
func (b RepositoryBinding) Identity() Identity   { return b.identity }

// CoordinatorError preserves native causes and all bootstrap creation records,
// including when acquisition cannot return a UserLease. It assigns no transaction
// outcome and grants no authority to remove retained or uncertain anchors.
type CoordinatorError struct {
	logical  string
	physical string
	records  []CreationRecord
	err      error
}

func (e *CoordinatorError) LogicalPath() string               { return e.logical }
func (e *CoordinatorError) PhysicalPath() string              { return e.physical }
func (e *CoordinatorError) CreationRecords() []CreationRecord { return slices.Clone(e.records) }
func (e *CoordinatorError) Unwrap() error                     { return e.err }
func (e *CoordinatorError) Error() string {
	text := fmt.Sprintf(
		"nativefs coordination: logical=%q recorded-physical=%q: %v",
		e.logical,
		e.physical,
		e.err,
	)
	for _, record := range e.records {
		text += fmt.Sprintf(
			"; retained or uncertain creation path=%q kind=%#x observations=%+v",
			record.path,
			record.kind,
			record.observations,
		)
	}
	return text + "; Inspect these exact selections, physical paths and authority observations; stop conflicting edits and resolve the stated mismatch before a new acquisition. Do not remove or repair rendezvous objects while another operation may hold them."
}

func coordinatorError(logical, physical string, records []CreationRecord, err error) error {
	if err == nil {
		return nil
	}
	var selection *CoordinatorError
	if logical == "" && errors.As(err, &selection) && selection.logical != "" {
		logical, physical = selection.logical, selection.physical
	}
	return &CoordinatorError{
		logical:  logical,
		physical: physical,
		records:  slices.Clone(records),
		err:      err,
	}
}
