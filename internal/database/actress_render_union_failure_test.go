package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestActressMutationsAbortWhenAffectedMovieUnionCannotBeRead(t *testing.T) {
	operations := []struct {
		name string
		run  func(*ActressRepository, models.Actress) error
	}{
		{
			name: "update",
			run: func(repo *ActressRepository, actress models.Actress) error {
				actress.FirstName = "Changed"
				return repo.Update(t.Context(), &actress)
			},
		},
		{
			name: "rename",
			run: func(repo *ActressRepository, actress models.Actress) error {
				return repo.RenameNameFields(t.Context(), actress.ID, "Changed", actress.LastName, actress.JapaneseName)
			},
		},
		{
			name: "delete",
			run: func(repo *ActressRepository, actress models.Actress) error {
				return repo.Delete(t.Context(), actress.ID)
			},
		},
		{
			name: "canonical update",
			run: func(repo *ActressRepository, actress models.Actress) error {
				return repo.UpdateCanonicalFields(t.Context(), actress.ID, "Changed", actress.LastName, actress.JapaneseName, "changed-thumb")
			},
		},
		{
			name: "import update",
			run: func(repo *ActressRepository, actress models.Actress) error {
				incoming := actress
				incoming.FirstName = "Imported"
				return repo.ImportUpsert(t.Context(), &incoming)
			},
		},
	}

	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			original := models.Actress{
				DMMID: 91001, FirstName: "Original", LastName: "Identity",
				JapaneseName: "元女優", ThumbURL: "original-thumb", Origin: ActressOriginScrape,
			}
			require.NoError(t, db.Create(&original).Error)
			require.NoError(t, db.Migrator().DropTable("movie_actresses"))

			err := operation.run(NewActressRepository(db), original)
			require.ErrorContains(t, err, "movie_actresses")

			var stored models.Actress
			require.NoError(t, db.First(&stored, original.ID).Error)
			require.Equal(t, original, stored, "failed mutation must preserve the complete persisted identity")
		})
	}
}

func TestCollisionResolutionAbortsWhenAffectedMovieUnionCannotBeRead(t *testing.T) {
	db, service, credit, collision := collisionFixture(t)
	require.NoError(t, db.Migrator().DropTable("movie_actresses"))

	remaining, err := service.Resolve(t.Context(), collision.ID, models.CollisionResolutionKeepIdentity, 0)
	require.ErrorContains(t, err, "movie_actresses")
	require.Zero(t, remaining)

	var storedCollision models.CreditCollision
	require.NoError(t, db.First(&storedCollision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, storedCollision.Status)
	require.Empty(t, storedCollision.Resolution)
	var storedCredit models.MovieCredit
	require.NoError(t, db.First(&storedCredit, credit.ID).Error)
	require.Equal(t, credit.ActressID, storedCredit.ActressID)
	var movie models.Movie
	require.NoError(t, db.First(&movie, "content_id = ?", credit.MovieContentID).Error)
	require.False(t, movie.RenderDirty)
	require.Zero(t, movie.RenderGeneration)
}
