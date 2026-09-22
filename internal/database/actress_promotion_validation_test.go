package database

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPromoteCandidateNormalizesAndValidatesFinalCanonicalFields(t *testing.T) {
	tests := []struct {
		name                         string
		first, last, japanese, thumb string
		wantErr                      bool
		wantFirst, wantLast, wantJP  string
		wantThumb                    string
	}{
		{name: "id only", wantErr: true},
		{name: "whitespace only", first: " ", last: "\t", japanese: "\n", thumb: " ", wantErr: true},
		{name: "last only", last: " Last ", wantErr: true},
		{name: "japanese only", japanese: "  日本名  ", wantJP: "日本名"},
		{name: "first only", first: "  First  ", wantFirst: "First"},
		{name: "trim every field", first: "  First  ", last: "  Last  ", japanese: "  日本名  ", thumb: "  thumb  ", wantFirst: "First", wantLast: "Last", wantJP: "日本名", wantThumb: "thumb"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			candidate := models.Actress{DMMID: 801, Origin: ActressOriginScrape, AmbiguityQuarantined: true}
			require.NoError(t, db.Create(&candidate).Error)
			movie := models.Movie{ContentID: "direct-promotion-validation", ID: "direct-promotion-validation", RenderGeneration: 13}
			require.NoError(t, db.Create(&movie).Error)
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, CreditedName: "reported"}
			require.NoError(t, db.Create(&credit).Error)
			collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, Status: models.CollisionStatusOpen}
			require.NoError(t, db.Create(&collision).Error)
			translation := models.ActressTranslation{ActressID: candidate.ID, Language: "en", DisplayName: "untouched"}
			require.NoError(t, db.Create(&translation).Error)

			err := repo.PromoteCandidate(t.Context(), candidate.ID, test.first, test.last, test.japanese, test.thumb)
			if !test.wantErr {
				require.NoError(t, err)
				var stored models.Actress
				require.NoError(t, db.First(&stored, candidate.ID).Error)
				require.True(t, stored.Verified)
				require.Equal(t, test.wantFirst, stored.FirstName)
				require.Equal(t, test.wantLast, stored.LastName)
				require.Equal(t, test.wantJP, stored.JapaneseName)
				require.Equal(t, test.wantThumb, stored.ThumbURL)
				return
			}

			require.ErrorIs(t, err, ErrInvalidLookup)
			require.Contains(t, err.Error(), "candidate")
			var stored models.Actress
			require.NoError(t, db.First(&stored, candidate.ID).Error)
			require.False(t, stored.Verified)
			require.Equal(t, ActressOriginScrape, stored.Origin)
			require.True(t, stored.AmbiguityQuarantined)
			require.Empty(t, stored.FirstName)
			require.Empty(t, stored.LastName)
			require.Empty(t, stored.JapaneseName)
			require.Empty(t, stored.ThumbURL)
			require.NoError(t, db.First(&translation, translation.ID).Error)
			require.NoError(t, db.First(&credit, credit.ID).Error)
			require.Equal(t, candidate.ID, credit.ActressID)
			require.NoError(t, db.First(&collision, collision.ID).Error)
			require.Equal(t, models.CollisionStatusOpen, collision.Status)
			require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
			require.False(t, movie.RenderDirty)
			require.Equal(t, int64(13), movie.RenderGeneration)
			var projectionCount int64
			require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ? AND actress_id = ?", movie.ContentID, candidate.ID).Count(&projectionCount).Error)
			require.Zero(t, projectionCount)
		})
	}
}

func TestPromoteCandidateValidationPrecedesDatabaseAccess(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	require.NoError(t, db.Close())
	err := repo.PromoteCandidate(t.Context(), 42, " ", "Last", " ", " thumb ")
	require.ErrorIs(t, err, ErrInvalidLookup)
	require.False(t, errors.Is(err, ErrCandidateAlreadyVerified))
}
