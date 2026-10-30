package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestImportUpsertUnreadableDMMIdentityFailsClosedAndRecovers(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	actor := models.Actress{DMMID: 991234, FirstName: "Old", LastName: "Family"}
	created, err := repo.ImportUpsertWithOptions(t.Context(), &actor, ActressImportOptions{})
	require.NoError(t, err)
	require.Equal(t, "created", created.Outcome)
	require.NotZero(t, actor.ID)
	require.True(t, actor.Verified)
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: actor.ID, Language: "en", DisplayName: "Translated"}).Error)
	require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Stage Name", CanonicalName: "Family Old"}).Error)
	movie := models.Movie{ContentID: "unreadable-import-owner", ID: "UNREADABLE-OWNER", RenderGeneration: 4}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actor.ID, CreditedName: "Family Old", Origin: string(models.CreditOriginUser)}).Error)

	// SQLite's existing non-STRICT BOOLEAN column can contain externally
	// damaged data. Prove the actual native driver cannot hydrate this value
	// as a bool, rather than injecting a repository or query callback error.
	require.NoError(t, db.Exec("UPDATE actresses SET verified = ? WHERE id = ?", "not-a-boolean", actor.ID).Error)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	var verified bool
	err = sqlDB.QueryRowContext(t.Context(), "SELECT verified FROM actresses WHERE id = ?", actor.ID).Scan(&verified)
	require.ErrorContains(t, err, "verified")
	require.ErrorContains(t, err, "bool")
	var storedValue, storedType string
	require.NoError(t, sqlDB.QueryRowContext(t.Context(), "SELECT CAST(verified AS TEXT), typeof(verified) FROM actresses WHERE id = ?", actor.ID).Scan(&storedValue, &storedType))
	require.Equal(t, "not-a-boolean", storedValue)
	require.Equal(t, "text", storedType)

	// Untyped row snapshots retain the damaged value and every persisted
	// column, including import provenance, without asking GORM to repair it.
	snapshot := func() map[string][]map[string]any {
		t.Helper()
		state := make(map[string][]map[string]any)
		for _, table := range []string{"actresses", "actress_aliases", "actress_translations", "movies", "movie_credits", "movie_actresses"} {
			rows, err := sqlDB.QueryContext(t.Context(), "SELECT * FROM "+table+" ORDER BY rowid ASC")
			require.NoError(t, err)
			defer func() { _ = rows.Close() }()
			columns, err := rows.Columns()
			require.NoError(t, err)
			for rows.Next() {
				values := make([]any, len(columns))
				destinations := make([]any, len(columns))
				for i := range values {
					destinations[i] = &values[i]
				}
				require.NoError(t, rows.Scan(destinations...))
				row := make(map[string]any, len(columns))
				for i, column := range columns {
					row[column] = values[i]
				}
				state[table] = append(state[table], row)
			}
			require.NoError(t, rows.Err())
			require.NoError(t, rows.Close())
		}
		return state
	}
	before := snapshot()
	incoming := models.Actress{DMMID: actor.DMMID, FirstName: " New ", LastName: "Family"}
	callerBefore := incoming
	result, err := repo.ImportUpsertWithOptions(t.Context(), &incoming, ActressImportOptions{OverwriteNames: true})
	require.Nil(t, result)
	require.ErrorContains(t, err, "find import match dmm 991234")
	require.ErrorContains(t, err, "verified")
	require.ErrorContains(t, err, "bool")
	require.Equal(t, callerBefore, incoming, "do not assign identity, trim names, or claim success on failed hydration")
	require.Equal(t, before, snapshot(), "do not mutate identity, aliases, translations, credits, or render state")

	// Repair only the malformed scalar, then repeat the same public request.
	require.NoError(t, db.Exec("UPDATE actresses SET verified = ? WHERE id = ?", true, actor.ID).Error)
	result, err = repo.ImportUpsertWithOptions(t.Context(), &incoming, ActressImportOptions{OverwriteNames: true})
	require.NoError(t, err)
	require.Equal(t, "updated", result.Outcome)
	require.Equal(t, actor.ID, incoming.ID)
	require.Equal(t, actor.DMMID, incoming.DMMID)
	require.Equal(t, "New", incoming.FirstName)
	require.True(t, incoming.Verified)
	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
	require.EqualValues(t, 1, count, "retry must update the same identity, not create a fallback duplicate")
	stored, err := repo.FindByID(t.Context(), actor.ID)
	require.NoError(t, err)
	require.Equal(t, incoming, *stored)
	var credit models.MovieCredit
	require.NoError(t, db.First(&credit, "movie_content_id = ?", movie.ContentID).Error)
	require.Equal(t, actor.ID, credit.ActressID)
	require.Equal(t, "Family New", credit.CreditedName)
	dirty, generation := tagRenderState(t, db, movie.ContentID)
	require.True(t, dirty)
	require.Equal(t, movie.RenderGeneration+1, generation)
}
