package database

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestForcedRefreshIdentityConstraintIsOperationOnly(t *testing.T) {
	ctx := context.Background()
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)
	stored, err := repo.Upsert(ctx, &models.Movie{ContentID: "original001", ID: "ORIGINAL-001", Title: "Original"})
	require.NoError(t, err)
	pending := stored.Clone()
	pending.RequireExactContentID = true
	pending.RefreshMovieID = stored.ID
	pending.RefreshExpectedContentID = stored.ContentID
	assert.Equal(t, stored.ContentID, pending.Clone().RefreshExpectedContentID)
	assert.Equal(t, stored.ID, pending.Clone().RefreshMovieID)
	assert.True(t, pending.Clone().RequireExactContentID, "deferred persistence retains the constraint")
	originalWire, err := json.Marshal(stored)
	require.NoError(t, err)
	pendingWire, err := json.Marshal(pending)
	require.NoError(t, err)
	assert.JSONEq(t, string(originalWire), string(pendingWire), "the constraint must not cross the wire")
	var roundTrip models.Movie
	require.NoError(t, json.Unmarshal([]byte(`{"id":"ORIGINAL-001","content_id":"original001","RequireExactContentID":true,"require_exact_content_id":true,"RefreshMovieID":"OTHER-001","refresh_movie_id":"OTHER-001","RefreshExpectedContentID":"other001","refresh_expected_content_id":"other001"}`), &roundTrip))
	assert.Empty(t, roundTrip.RefreshMovieID)
	assert.Empty(t, roundTrip.RefreshExpectedContentID)
	assert.False(t, db.Migrator().HasColumn(&models.Movie{}, "RefreshExpectedContentID"))
	assert.False(t, db.Migrator().HasColumn(&models.Movie{}, "RefreshMovieID"))
	assert.False(t, roundTrip.RequireExactContentID, "client JSON cannot set the internal operation flag")
	assert.False(t, db.Migrator().HasColumn(&models.Movie{}, "RequireExactContentID"))
	saved, err := repo.Upsert(ctx, pending)
	require.NoError(t, err)
	assert.False(t, pending.RequireExactContentID, "input constraint is consumed")
	assert.Empty(t, pending.RefreshMovieID)
	assert.Empty(t, saved.RefreshMovieID)
	assert.Empty(t, pending.RefreshExpectedContentID)
	assert.Empty(t, saved.RefreshExpectedContentID)
	assert.False(t, saved.RequireExactContentID, "persisted result is ordinary metadata")
	assert.Equal(t, stored.RenderGeneration, saved.RenderGeneration, "the flag is not a render input")
	assert.Equal(t, stored.RenderDirty, saved.RenderDirty)

	// Legacy/default fallback remains supported, including after a forced no-op.
	pending = stored.Clone()
	pending.ContentID = "legacy-alias"
	pending.Title = "Ordinary fallback"
	saved, err = repo.Upsert(ctx, pending)
	require.NoError(t, err)
	assert.Equal(t, "original001", saved.ContentID)
	assert.Equal(t, "Ordinary fallback", saved.Title)
}

func TestForcedRefreshIdentityCheckErrorRollsBack(t *testing.T) {
	ctx := context.Background()
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)
	stored, err := repo.Upsert(ctx, &models.Movie{ContentID: "original001", ID: "ORIGINAL-001", Title: "Original"})
	require.NoError(t, err)
	const callback = "force-refresh-identity-query-error"
	require.NoError(t, db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), "content_id <> ") {
			tx.AddError(assert.AnError)
		}
	}))
	pending := stored.Clone()
	pending.Title = "Rejected"
	pending.RequireExactContentID = true
	_, err = repo.Upsert(ctx, pending)
	require.ErrorIs(t, err, assert.AnError)
	assert.ErrorContains(t, err, "check refresh identity")
	assert.False(t, pending.RequireExactContentID)
	require.NoError(t, db.Callback().Query().Remove(callback))
	saved, err := repo.FindByContentID(ctx, stored.ContentID)
	require.NoError(t, err)
	assert.Equal(t, stored, saved)
}

func TestForcedRefreshKnownIdentityCannotBeRedirectedOrResurrected(t *testing.T) {
	for _, mode := range []string{"different_explicit", "missing_changed_display", "deleted_expected"} {
		t.Run(mode, func(t *testing.T) {
			db := newDatabaseTestDB(t)
			repo := NewMovieRepository(db)
			ctx := t.Context()
			original, err := repo.Upsert(ctx, &models.Movie{ID: "ORIGINAL-001", ContentID: "original001", Title: "Original"})
			require.NoError(t, err)
			other, err := repo.Upsert(ctx, &models.Movie{ID: "OTHER-002", ContentID: "other002", Title: "Other"})
			require.NoError(t, err)
			candidate := &models.Movie{ID: original.ID, ContentID: other.ContentID, Title: "Must reject", RequireExactContentID: true, RefreshMovieID: original.ID, RefreshExpectedContentID: original.ContentID}
			switch mode {
			case "missing_changed_display":
				candidate.ContentID = ""
				candidate.RequireExactContentID = false
				candidate.ID = other.ID
			case "deleted_expected":
				require.NoError(t, repo.Delete(ctx, original.ID))
				candidate.ContentID = original.ContentID
			}
			var before []models.Movie
			require.NoError(t, db.Order("content_id").Find(&before).Error)
			_, err = repo.Upsert(ctx, candidate)
			require.ErrorIs(t, err, ErrMovieContentIDConflict)
			assert.Empty(t, candidate.RefreshMovieID)
			assert.Empty(t, candidate.RefreshExpectedContentID)
			assert.False(t, candidate.RequireExactContentID)
			var after []models.Movie
			require.NoError(t, db.Order("content_id").Find(&after).Error)
			assert.Equal(t, before, after)
		})
	}
}
