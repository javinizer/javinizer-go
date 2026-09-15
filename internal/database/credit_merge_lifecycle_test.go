package database

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
)

func TestCreditMergeMovesMembershipAndTranslations(t *testing.T) {
	for _, overlap := range []bool{false, true} {
		t.Run(map[bool]string{false: "move", true: "combine"}[overlap], func(t *testing.T) {
			db, _, source, collision := collisionFixture(t)
			source.UserOverride = true
			source.OverrideName = "Mine"
			source.Origin = "user"
			source.OrderPinned = true
			source.OrderIndex = 8
			source.Suppressed = true
			source.DisplayForceCanonical = true
			require.NoError(t, db.Save(&source).Error)
			target := models.Actress{FirstName: "Target", Verified: true}
			require.NoError(t, db.Create(&target).Error)
			var dest models.MovieCredit
			if overlap {
				dest = models.MovieCredit{MovieContentID: source.MovieContentID, ActressID: target.ID}
				require.NoError(t, db.Create(&dest).Error)
			}
			translation := models.ActressTranslation{ActressID: source.ActressID, Language: "en", FirstName: "Translated", SourceName: "Original Truth"}
			require.NoError(t, db.Create(&translation).Error)
			if overlap {
				require.NoError(t, db.Create(&models.ActressTranslation{ActressID: target.ID, Language: "en", FirstName: "Target Translation"}).Error)
			}
			require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return moveCredits(tx, source.ActressID, target.ID) }))
			var credits []models.MovieCredit
			require.NoError(t, db.Where("movie_content_id = ?", source.MovieContentID).Find(&credits).Error)
			require.Len(t, credits, 1)
			require.Equal(t, target.ID, credits[0].ActressID)
			require.True(t, credits[0].UserOverride)
			require.Equal(t, "Mine", credits[0].OverrideName)
			require.True(t, credits[0].Suppressed)
			require.True(t, credits[0].DisplayForceCanonical)
			require.True(t, credits[0].OrderPinned)
			require.Equal(t, 8, credits[0].OrderIndex)
			require.NoError(t, db.First(&collision, collision.ID).Error)
			require.Equal(t, credits[0].ID, collision.CreditID)
			var translations []models.ActressTranslation
			require.NoError(t, db.Where("actress_id = ?", target.ID).Find(&translations).Error)
			require.Len(t, translations, 1)
			if overlap {
				require.Equal(t, "Target Translation", translations[0].FirstName)
			} else {
				require.Equal(t, "Translated", translations[0].FirstName)
			}
		})
	}
}
