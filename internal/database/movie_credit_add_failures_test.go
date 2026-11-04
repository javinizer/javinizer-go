package database

import (
	"errors"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// These callbacks inject application-visible GORM errors at storage boundaries.
// They exercise error propagation and transaction rollback, not actual disk or
// driver failures. Every case starts with valid movie, actress, and cast data.
func TestAddUserCreditInjectedStorageFailuresRemainAtomic(t *testing.T) {
	for _, tc := range []struct {
		name, operation string
		existing        bool
		occurrence      int
		matches         func(*gorm.DB) bool
	}{
		{"write admission", "raw", false, 1, func(tx *gorm.DB) bool {
			return strings.HasPrefix(tx.Statement.SQL.String(), "UPDATE movies SET content_id = content_id WHERE 0")
		}},
		{"movie lookup", "query", false, 1, func(tx *gorm.DB) bool {
			_, ok := tx.Statement.Dest.(*[]models.Movie)
			return ok
		}},
		{"actress lookup", "query", false, 1, func(tx *gorm.DB) bool {
			_, ok := tx.Statement.Dest.(*models.Actress)
			return ok
		}},
		{"legacy cast lookup", "query", false, 1, func(tx *gorm.DB) bool {
			return tx.Statement.Table == "actresses" && len(tx.Statement.Joins) > 0
		}},
		{"legacy order lookup", "row", false, 1, userCreditOrderLookup},
		{"legacy credit creation", "create", false, 1, func(tx *gorm.DB) bool {
			credit, ok := tx.Statement.Dest.(*models.MovieCredit)
			return ok && credit.LegacyInferred
		}},
		{"existing credit lookup", "query", true, 1, func(tx *gorm.DB) bool {
			_, ok := tx.Statement.Dest.(*models.MovieCredit)
			return ok && len(tx.Statement.Preloads) == 0
		}},
		{"credit ownership update", "update", true, 1, func(tx *gorm.DB) bool {
			return tx.Statement.Table == "movie_credits"
		}},
		{"new credit order lookup", "row", false, 2, userCreditOrderLookup},
		{"new credit creation", "create", false, 1, func(tx *gorm.DB) bool {
			credit, ok := tx.Statement.Dest.(*models.MovieCredit)
			return ok && !credit.LegacyInferred
		}},
		{"cast projection insert", "raw", false, 1, func(tx *gorm.DB) bool {
			return strings.HasPrefix(tx.Statement.SQL.String(), "INSERT OR IGNORE INTO movie_actresses")
		}},
		{"quarantine revision lookup", "query", false, 1, func(tx *gorm.DB) bool {
			return tx.Statement.Table == "actress_quarantine_state"
		}},
		{"result snapshot lookup", "query", false, 1, func(tx *gorm.DB) bool {
			_, ok := tx.Statement.Dest.(*models.MovieCredit)
			return ok && len(tx.Statement.Preloads) > 0
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			legacy := seedUserCreditActress(t, db, "Legacy")
			added := seedUserCreditActress(t, db, "Added")
			const contentID = "credit-storage-failure"
			seedArtifactPublicationMovie(t, db, contentID, 7, false)
			require.NoError(t, db.Exec("INSERT INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)", contentID, legacy.ID).Error)
			if tc.existing {
				credit := models.MovieCredit{MovieContentID: contentID, ActressID: added.ID, Origin: string(models.CreditOriginScrape), CreditedName: "Reported", OverrideName: "Custom", UserOverride: true, OrderPinned: true, OrderIndex: 4}
				require.NoError(t, db.Create(&credit).Error)
			}
			var beforeCredits []models.MovieCredit
			require.NoError(t, db.Order("id").Find(&beforeCredits).Error)
			var beforeActresses []models.Actress
			require.NoError(t, db.Order("id").Find(&beforeActresses).Error)
			var beforeRevision struct{ Revision, ConsumedRevision int64 }
			require.NoError(t, db.Table("actress_quarantine_state").Where("id = 1").Take(&beforeRevision).Error)

			injected := errors.New("injected add-user-credit storage failure")
			seen, fired := 0, false
			callback := func(tx *gorm.DB) {
				if fired || !tc.matches(tx) {
					return
				}
				seen++
				if seen == tc.occurrence {
					fired = true
					_ = tx.AddError(injected)
				}
			}
			const hook = "test:add_user_credit_storage_failure"
			var register func(string, func(*gorm.DB)) error
			var remove func(string) error
			switch tc.operation {
			case "query":
				register, remove = db.Callback().Query().Before("gorm:query").Register, db.Callback().Query().Remove
			case "row":
				register, remove = db.Callback().Row().Before("gorm:row").Register, db.Callback().Row().Remove
			case "create":
				register, remove = db.Callback().Create().Before("gorm:create").Register, db.Callback().Create().Remove
			case "update":
				register, remove = db.Callback().Update().Before("gorm:update").Register, db.Callback().Update().Remove
			case "raw":
				register, remove = db.Callback().Raw().Before("gorm:raw").Register, db.Callback().Raw().Remove
			default:
				t.Fatalf("unknown callback operation %q", tc.operation)
			}
			require.NoError(t, register(hook, callback))
			t.Cleanup(func() { _ = remove(hook) })

			result, err := NewMovieCreditRepository(db).AddUserCredit(t.Context(), contentID, added.ID)
			require.True(t, fired, "the requested storage boundary must be reached")
			require.ErrorIs(t, err, injected)
			require.Nil(t, result, "a failed transaction must not return an uncommitted credit")
			require.NoError(t, remove(hook))

			var afterCredits []models.MovieCredit
			require.NoError(t, db.Order("id").Find(&afterCredits).Error)
			require.Equal(t, beforeCredits, afterCredits, "legacy materialization and target credit changes must roll back")
			var projection []uint
			require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", contentID).Pluck("actress_id", &projection).Error)
			require.Equal(t, []uint{legacy.ID}, projection)
			var afterActresses []models.Actress
			require.NoError(t, db.Order("id").Find(&afterActresses).Error)
			require.Equal(t, beforeActresses, afterActresses)
			var afterRevision struct{ Revision, ConsumedRevision int64 }
			require.NoError(t, db.Table("actress_quarantine_state").Where("id = 1").Take(&afterRevision).Error)
			require.Equal(t, beforeRevision, afterRevision, "quarantine trigger revisions must roll back too")
			movie := loadArtifactPublicationMovie(t, db, contentID)
			require.Equal(t, int64(7), movie.RenderGeneration)
			require.False(t, movie.RenderDirty)

			// The same public operation succeeds after the temporary fault is gone.
			result, err = NewMovieCreditRepository(db).AddUserCredit(t.Context(), contentID, added.ID)
			require.NoError(t, err)
			require.Equal(t, added.ID, result.ActressID)
			require.Equal(t, string(models.CreditOriginUser), result.Origin)
			var credits []models.MovieCredit
			require.NoError(t, db.Where("movie_content_id = ?", contentID).Find(&credits).Error)
			require.Len(t, credits, 2)
		})
	}
}

func userCreditOrderLookup(tx *gorm.DB) bool {
	return tx.Statement.Table == "movie_credits" &&
		len(tx.Statement.Selects) == 1 && tx.Statement.Selects[0] == "COALESCE(MAX(order_index), -1) + 1"
}

func TestAddUserCreditRejectsMovieCreatedWithoutContentID(t *testing.T) {
	db := newCreditTestDB(t)
	movies := NewMovieRepository(db)
	actress := seedUserCreditActress(t, db, "Verified")
	// Unlike UpsertWithTranslations, the public Create operation accepts a
	// legacy ID without deriving ContentID. AddUserCredit must reject this
	// reachable input rather than attach credits to an empty movie key.
	movie := models.Movie{ID: "legacy-id", Title: "Legacy movie", RenderGeneration: 3}
	require.NoError(t, movies.Create(t.Context(), &movie))
	before, err := movies.FindByID(t.Context(), movie.ID)
	require.NoError(t, err)
	require.Equal(t, "", before.ContentID)

	credit, err := NewMovieCreditRepository(db).AddUserCredit(t.Context(), movie.ID, actress.ID)
	require.ErrorIs(t, err, ErrInvalidLookup)
	require.ErrorContains(t, err, "has no content ID")
	require.Nil(t, credit)

	after, err := movies.FindByID(t.Context(), movie.ID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	var count int64
	require.NoError(t, db.Model(&models.MovieCredit{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, db.Table("movie_actresses").Count(&count).Error)
	require.Zero(t, count)
}
