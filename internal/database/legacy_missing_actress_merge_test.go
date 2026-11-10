package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMoveMovieAssociationsDoesNotInventCastForMissingLegacyActress(t *testing.T) {
	db := newDatabaseTestDB(t)
	// Older databases can contain join rows whose actress no longer exists.
	// Such rows locate a movie but must not authorize replacing its real cast.
	require.NoError(t, db.Exec("PRAGMA foreign_keys = OFF").Error)
	target := models.Actress{FirstName: "Target", DMMID: 73101}
	other := models.Actress{FirstName: "Unrelated", DMMID: 73102}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&other).Error)
	const missingID uint = 999999
	var missingCount int64
	require.NoError(t, db.Model(&models.Actress{}).Where("id = ?", missingID).Count(&missingCount).Error)
	require.Zero(t, missingCount)
	for _, withTarget := range []bool{false, true} {
		contentID := "missing-legacy-actress"
		if withTarget {
			contentID += "-with-target"
		}
		movie := models.Movie{ContentID: contentID, ID: contentID}
		require.NoError(t, db.Create(&movie).Error)
		require.NoError(t, db.Model(&movie).Association("Actresses").Append(&other))
		if withTarget {
			require.NoError(t, db.Model(&movie).Association("Actresses").Append(&target))
		}
		require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", contentID, missingID).Error)
	}
	type association struct {
		MovieContentID string
		ActressID      uint
	}
	var before []association
	require.NoError(t, db.Table("movie_actresses").Order("movie_content_id, actress_id").Find(&before).Error)
	require.Len(t, before, 5)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		count, err := moveMovieAssociations(tx, missingID, target.ID)
		require.NoError(t, err)
		require.Zero(t, count, "an orphan join row is not a loaded source identity")
		return err
	}))
	var after []association
	require.NoError(t, db.Table("movie_actresses").Order("movie_content_id, actress_id").Find(&after).Error)
	require.Equal(t, before, after, "skipping the missing identity must preserve both real cast and legacy evidence")
}
