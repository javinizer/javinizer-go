package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMoveMovieAssociationsPreservesExactCastAndIsIdempotent(t *testing.T) {
	for _, sourceFirst := range []bool{true, false} {
		name := "target_has_lower_id"
		if sourceFirst {
			name = "source_has_lower_id"
		}
		t.Run(name, func(t *testing.T) {
			db := newDatabaseTestDB(t)
			source := models.Actress{FirstName: "Source", DMMID: 74101}
			target := models.Actress{FirstName: "Target", DMMID: 74102}
			other := models.Actress{FirstName: "Unrelated", DMMID: 74103}
			if sourceFirst {
				require.NoError(t, db.Create(&source).Error)
				require.NoError(t, db.Create(&target).Error)
				require.Less(t, source.ID, target.ID)
			} else {
				require.NoError(t, db.Create(&target).Error)
				require.NoError(t, db.Create(&source).Error)
				require.Less(t, target.ID, source.ID)
			}
			require.NoError(t, db.Create(&other).Error)

			movies := []struct {
				id   string
				cast []models.Actress
				want []uint
			}{
				{"source-only", []models.Actress{source}, []uint{target.ID}},
				{"source-and-other", []models.Actress{source, other}, []uint{target.ID, other.ID}},
				{"source-target-and-other", []models.Actress{source, target, other}, []uint{target.ID, other.ID}},
				{"target-and-other", []models.Actress{target, other}, []uint{target.ID, other.ID}},
				{"other-only", []models.Actress{other}, []uint{other.ID}},
				{"empty-cast", nil, nil},
			}
			for _, fixture := range movies {
				movie := models.Movie{ContentID: fixture.id, ID: fixture.id, Actresses: fixture.cast}
				require.NoError(t, db.Create(&movie).Error)
			}

			assertCast := func() {
				t.Helper()
				for _, fixture := range movies {
					var movie models.Movie
					require.NoError(t, db.Preload("Actresses").First(&movie, "content_id = ?", fixture.id).Error)
					var ids []uint
					for _, actress := range movie.Actresses {
						ids = append(ids, actress.ID)
					}
					require.ElementsMatch(t, fixture.want, ids, "cast for %s", fixture.id)
					// Check persisted join rows too: a preload alone could hide stale
					// references to a missing actress or duplicate target entries.
					var storedIDs []uint
					require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", fixture.id).Pluck("actress_id", &storedIDs).Error)
					require.ElementsMatch(t, fixture.want, storedIDs, "stored cast for %s", fixture.id)
				}
			}

			for _, wantCount := range []int{3, 0} {
				require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
					count, err := moveMovieAssociations(tx, source.ID, target.ID)
					require.NoError(t, err)
					require.Equal(t, wantCount, count)
					return err
				}))
				assertCast()
			}
		})
	}
}
