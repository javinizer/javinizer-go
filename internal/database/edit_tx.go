package database

import (
	"context"

	"gorm.io/gorm"
)

// EditUnit bundles transaction-scoped repositories used by atomic review-edit
// commits (POSTER-WRITE-HARDENING D4/D8). Repositories constructed from an
// EditUnit share ONE gorm transaction: every write inside the unit commits or
// rolls back together.
type EditUnit struct {
	Movies    MovieRepositoryInterface
	Actresses ActressRepositoryInterface
	Jobs      JobRepositoryInterface
}

// WithEditTx runs fn inside a single SQLite/gorm transaction, handing it a
// transaction-scoped repository set. Returning a non-nil error from fn rolls
// everything back. Callers OUTSIDE the transaction must keep using the
// outer repos — the unit exists only for the duration of fn.
func (db *DB) WithEditTx(ctx context.Context, fn func(u EditUnit) error) error {
	if db == nil || db.DB == nil {
		return gorm.ErrInvalidDB
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txDB := &DB{DB: tx, dsn: db.dsn, fs: db.fs}
		return fn(EditUnit{
			Movies:    NewMovieRepository(txDB),
			Actresses: NewActressRepository(txDB),
			Jobs:      NewJobRepository(txDB),
		})
	})
}
