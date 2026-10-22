package database

import (
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestImportUserAliasSparsePreservesEntireIdentity(t *testing.T) {
	for _, verified := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			for _, sparse := range []bool{false, true} {
				t.Run(fmt.Sprintf("verified=%t/fail=%t/sparse=%t", verified, fail, sparse), func(t *testing.T) {
					db := newCreditTestDB(t)
					repo := NewActressRepository(db)
					a := models.Actress{DMMID: 0, FirstName: "", LastName: "Family", JapaneseName: "", ThumbURL: "manual.jpg", Aliases: "Old", Origin: ActressOriginUser, Verified: verified, AmbiguityQuarantined: true}
					require.NoError(t, db.Create(&a).Error)
					movie := models.Movie{ID: "ALIAS-001", ContentID: "alias001", RenderGeneration: 7}
					require.NoError(t, db.Create(&movie).Error)
					credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: a.ID, CreditedName: "Original evidence", UserOverride: true, Suppressed: true}
					require.NoError(t, db.Create(&credit).Error)
					alias := models.ActressAlias{AliasName: "Old claim", CanonicalName: "Family"}
					require.NoError(t, db.Create(&alias).Error)
					before, err := repo.FindByID(t.Context(), a.ID)
					require.NoError(t, err)
					if fail {
						require.NoError(t, db.Exec("CREATE TRIGGER fail_user_alias BEFORE UPDATE OF aliases ON actresses BEGIN SELECT RAISE(ABORT, 'alias metadata failure'); END").Error)
					}
					incoming := models.Actress{ID: a.ID, DMMID: 999, FirstName: "Forbidden", LastName: "Forbidden", JapaneseName: "Forbidden", ThumbURL: "forbidden.jpg", Aliases: "New"}
					if sparse {
						incoming = models.Actress{ID: a.ID, Aliases: "New"}
					}
					result, err := repo.ImportUpsertWithOptions(t.Context(), &incoming, ActressImportOptions{OverwriteNames: true})
					if fail {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
						require.Equal(t, "updated", result.Outcome)
					}
					got, err := repo.FindByID(t.Context(), a.ID)
					require.NoError(t, err)
					if !fail {
						require.Equal(t, "New", got.Aliases)
						got.Aliases = before.Aliases
						got.UpdatedAt = before.UpdatedAt
						require.Equal(t, "New", incoming.Aliases)
						require.Empty(t, incoming.FirstName)
						require.Zero(t, incoming.DMMID)
					}
					require.Equal(t, before, got)
					var afterMovie models.Movie
					require.NoError(t, db.First(&afterMovie, "content_id = ?", movie.ContentID).Error)
					require.Equal(t, movie.RenderGeneration, afterMovie.RenderGeneration)
					require.Equal(t, movie.RenderDirty, afterMovie.RenderDirty)
					var afterCredit models.MovieCredit
					require.NoError(t, db.First(&afterCredit, credit.ID).Error)
					require.Equal(t, credit, afterCredit)
					var afterAliases []models.ActressAlias
					require.NoError(t, db.Find(&afterAliases).Error)
					require.Equal(t, []models.ActressAlias{alias}, afterAliases)
				})
			}
		}
	}
}
