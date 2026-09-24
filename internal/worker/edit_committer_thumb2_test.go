package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestEditCommitterThumbnailEditFailureAborts(t *testing.T) {
	actresses := mocks.NewMockActressRepositoryInterface(t)
	actresses.EXPECT().FindByID(context.Background(), uint(21)).Return(
		&models.Actress{ID: 21, FirstName: "same", LastName: "name", ThumbURL: "old-thumb"}, nil)
	actresses.EXPECT().RenameIdentityFields(context.Background(), uint(21), "same", "name", "", "new-thumb").Return(errors.New("thumb write"))

	c := newTestCommitter(database.EditUnit{Actresses: actresses})
	err := c.Commit(context.Background(), &EditCommitPlan{Renames: []ActressRenamePlan{
		{ID: 21, FirstName: "same", LastName: "name", ThumbURL: "new-thumb", ThumbEdited: true},
	}})
	require.ErrorContains(t, err, "persist actress identity edit")
}
