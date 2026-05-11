package dotty

import (
	"errors"
	"fmt"
	"os"
)

type Tx struct {
	rollbacks []func() error
	cleanups  []func() error
}

func RunAtomic(fn func(*Tx) error) error {
	tx := &Tx{}
	if err := fn(tx); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("%w (rollback failed: %w)", err, rollbackErr)
		}
		return err
	}
	return tx.Commit()
}

func (tx *Tx) AddRollback(fn func() error) {
	tx.rollbacks = append(tx.rollbacks, fn)
}

func (tx *Tx) AddCleanup(fn func() error) {
	tx.cleanups = append(tx.cleanups, fn)
}

func (tx *Tx) Rollback() error {
	var errs []error
	for i := len(tx.rollbacks) - 1; i >= 0; i-- {
		if err := tx.rollbacks[i](); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	tx.rollbacks = nil
	tx.cleanups = nil
	return errors.Join(errs...)
}

func (tx *Tx) Commit() error {
	var errs []error
	for _, cleanup := range tx.cleanups {
		if err := cleanup(); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	tx.rollbacks = nil
	tx.cleanups = nil
	return errors.Join(errs...)
}
