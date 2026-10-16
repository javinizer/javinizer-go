package batch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func previewTableSnapshot(t *testing.T, db *database.DB) string {
	t.Helper()
	var tables []string
	require.NoError(t, db.Raw("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name").Scan(&tables).Error)
	out := map[string]any{}
	for _, table := range tables {
		var rows []map[string]interface{}
		require.NoError(t, db.Raw(fmt.Sprintf("SELECT * FROM %q ORDER BY rowid", table)).Find(&rows).Error)
		out[table] = rows
	}
	data, err := json.Marshal(out)
	require.NoError(t, err)
	return string(data)
}

func TestPreviewCastSkipsUnchangedAndStaleEchoes(t *testing.T) {
	old := &models.Movie{Actresses: []models.Actress{{ID: 1, FirstName: "Original", Verified: true, Origin: "user"}}}
	current := old.Clone()
	current.Actresses[0].FirstName = "Current"
	for _, tc := range []struct {
		name    string
		cast    *models.Movie
		version string
	}{
		{"omitted", &models.Movie{}, ""},
		{"current echo", current, models.ActressCastVersion(current.Actresses)},
		{"old echo old version", old, models.ActressCastVersion(old.Actresses)},
		{"old echo no version", old, ""},
		{"old echo current version", old, models.ActressCastVersion(current.Actresses)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			proposed := contracts.MovieViewToModel(contracts.MovieViewFromModel(tc.cast))
			projection, resolveErr := previewProposedCast(t.Context(), nil, current, old, proposed, previewCastView(proposed, tc.version), true)
			require.Nil(t, resolveErr)
			require.Same(t, current, projection, "read-only echoes must not require a write transaction")
		})
	}
}

func TestPreviewCastErrorsNeverReturnProvisionalMovie(t *testing.T) {
	for _, mode := range []string{"no database", "persisted missing", "cast changed", "lookup failed", "rename failed", "upsert failed", "rollback failed", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			deps := createTestDeps(t, newDisplayTitlePreviewConfig("<ACTORS>"), "")
			a := models.Actress{FirstName: "Original", Verified: true, Origin: "user"}
			require.NoError(t, deps.Repos.DB.Create(&a).Error)
			authority, err := deps.Repos.MovieRepo.Upsert(t.Context(), &models.Movie{ContentID: "ERROR-401", ID: "ERROR-401", Title: "Title", Actresses: []models.Actress{a}})
			require.NoError(t, err)
			baseline := authority.Clone()
			candidate := contracts.MovieViewToModel(contracts.MovieViewFromModel(authority))
			candidate.Actresses[0].FirstName = "Edited"
			version := models.ActressCastVersion(authority.Actresses)
			db := deps.Repos.DB
			ctx := t.Context()
			status := http.StatusInternalServerError
			switch mode {
			case "no database":
				db, status = nil, http.StatusServiceUnavailable
			case "persisted missing":
				authority.ContentID, candidate.ContentID = "MISSING", "MISSING"
				status = http.StatusConflict
			case "cast changed":
				require.NoError(t, db.Model(&models.Actress{}).Where("id=?", a.ID).Update("first_name", "Concurrent").Error)
				status = http.StatusConflict
			case "lookup failed":
				require.NoError(t, db.Exec("ALTER TABLE movies RENAME TO hidden_movies").Error)
			case "rename failed":
				require.NoError(t, db.Exec("CREATE TRIGGER deny_preview_rename BEFORE UPDATE ON actresses BEGIN SELECT RAISE(ABORT,'denied rename'); END").Error)
			case "upsert failed":
				require.NoError(t, db.Exec("CREATE TRIGGER deny_preview_movie BEFORE UPDATE OF title ON movies BEGIN SELECT RAISE(ABORT,'denied upsert'); END").Error)
			case "rollback failed":
				pool, err := db.DB.DB()
				require.NoError(t, err)
				pool.SetMaxOpenConns(1)
				conn, err := pool.Conn(t.Context())
				require.NoError(t, err)
				require.NoError(t, conn.Raw(func(c any) error {
					c.(*sqlite3.SQLiteConn).RegisterAuthorizer(func(op int, a, _, _ string) int {
						if op == sqlite3.SQLITE_TRANSACTION && a == "ROLLBACK" {
							return sqlite3.SQLITE_DENY
						}
						return sqlite3.SQLITE_OK
					})
					return nil
				}))
				require.NoError(t, conn.Close())
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				status = http.StatusServiceUnavailable
			}
			before := previewTableSnapshot(t, deps.Repos.DB)
			original := candidate.Clone()
			projected, resolveErr := previewProposedCast(ctx, db, authority, baseline, candidate, previewCastView(candidate, version), true)
			require.Nil(t, projected)
			require.NotNil(t, resolveErr)
			require.Equal(t, status, resolveErr.Status, resolveErr.Err)
			require.Equal(t, before, previewTableSnapshot(t, deps.Repos.DB))
			require.Equal(t, original, candidate)
		})
	}
}

func TestPreviewCastCreatesOnlyProvisionalNewIdentities(t *testing.T) {
	deps := createTestDeps(t, newDisplayTitlePreviewConfig("<ACTORS>"), "")
	authority := &models.Movie{ContentID: "NEW-401", ID: "NEW-401", Title: "Title", Actresses: []models.Actress{}}
	candidate := authority.Clone()
	candidate.Actresses = []models.Actress{{FirstName: "New"}}
	before := previewTableSnapshot(t, deps.Repos.DB)
	projection, resolveErr := previewProposedCast(t.Context(), deps.Repos.DB, authority, authority, candidate, previewCastView(candidate, models.ActressCastVersion(authority.Actresses)), false)
	require.Nil(t, resolveErr)
	require.Len(t, projection.Credits, 1)
	require.NotZero(t, projection.Credits[0].ActressID)
	require.Equal(t, "New", projection.Credits[0].Actress.FirstName)
	require.Zero(t, candidate.Actresses[0].ID)
	require.Equal(t, before, previewTableSnapshot(t, deps.Repos.DB))
}

func previewCastView(movie *models.Movie, version string) *contracts.MovieView {
	view := contracts.MovieViewFromModel(movie)
	view.CastVersion = version
	return view
}
