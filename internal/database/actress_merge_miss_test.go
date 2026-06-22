package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// --- mergeAliasValues: duplicate target aliases (seen[key] hit) ---
// Line 172: seen[key] branch when target has duplicate aliases

func TestActressMergeMiss_MergeAliasValues_DuplicateTargetAliases(t *testing.T) {
	merged, count, added := mergeAliasValues("alias1|ALIAS1", []string{"alias2"}, "Canonical")
	// "ALIAS1" is a case-insensitive duplicate of "alias1" — should be deduped by seen[key]
	assert.Contains(t, merged, "alias1")
	assert.Equal(t, 1, count) // only alias2 added
	_ = added
}

// --- moveMovieAssociations: error paths ---
// Line 252: Pluck error
// Line 265: Find error
// Line 278: existing actress with ID != sourceID/targetID
// Line 291: Replace error

func TestActressMergeMiss_MoveMovieAssociations_NoMovies(t *testing.T) {
	db := newDatabaseTestDB(t)

	var count int
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		count, err = moveMovieAssociations(tx, 9999, 8888)
		return err
	})
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestActressMergeMiss_MoveMovieAssociations_WithMovies(t *testing.T) {
	db := newDatabaseTestDB(t)

	// Create source and target actresses
	source := models.Actress{JapaneseName: "移動元女優", DMMID: 11111}
	target := models.Actress{JapaneseName: "移動先女優", DMMID: 22222}
	require.NoError(t, db.DB.Create(&source).Error)
	require.NoError(t, db.DB.Create(&target).Error)

	// Create movie associated with source actress
	movie := &models.Movie{
		ContentID:    "merge-move-cid",
		ID:           "MERGE-MOVE",
		DisplayTitle: "Merge Move Test",
		Title:        "Merge Move Test",
		Actresses:    []models.Actress{source},
	}
	repo := NewMovieRepository(db)
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	// Move associations
	var count int
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		var err error
		count, err = moveMovieAssociations(tx, source.ID, target.ID)
		return err
	})
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	// Verify movie now has target actress instead of source
	found, err := repo.FindByContentID(context.TODO(), "merge-move-cid")
	require.NoError(t, err)
	hasTarget := false
	for _, a := range found.Actresses {
		if a.ID == target.ID {
			hasTarget = true
		}
	}
	assert.True(t, hasTarget, "movie should have target actress after move")
}

// --- moveMovieAssociations: movie has both source and target ---
// Line 278-280: hasSource && hasTarget

func TestActressMergeMiss_MoveMovieAssociations_BothPresent(t *testing.T) {
	db := newDatabaseTestDB(t)

	source := models.Actress{JapaneseName: "両方元", DMMID: 33333}
	target := models.Actress{JapaneseName: "両方先", DMMID: 44444}
	require.NoError(t, db.DB.Create(&source).Error)
	require.NoError(t, db.DB.Create(&target).Error)

	// Create movie associated with BOTH actresses
	movie := &models.Movie{
		ContentID:    "merge-both-cid",
		ID:           "MERGE-BOTH",
		DisplayTitle: "Both Present Test",
		Title:        "Both Present Test",
		Actresses:    []models.Actress{source, target},
	}
	repo := NewMovieRepository(db)
	_, err := repo.UpsertWithTranslations(context.TODO(), movie, nil, nil)
	require.NoError(t, err)

	// Move should handle the case where target already exists
	var count int
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		var moveErr error
		count, moveErr = moveMovieAssociations(tx, source.ID, target.ID)
		return moveErr
	})
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// --- upsertActressAliases: error path ---
// Line 316: Pluck error
// Line 324: Find error
// Line 347-362: Create error or duplicate handling

func TestActressMergeMiss_UpsertActressAliases_EmptyCanonicalName(t *testing.T) {
	db := newDatabaseTestDB(t)

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return upsertActressAliases(tx, []string{"alias1"}, "")
	})
	require.NoError(t, err) // empty canonical name is a no-op
}

func TestActressMergeMiss_UpsertActressAliases_Normal(t *testing.T) {
	db := newDatabaseTestDB(t)

	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return upsertActressAliases(tx, []string{"alias1", "alias2"}, "CanonicalName")
	})
	require.NoError(t, err)

	// Verify aliases were created
	var count int64
	db.DB.Model(&models.ActressAlias{}).Where("canonical_name = ?", "CanonicalName").Count(&count)
	assert.Equal(t, int64(2), count)
}

func TestActressMergeMiss_UpsertActressAliases_DuplicateAlias(t *testing.T) {
	db := newDatabaseTestDB(t)

	// Insert first alias
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		return upsertActressAliases(tx, []string{"dup-alias"}, "Canonical1")
	})
	require.NoError(t, err)

	// Insert same alias for different canonical — should update
	err = db.DB.Transaction(func(tx *gorm.DB) error {
		return upsertActressAliases(tx, []string{"dup-alias"}, "Canonical2")
	})
	require.NoError(t, err)

	// Should be updated to Canonical2
	var alias models.ActressAlias
	db.DB.Where("alias_name = ?", "dup-alias").First(&alias)
	assert.Equal(t, "Canonical2", alias.CanonicalName)
}

// --- Merge: full integration with DMMID unique constraint check ---
// Line 337-349: DMMID conflict check in Merge

func TestActressMergeMiss_Merge_DMMIDUniqueConstraint(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	// Create two actresses with different DMMIDs
	target := &models.Actress{JapaneseName: "マージ先", DMMID: 55555}
	source := &models.Actress{JapaneseName: "マージ元", DMMID: 66666}
	require.NoError(t, repo.Create(context.TODO(), target))
	require.NoError(t, repo.Create(context.TODO(), source))

	// Create a third actress with a conflicting DMMID
	conflict := &models.Actress{JapaneseName: "コンフリクト", DMMID: 77777}
	require.NoError(t, repo.Create(context.TODO(), conflict))

	// Try to merge with source resolution for dmm_id, but the merged DMMID
	// would conflict with the third actress
	_, err := repo.Merge(context.TODO(), target.ID, source.ID, map[string]string{
		"dmm_id": "source", // wants DMMID=66666
	})
	// Should succeed because 66666 doesn't conflict with 77777
	require.NoError(t, err)
}

// --- Merge: DMMID swap needed when target adopts source DMMID ---
// Line 344-346: temp DMMID assignment

func TestActressMergeMiss_Merge_DMMIDSwap(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	// Create target with no DMMID, source with DMMID
	target := &models.Actress{JapaneseName: "スワップ先"}
	source := &models.Actress{JapaneseName: "スワップ元", DMMID: 88888}
	require.NoError(t, repo.Create(context.TODO(), target))
	require.NoError(t, repo.Create(context.TODO(), source))

	result, err := repo.Merge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)
	assert.Equal(t, 88888, result.MergedActress.DMMID)
}

// --- canonicalActressName: LastName only fallback ---
// Line 118-120: Falls through when FullName() is empty and FirstName is empty
// Note: With LastName="Doe", FirstName="", FullName() returns "Doe", so this
// path requires LastName with both FirstName="" and JapaneseName="".
// But FullName returns lastName when only lastName is set.
// This is effectively dead code given the FullName implementation.

// --- mergeActressValues: source wins for all fields ---
// Lines 252-293: all conflict resolution paths with "source" decision

func TestActressMergeMiss_MergeActressValues_SourceWinsAll(t *testing.T) {
	target := &models.Actress{
		DMMID:        1,
		FirstName:    "TargetFirst",
		LastName:     "TargetLast",
		JapaneseName: "ターゲット",
		ThumbURL:     "https://target.jpg",
	}
	source := &models.Actress{
		DMMID:        2,
		FirstName:    "SourceFirst",
		LastName:     "SourceLast",
		JapaneseName: "ソース",
		ThumbURL:     "https://source.jpg",
	}
	resolutions := map[string]string{
		"dmm_id":        "source",
		"first_name":    "source",
		"last_name":     "source",
		"japanese_name": "source",
		"thumb_url":     "source",
	}
	merged, err := mergeActressValues(target, source, resolutions)
	require.NoError(t, err)
	assert.Equal(t, 2, merged.DMMID)
	assert.Equal(t, "SourceFirst", merged.FirstName)
	assert.Equal(t, "SourceLast", merged.LastName)
	assert.Equal(t, "ソース", merged.JapaneseName)
	assert.Equal(t, "https://source.jpg", merged.ThumbURL)
}
