package worker

import (
	"context"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/database"
)

// InvalidActressRenameError rejects clearing all names of a named identity.
// Existing unnamed identities remain valid, including DMM-only records.
type InvalidActressRenameError struct {
	ActressID uint
}

func (e *InvalidActressRenameError) Error() string {
	return fmt.Sprintf("actress %d must retain at least one non-empty name", e.ActressID)
}

func actressNamesEmpty(first, last, japanese string) bool {
	return strings.TrimSpace(first) == "" && strings.TrimSpace(last) == "" && strings.TrimSpace(japanese) == ""
}

// ApplyActressRenames is the DB-only identity-edit leg shared by review saves
// and rollback-only previews. The caller supplies transaction-scoped repositories.
// It does not publish results, write envelopes, or touch media/cache files.
func ApplyActressRenames(ctx context.Context, repo database.ActressRepositoryInterface, renames []ActressRenamePlan) error {
	for _, rn := range renames {
		existing, err := repo.FindByID(ctx, rn.ID)
		switch {
		case err != nil && !database.IsNotFound(err):
			return fmt.Errorf("load actress for rename: %w", err)
		case err != nil || existing == nil:
			continue
		}
		if !actressNamesEmpty(existing.FirstName, existing.LastName, existing.JapaneseName) && actressNamesEmpty(rn.FirstName, rn.LastName, rn.JapaneseName) {
			return &InvalidActressRenameError{ActressID: rn.ID}
		}
		namesUnchanged := existing.FirstName == rn.FirstName && existing.LastName == rn.LastName && existing.JapaneseName == rn.JapaneseName
		thumbChanged := rn.ThumbEdited
		if namesUnchanged && !thumbChanged {
			continue
		}
		if thumbChanged {
			if err := repo.RenameIdentityFields(ctx, rn.ID, rn.FirstName, rn.LastName, rn.JapaneseName, rn.ThumbURL); err != nil {
				return fmt.Errorf("persist actress identity edit: %w", err)
			}
			continue
		}
		if err := repo.RenameNameFields(ctx, rn.ID, rn.FirstName, rn.LastName, rn.JapaneseName); err != nil {
			return fmt.Errorf("persist actress name edit: %w", err)
		}
	}
	return nil
}
