package database

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/models"
)

// JournalCompletionColumns is the narrow completion-owned part of an operation
// row. UpdateJournalInTx writes it atomically with the journal; all other stored
// metadata is preserved. The three primary-path fields are assigned verbatim,
// including zero values. An empty RevertStatus preserves the stored status.
// FoundNFOPath overrides a stored NFO path when nonempty; otherwise NFOPath is
// used only when the stored NFO path is empty (or NULL).
type JournalCompletionColumns struct {
	NewPath         string
	InPlaceRenamed  bool
	OriginalDirPath string
	RevertStatus    models.RevertStatusEnum
	FoundNFOPath    string
	NFOPath         string
}

type journalCompletionColumnsContextKey struct{}

// WithJournalCompletionColumns opts UpdateJournalInTx into a combined journal
// and completion-column commit. completion runs once, after the journal merge
// callback succeeds and before any UPDATE. It may use that callback's admission
// result: returning nil leaves columns untouched (including an exact replay).
// An error aborts the entire transaction. A nil callback disables the option.
// The callback must not perform nested repository writes or external mutation.
func WithJournalCompletionColumns(ctx context.Context, completion func() (*JournalCompletionColumns, error)) context.Context {
	return context.WithValue(ctx, journalCompletionColumnsContextKey{}, completion)
}

// ResolveJournalCompletionColumns evaluates the optional completion payload.
// Repository adapters must call it only after a successful journal callback and
// apply the result in the same transaction as the journal, honoring the narrow
// field semantics above. No option (or a nil callback) resolves to nil.
func ResolveJournalCompletionColumns(ctx context.Context) (*JournalCompletionColumns, error) {
	completion, _ := ctx.Value(journalCompletionColumnsContextKey{}).(func() (*JournalCompletionColumns, error))
	if completion == nil {
		return nil, nil
	}
	return completion()
}
