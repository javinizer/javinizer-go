package worker

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/database"
)

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
