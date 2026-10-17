package worker

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestApplyActressRenamesRejectsClearingNamedIdentity(t *testing.T) {
	for _, existing := range []models.Actress{
		{FirstName: " First "}, {LastName: " Last "}, {JapaneseName: " 名前 "},
	} {
		for _, blank := range []string{"", " \t\n\u3000"} {
			for _, thumbEdited := range []bool{false, true} {
				t.Run(fmt.Sprintf("%q/%q/thumb=%v", existing.FullName(), blank, thumbEdited), func(t *testing.T) {
					repo := mocks.NewMockActressRepositoryInterface(t)
					existing.ID = 31
					repo.EXPECT().FindByID(context.Background(), uint(31)).Return(&existing, nil)
					err := ApplyActressRenames(context.Background(), repo, []ActressRenamePlan{{ID: 31, FirstName: blank, LastName: blank, JapaneseName: blank, ThumbEdited: thumbEdited, ThumbURL: "new-thumb"}})
					var invalid *InvalidActressRenameError
					require.ErrorAs(t, fmt.Errorf("wrapped: %w", err), &invalid)
					require.Equal(t, uint(31), invalid.ActressID)
					require.EqualError(t, err, "actress 31 must retain at least one non-empty name")
					// The strict mock permits no name or thumbnail write on invalid input.
				})
			}
		}
	}
}

func TestApplyActressRenamesPreservesCompatibleEdits(t *testing.T) {
	for _, tc := range []struct {
		name     string
		existing models.Actress
		rename   ActressRenamePlan
	}{
		{name: "named no-op", existing: models.Actress{FirstName: "First"}, rename: ActressRenamePlan{FirstName: "First"}},
		{name: "last-only no-op", existing: models.Actress{LastName: "Last"}, rename: ActressRenamePlan{LastName: "Last"}},
		{name: "rename to last-only", existing: models.Actress{FirstName: "First"}, rename: ActressRenamePlan{LastName: "Last"}},
		{name: "first-only rename", existing: models.Actress{LastName: "Last"}, rename: ActressRenamePlan{FirstName: "First"}},
		{name: "Japanese-only rename", existing: models.Actress{FirstName: "First"}, rename: ActressRenamePlan{JapaneseName: "名前"}},
		{name: "unnamed no-op"},
		{name: "DMM-only no-op", existing: models.Actress{DMMID: 123}},
		{name: "unnamed whitespace cleanup", existing: models.Actress{FirstName: " \t", LastName: "\u3000"}},
		{name: "unnamed gains name", rename: ActressRenamePlan{LastName: "Last"}},
		{name: "DMM-only thumbnail", existing: models.Actress{DMMID: 123}, rename: ActressRenamePlan{ThumbEdited: true, ThumbURL: "new-thumb"}},
		{name: "named thumbnail", existing: models.Actress{FirstName: "First"}, rename: ActressRenamePlan{FirstName: "First", ThumbEdited: true, ThumbURL: "new-thumb"}},
		{name: "named thumbnail clear", existing: models.Actress{FirstName: "First", ThumbURL: "old-thumb"}, rename: ActressRenamePlan{FirstName: "First", ThumbEdited: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := mocks.NewMockActressRepositoryInterface(t)
			tc.existing.ID, tc.rename.ID = 31, 31
			repo.EXPECT().FindByID(context.Background(), uint(31)).Return(&tc.existing, nil)
			r := tc.rename
			if r.ThumbEdited {
				repo.EXPECT().RenameIdentityFields(context.Background(), r.ID, r.FirstName, r.LastName, r.JapaneseName, r.ThumbURL).Return(nil)
			} else if tc.existing.FirstName != r.FirstName || tc.existing.LastName != r.LastName || tc.existing.JapaneseName != r.JapaneseName {
				repo.EXPECT().RenameNameFields(context.Background(), r.ID, r.FirstName, r.LastName, r.JapaneseName).Return(nil)
			}
			require.NoError(t, ApplyActressRenames(context.Background(), repo, []ActressRenamePlan{r}))
		})
	}
}

// Legacy unnamed and DMM-only identities can still be echoed and have their
// thumbnail edited through the real transactional repository.
func TestApplyActressRenamesUnnamedDatabaseCompatibility(t *testing.T) {
	for _, dmmID := range []int{0, 123} {
		t.Run(fmt.Sprintf("dmm=%d", dmmID), func(t *testing.T) {
			db := newActressEditTestDB(t)
			actor := models.Actress{DMMID: dmmID, ThumbURL: "old-thumb"}
			require.NoError(t, db.Create(&actor).Error)
			for _, thumbEdited := range []bool{false, true} {
				require.NoError(t, db.WithEditTx(t.Context(), func(u database.EditUnit) error {
					return ApplyActressRenames(t.Context(), u.Actresses, []ActressRenamePlan{{ID: actor.ID, ThumbEdited: thumbEdited, ThumbURL: "new-thumb"}})
				}))
				actual, err := db.Repositories().ActressRepo.FindByID(t.Context(), actor.ID)
				require.NoError(t, err)
				require.Empty(t, actual.FirstName)
				require.Empty(t, actual.LastName)
				require.Empty(t, actual.JapaneseName)
				require.Equal(t, dmmID, actual.DMMID)
				if thumbEdited {
					require.Equal(t, "new-thumb", actual.ThumbURL)
				} else {
					require.Equal(t, actor.ThumbURL, actual.ThumbURL)
					require.True(t, actor.UpdatedAt.Equal(actual.UpdatedAt), "no-op echo must not write the row")
				}
			}
		})
	}
}
