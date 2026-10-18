package database

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/models"
)

type operationColumnGuardContextKey struct{}

// WithOperationColumnGuard adds optional validation to UpdateNonJournalFields.
// The guard sees only the current row's ID, GeneratedFiles, and RevertStatus,
// read inside the existing BEGIN IMMEDIATE transaction before any column write.
// Returning an error aborts the entire update. Missing rows remain a no-op and
// do not invoke the guard. A nil guard leaves ordinary update behavior intact.
//
// The guard must only inspect the supplied snapshot: it must not mutate it or
// perform nested repository writes while the transaction holds the write lock.
func WithOperationColumnGuard(ctx context.Context, guard func(current *models.BatchFileOperation) error) context.Context {
	return context.WithValue(ctx, operationColumnGuardContextKey{}, guard)
}
