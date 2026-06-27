package database

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// 1. ActressAliasRepository — full CRUD lifecycle
// ============================================================================

func TestRepoCRUD_ActressAlias_CreateAndFind(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	alias := &models.ActressAlias{
		AliasName:     "CRUD-Alias-1",
		CanonicalName: "Canonical-1",
	}
	require.NoError(t, repo.Create(context.Background(), alias))
	assert.NotZero(t, alias.ID)

	found, err := repo.FindByAliasName(context.Background(), "CRUD-Alias-1")
	require.NoError(t, err)
	assert.Equal(t, "Canonical-1", found.CanonicalName)
	assert.Equal(t, alias.ID, found.ID)
}

func TestRepoCRUD_ActressAlias_FindByAliasName_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	_, err := repo.FindByAliasName(context.Background(), "nonexistent-alias")
	require.Error(t, err)
}

func TestRepoCRUD_ActressAlias_Upsert_CreatePath(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	alias := &models.ActressAlias{
		AliasName:     "Upsert-New",
		CanonicalName: "Canonical-New",
	}
	require.NoError(t, repo.Upsert(context.Background(), alias))
	assert.NotZero(t, alias.ID)

	found, err := repo.FindByAliasName(context.Background(), "Upsert-New")
	require.NoError(t, err)
	assert.Equal(t, "Canonical-New", found.CanonicalName)
}

func TestRepoCRUD_ActressAlias_Upsert_UpdatePath(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	alias := &models.ActressAlias{
		AliasName:     "Upsert-Update",
		CanonicalName: "Original",
	}
	require.NoError(t, repo.Create(context.Background(), alias))
	originalID := alias.ID

	updated := &models.ActressAlias{
		AliasName:     "Upsert-Update",
		CanonicalName: "Updated",
	}
	require.NoError(t, repo.Upsert(context.Background(), updated))
	assert.Equal(t, originalID, updated.ID, "upsert should preserve existing ID")

	found, err := repo.FindByAliasName(context.Background(), "Upsert-Update")
	require.NoError(t, err)
	assert.Equal(t, "Updated", found.CanonicalName)
}

func TestRepoCRUD_ActressAlias_FindByCanonicalName(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	for _, a := range []*models.ActressAlias{
		{AliasName: "Alpha1", CanonicalName: "Alpha"},
		{AliasName: "Alpha2", CanonicalName: "Alpha"},
		{AliasName: "Beta1", CanonicalName: "Beta"},
	} {
		require.NoError(t, repo.Create(context.Background(), a))
	}

	aliases, err := repo.FindByCanonicalName(context.Background(), "Alpha")
	require.NoError(t, err)
	assert.Len(t, aliases, 2)

	aliases, err = repo.FindByCanonicalName(context.Background(), "Beta")
	require.NoError(t, err)
	assert.Len(t, aliases, 1)

	// Empty result for nonexistent canonical name
	aliases, err = repo.FindByCanonicalName(context.Background(), "Gamma")
	require.NoError(t, err)
	assert.Len(t, aliases, 0)
}

func TestRepoCRUD_ActressAlias_List(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	for _, a := range []*models.ActressAlias{
		{AliasName: "ListA1", CanonicalName: "ListA"},
		{AliasName: "ListA2", CanonicalName: "ListA"},
	} {
		require.NoError(t, repo.Create(context.Background(), a))
	}

	aliases, err := repo.List(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(aliases), 2)
}

func TestRepoCRUD_ActressAlias_Delete(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	alias := &models.ActressAlias{
		AliasName:     "ToDelete",
		CanonicalName: "DeleteCanonical",
	}
	require.NoError(t, repo.Create(context.Background(), alias))

	require.NoError(t, repo.Delete(context.Background(), "ToDelete"))

	_, err := repo.FindByAliasName(context.Background(), "ToDelete")
	require.Error(t, err, "deleted alias should not be found")
}

func TestRepoCRUD_ActressAlias_GetAliasMap(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	for _, a := range []*models.ActressAlias{
		{AliasName: "MapA", CanonicalName: "CanonicalA"},
		{AliasName: "MapB", CanonicalName: "CanonicalB"},
	} {
		require.NoError(t, repo.Create(context.Background(), a))
	}

	m, err := repo.GetAliasMap(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "CanonicalA", m["MapA"])
	assert.Equal(t, "CanonicalB", m["MapB"])
}

// ============================================================================
// 2. GenreRepository — List, FindOrCreate
// ============================================================================

func TestRepoCRUD_Genre_FindOrCreate_New(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newGenreRepository(db)

	genre, err := repo.FindOrCreate(context.Background(), "CRUD-Genre-New")
	require.NoError(t, err)
	assert.NotZero(t, genre.ID)
	assert.Equal(t, "CRUD-Genre-New", genre.Name)
}

func TestRepoCRUD_Genre_FindOrCreate_Existing(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newGenreRepository(db)

	first, err := repo.FindOrCreate(context.Background(), "CRUD-Genre-Exist")
	require.NoError(t, err)

	second, err := repo.FindOrCreate(context.Background(), "CRUD-Genre-Exist")
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID, "should return the same genre on duplicate FindOrCreate")
}

func TestRepoCRUD_Genre_List(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newGenreRepository(db)

	for _, name := range []string{"GL-A", "GL-B", "GL-C"} {
		_, err := repo.FindOrCreate(context.Background(), name)
		require.NoError(t, err)
	}

	genres, err := repo.List(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(genres), 3)

	names := make(map[string]bool)
	for _, g := range genres {
		names[g.Name] = true
	}
	assert.True(t, names["GL-A"])
	assert.True(t, names["GL-B"])
	assert.True(t, names["GL-C"])
}

// ============================================================================
// 3. EventRepository — Create, List, FindByID, DeleteOlderThan, Count, CountFiltered, FindFiltered
// ============================================================================

func TestRepoCRUD_Event_CreateAndFindByID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	event := &models.Event{
		EventType: models.EventCategoryScraper,
		Severity:  models.SeverityError,
		Message:   "test event",
		Source:    "test-source",
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, repo.Create(context.Background(), event))
	assert.NotZero(t, event.ID)

	found, err := repo.FindByID(context.Background(), event.ID)
	require.NoError(t, err)
	assert.Equal(t, "test event", found.Message)
	assert.Equal(t, models.EventCategoryScraper, found.EventType)
}

func TestRepoCRUD_Event_FindByID_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	_, err := repo.FindByID(context.Background(), 99999)
	require.Error(t, err)
}

func TestRepoCRUD_Event_List(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	for i := 0; i < 5; i++ {
		require.NoError(t, repo.Create(context.Background(), &models.Event{
			EventType: models.EventCategorySystem,
			Severity:  models.SeverityInfo,
			Message:   "list-event",
			Source:    "list-source",
			CreatedAt: time.Now().UTC().Add(time.Duration(i) * time.Second),
		}))
	}

	events, err := repo.List(context.Background(), 3, 0)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(events), 3, "limit should be respected")

	all, err := repo.List(context.Background(), 0, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(all), 5)
}

func TestRepoCRUD_Event_Count(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Create(context.Background(), &models.Event{
			EventType: models.EventCategoryScraper,
			Severity:  models.SeverityWarn,
			Message:   "count-event",
			CreatedAt: time.Now().UTC(),
		}))
	}

	count, err := repo.Count(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, int64(3))
}

func TestRepoCRUD_Event_CountFiltered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.Event{
		EventType: models.EventCategoryScraper,
		Severity:  models.SeverityError,
		Message:   "filtered-1",
		Source:    "src-a",
		CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Create(context.Background(), &models.Event{
		EventType: models.EventCategoryOrganize,
		Severity:  models.SeverityInfo,
		Message:   "filtered-2",
		Source:    "src-b",
		CreatedAt: time.Now().UTC(),
	}))

	count, err := repo.CountFiltered(context.Background(), EventFilter{EventType: models.EventCategoryScraper})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, int64(1))
}

func TestRepoCRUD_Event_FindFiltered(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.Event{
		EventType: models.EventCategoryScraper,
		Severity:  models.SeverityError,
		Message:   "ff-1",
		Source:    "ff-src",
		CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Create(context.Background(), &models.Event{
		EventType: models.EventCategorySystem,
		Severity:  models.SeverityDebug,
		Message:   "ff-2",
		Source:    "ff-other",
		CreatedAt: time.Now().UTC(),
	}))

	events, err := repo.FindFiltered(context.Background(), EventFilter{Source: "ff-src"}, 10, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(events), 1)
	assert.Equal(t, "ff-1", events[0].Message)
}

func TestRepoCRUD_Event_DeleteOlderThan(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	old := &models.Event{
		EventType: models.EventCategorySystem,
		Severity:  models.SeverityInfo,
		Message:   "old-event",
		Source:    "cleanup",
		CreatedAt: time.Now().UTC().Add(-48 * time.Hour),
	}
	require.NoError(t, repo.Create(context.Background(), old))

	recent := &models.Event{
		EventType: models.EventCategorySystem,
		Severity:  models.SeverityInfo,
		Message:   "recent-event",
		Source:    "cleanup",
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, repo.Create(context.Background(), recent))

	deleted, err := repo.DeleteOlderThan(context.Background(), time.Now().UTC().Add(-24*time.Hour))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, deleted, int64(1))

	_, err = repo.FindByID(context.Background(), old.ID)
	require.Error(t, err, "old event should be deleted")

	_, err = repo.FindByID(context.Background(), recent.ID)
	require.NoError(t, err, "recent event should still exist")
}

func TestRepoCRUD_Event_CountGroupBySource(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.Event{
		EventType: models.EventCategoryScraper, Severity: models.SeverityInfo,
		Message: "gs-1", Source: "group-src", CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Create(context.Background(), &models.Event{
		EventType: models.EventCategoryScraper, Severity: models.SeverityInfo,
		Message: "gs-2", Source: "group-src", CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Create(context.Background(), &models.Event{
		EventType: models.EventCategoryScraper, Severity: models.SeverityInfo,
		Message: "gs-3", Source: "other-src", CreatedAt: time.Now().UTC(),
	}))

	bySource, err := repo.CountGroupBySource(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, bySource["group-src"], int64(2))
	assert.GreaterOrEqual(t, bySource["other-src"], int64(1))
}

// ============================================================================
// 4. HistoryRepository — Create, FindByID, List, Delete, extra queries
// ============================================================================

func TestRepoCRUD_History_CreateAndFindByID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	history := &models.History{
		MovieID:      "HIST-001",
		Operation:    models.HistoryOpOrganize,
		OriginalPath: "/old/path",
		NewPath:      "/new/path",
		Status:       models.HistoryStatusSuccess,
		CreatedAt:    time.Now().UTC(),
	}
	require.NoError(t, repo.Create(context.Background(), history))
	assert.NotZero(t, history.ID)

	found, err := repo.FindByID(context.Background(), history.ID)
	require.NoError(t, err)
	assert.Equal(t, "HIST-001", found.MovieID)
	assert.Equal(t, models.HistoryOpOrganize, found.Operation)
}

func TestRepoCRUD_History_FindByID_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	_, err := repo.FindByID(context.Background(), 99999)
	require.Error(t, err)
}

func TestRepoCRUD_History_List(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	for i := 0; i < 5; i++ {
		require.NoError(t, repo.Create(context.Background(), &models.History{
			MovieID:   "HIST-LIST",
			Operation: models.HistoryOpScrape,
			Status:    models.HistoryStatusSuccess,
			CreatedAt: time.Now().UTC().Add(time.Duration(i) * time.Second),
		}))
	}

	all, err := repo.List(context.Background(), 0, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(all), 5)

	paged, err := repo.List(context.Background(), 2, 0)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(paged), 2)
}

func TestRepoCRUD_History_Delete(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	history := &models.History{
		MovieID:   "HIST-DEL",
		Operation: models.HistoryOpScrape,
		Status:    models.HistoryStatusSuccess,
		CreatedAt: time.Now().UTC(),
	}
	require.NoError(t, repo.Create(context.Background(), history))

	require.NoError(t, repo.Delete(context.Background(), history.ID))

	_, err := repo.FindByID(context.Background(), history.ID)
	require.Error(t, err, "deleted history should not be found")
}

func TestRepoCRUD_History_FindByMovieID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-MOVIE", Operation: models.HistoryOpScrape,
		Status: models.HistoryStatusSuccess, CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-MOVIE", Operation: models.HistoryOpOrganize,
		Status: models.HistoryStatusSuccess, CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "OTHER-MOVIE", Operation: models.HistoryOpScrape,
		Status: models.HistoryStatusSuccess, CreatedAt: time.Now().UTC(),
	}))

	results, err := repo.FindByMovieID(context.Background(), "HIST-MOVIE")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestRepoCRUD_History_FindByOperation(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-OP1", Operation: models.HistoryOpScrape,
		Status: models.HistoryStatusSuccess, CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-OP2", Operation: models.HistoryOpOrganize,
		Status: models.HistoryStatusSuccess, CreatedAt: time.Now().UTC(),
	}))

	results, err := repo.FindByOperation(context.Background(), models.HistoryOpScrape, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
	for _, r := range results {
		assert.Equal(t, models.HistoryOpScrape, r.Operation)
	}
}

func TestRepoCRUD_History_FindByStatus(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-ST1", Operation: models.HistoryOpScrape,
		Status: models.HistoryStatusFailed, CreatedAt: time.Now().UTC(),
	}))
	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-ST2", Operation: models.HistoryOpScrape,
		Status: models.HistoryStatusSuccess, CreatedAt: time.Now().UTC(),
	}))

	results, err := repo.FindByStatus(context.Background(), models.HistoryStatusFailed, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
	for _, r := range results {
		assert.Equal(t, models.HistoryStatusFailed, r.Status)
	}
}

func TestRepoCRUD_History_FindRecent(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-RECENT", Operation: models.HistoryOpScrape,
		Status: models.HistoryStatusSuccess, CreatedAt: time.Now().UTC(),
	}))

	results, err := repo.FindRecent(context.Background(), 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

func TestRepoCRUD_History_FindByDateRange(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	now := time.Now().UTC()
	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-DR", Operation: models.HistoryOpScrape,
		Status: models.HistoryStatusSuccess, CreatedAt: now,
	}))

	results, err := repo.FindByDateRange(context.Background(), now.Add(-1*time.Hour), now.Add(1*time.Hour))
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

func TestRepoCRUD_History_CountByStatus(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-CBS", Operation: models.HistoryOpScrape,
		Status: models.HistoryStatusSuccess, CreatedAt: time.Now().UTC(),
	}))

	count, err := repo.CountByStatus(context.Background(), models.HistoryStatusSuccess)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, int64(1))
}

func TestRepoCRUD_History_CountByOperation(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-CBO", Operation: models.HistoryOpOrganize,
		Status: models.HistoryStatusSuccess, CreatedAt: time.Now().UTC(),
	}))

	count, err := repo.CountByOperation(context.Background(), models.HistoryOpOrganize)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, int64(1))
}

func TestRepoCRUD_History_CountByMovieID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Create(context.Background(), &models.History{
			MovieID:   "HIST-CBM",
			Operation: models.HistoryOpScrape,
			Status:    models.HistoryStatusSuccess,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}))
	}
	// An unrelated movie must not be counted.
	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-OTHER", Operation: models.HistoryOpScrape,
		Status: models.HistoryStatusSuccess, CreatedAt: base,
	}))

	count, err := repo.CountByMovieID(context.Background(), "HIST-CBM")
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)

	other, err := repo.CountByMovieID(context.Background(), "HIST-OTHER")
	require.NoError(t, err)
	assert.Equal(t, int64(1), other)
}

func TestRepoCRUD_History_ListByMovieID_Pagination(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const total = 15
	for i := 0; i < total; i++ {
		require.NoError(t, repo.Create(context.Background(), &models.History{
			MovieID:   "PAG-MOVIE",
			Operation: models.HistoryOpScrape,
			Status:    models.HistoryStatusSuccess,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}))
	}

	count, err := repo.CountByMovieID(context.Background(), "PAG-MOVIE")
	require.NoError(t, err)
	assert.Equal(t, int64(total), count)

	// First page: the 10 newest records (created_at DESC).
	page1, err := repo.ListByMovieID(context.Background(), "PAG-MOVIE", 10, 0)
	require.NoError(t, err)
	assert.Len(t, page1, 10)
	assert.True(t, page1[0].CreatedAt.Equal(base.Add(14*time.Second)))

	// Second page: the remaining 5 records.
	page2, err := repo.ListByMovieID(context.Background(), "PAG-MOVIE", 10, 10)
	require.NoError(t, err)
	assert.Len(t, page2, 5)
	assert.True(t, page2[0].CreatedAt.Equal(base.Add(4*time.Second)))

	// Pages must not overlap and page1 must be strictly newer than page2.
	seen := make(map[uint]struct{}, len(page1)+len(page2))
	for _, h := range page1 {
		seen[h.ID] = struct{}{}
	}
	for _, h := range page2 {
		_, dup := seen[h.ID]
		assert.False(t, dup, "page2 record %d must not appear in page1", h.ID)
	}
	assert.True(t, page1[0].CreatedAt.After(page2[0].CreatedAt))
}

func TestRepoCRUD_History_ListByOperation_Pagination(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const total = 12
	for i := 0; i < total; i++ {
		require.NoError(t, repo.Create(context.Background(), &models.History{
			MovieID:   fmt.Sprintf("OP-%03d", i+1),
			Operation: models.HistoryOpScrape,
			Status:    models.HistoryStatusSuccess,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}))
	}
	// Records with a different operation must be excluded.
	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "OP-OTHER", Operation: models.HistoryOpOrganize,
		Status: models.HistoryStatusSuccess, CreatedAt: base,
	}))

	count, err := repo.CountByOperation(context.Background(), models.HistoryOpScrape)
	require.NoError(t, err)
	assert.Equal(t, int64(total), count)

	page1, err := repo.ListByOperation(context.Background(), models.HistoryOpScrape, 10, 0)
	require.NoError(t, err)
	assert.Len(t, page1, 10)
	for _, h := range page1 {
		assert.Equal(t, models.HistoryOpScrape, h.Operation)
	}

	page2, err := repo.ListByOperation(context.Background(), models.HistoryOpScrape, 10, 10)
	require.NoError(t, err)
	assert.Len(t, page2, 2)
	assert.True(t, page1[0].CreatedAt.After(page2[0].CreatedAt))
}

func TestRepoCRUD_History_ListByStatus_Pagination(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	const total = 12
	for i := 0; i < total; i++ {
		require.NoError(t, repo.Create(context.Background(), &models.History{
			MovieID:   fmt.Sprintf("ST-%03d", i+1),
			Operation: models.HistoryOpScrape,
			Status:    models.HistoryStatusFailed,
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}))
	}
	// Records with a different status must be excluded.
	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "ST-OTHER", Operation: models.HistoryOpScrape,
		Status: models.HistoryStatusSuccess, CreatedAt: base,
	}))

	count, err := repo.CountByStatus(context.Background(), models.HistoryStatusFailed)
	require.NoError(t, err)
	assert.Equal(t, int64(total), count)

	page1, err := repo.ListByStatus(context.Background(), models.HistoryStatusFailed, 10, 0)
	require.NoError(t, err)
	assert.Len(t, page1, 10)
	for _, h := range page1 {
		assert.Equal(t, models.HistoryStatusFailed, h.Status)
	}

	page2, err := repo.ListByStatus(context.Background(), models.HistoryStatusFailed, 10, 10)
	require.NoError(t, err)
	assert.Len(t, page2, 2)
	assert.True(t, page1[0].CreatedAt.After(page2[0].CreatedAt))
}

func TestRepoCRUD_History_DeleteByMovieID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-DBM", Operation: models.HistoryOpScrape,
		Status: models.HistoryStatusSuccess, CreatedAt: time.Now().UTC(),
	}))

	require.NoError(t, repo.DeleteByMovieID(context.Background(), "HIST-DBM"))

	results, err := repo.FindByMovieID(context.Background(), "HIST-DBM")
	require.NoError(t, err)
	assert.Len(t, results, 0)
}

func TestRepoCRUD_History_DeleteOlderThan(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-OLD", Operation: models.HistoryOpScrape,
		Status: models.HistoryStatusSuccess, CreatedAt: time.Now().UTC().Add(-48 * time.Hour),
	}))
	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-NEW", Operation: models.HistoryOpScrape,
		Status: models.HistoryStatusSuccess, CreatedAt: time.Now().UTC(),
	}))

	require.NoError(t, repo.DeleteOlderThan(context.Background(), time.Now().UTC().Add(-24*time.Hour)))

	results, err := repo.FindByMovieID(context.Background(), "HIST-OLD")
	require.NoError(t, err)
	assert.Len(t, results, 0, "old history should be deleted")

	results, err = repo.FindByMovieID(context.Background(), "HIST-NEW")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1, "recent history should remain")
}

func TestRepoCRUD_History_FindByBatchJobID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	batchID := "batch-job-123"
	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-BJ1", Operation: models.HistoryOpOrganize,
		Status: models.HistoryStatusSuccess, CreatedAt: time.Now().UTC(),
		BatchJobID: &batchID,
	}))
	require.NoError(t, repo.Create(context.Background(), &models.History{
		MovieID: "HIST-BJ2", Operation: models.HistoryOpOrganize,
		Status: models.HistoryStatusSuccess, CreatedAt: time.Now().UTC(),
		BatchJobID: &batchID,
	}))

	results, err := repo.FindByBatchJobID(context.Background(), batchID)
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

// ============================================================================
// 5. JobRepository — Create, FindByID, List, Update, Delete, Upsert
// ============================================================================

func TestRepoCRUD_Job_CreateAndFindByID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	job := &models.Job{
		ID:         "job-crud-1",
		Status:     models.JobStatusPending,
		TotalFiles: 10,
		StartedAt:  time.Now().UTC(),
	}
	require.NoError(t, repo.Create(context.Background(), job))

	found, err := repo.FindByID(context.Background(), "job-crud-1")
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusPending, found.Status)
	assert.Equal(t, 10, found.TotalFiles)
}

func TestRepoCRUD_Job_FindByID_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	_, err := repo.FindByID(context.Background(), "nonexistent-job")
	require.Error(t, err)
}

func TestRepoCRUD_Job_List(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Create(context.Background(), &models.Job{
			ID:        "job-list-" + string(rune('A'+i)),
			Status:    models.JobStatusCompleted,
			StartedAt: time.Now().UTC().Add(time.Duration(i) * time.Second),
		}))
	}

	jobs, err := repo.List(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(jobs), 3)
}

func TestRepoCRUD_Job_Update(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	job := &models.Job{
		ID:         "job-update-1",
		Status:     models.JobStatusRunning,
		TotalFiles: 5,
		Completed:  0,
		StartedAt:  time.Now().UTC(),
	}
	require.NoError(t, repo.Create(context.Background(), job))

	job.Status = models.JobStatusCompleted
	job.Completed = 5
	job.Progress = 100.0
	now := time.Now().UTC()
	job.CompletedAt = &now
	require.NoError(t, repo.Update(context.Background(), job))

	found, err := repo.FindByID(context.Background(), "job-update-1")
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCompleted, found.Status)
	assert.Equal(t, 5, found.Completed)
	assert.Equal(t, 100.0, found.Progress)
}

func TestRepoCRUD_Job_Upsert(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	job := &models.Job{
		ID:         "job-upsert-1",
		Status:     models.JobStatusPending,
		TotalFiles: 3,
		StartedAt:  time.Now().UTC(),
	}
	require.NoError(t, repo.Upsert(context.Background(), job))

	job.Status = models.JobStatusRunning
	require.NoError(t, repo.Upsert(context.Background(), job))

	found, err := repo.FindByID(context.Background(), "job-upsert-1")
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusRunning, found.Status)
}

func TestRepoCRUD_Job_Delete(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.Job{
		ID:        "job-del-1",
		Status:    models.JobStatusCompleted,
		StartedAt: time.Now().UTC(),
	}))

	require.NoError(t, repo.Delete(context.Background(), "job-del-1"))

	_, err := repo.FindByID(context.Background(), "job-del-1")
	require.Error(t, err, "deleted job should not be found")
}

func TestRepoCRUD_Job_DeleteOrganizedOlderThan(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	past := time.Now().UTC().Add(-48 * time.Hour)
	require.NoError(t, repo.Create(context.Background(), &models.Job{
		ID:          "job-org-old",
		Status:      models.JobStatusOrganized,
		OrganizedAt: &past,
		StartedAt:   past,
	}))

	recent := time.Now().UTC()
	require.NoError(t, repo.Create(context.Background(), &models.Job{
		ID:          "job-org-new",
		Status:      models.JobStatusOrganized,
		OrganizedAt: &recent,
		StartedAt:   recent,
	}))

	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	require.NoError(t, repo.DeleteOrganizedOlderThan(context.Background(), cutoff))

	_, err := repo.FindByID(context.Background(), "job-org-old")
	require.Error(t, err, "old organized job should be deleted")

	_, err = repo.FindByID(context.Background(), "job-org-new")
	require.NoError(t, err, "recent organized job should remain")
}

// ============================================================================
// 6. BaseRepository — nil entity, string ID paths, ListAll, FindByID, Delete
// ============================================================================

func TestRepoCRUD_BaseRepository_NilEntity(t *testing.T) {
	db := newDatabaseTestDB(t)
	br := NewBaseRepository[models.Genre, uint](
		db, "genre",
		func(g models.Genre) string { return g.Name },
		WithNewEntity[models.Genre, uint](func() models.Genre { return models.Genre{} }),
	)

	err := br.Create(context.Background(), nil)
	require.Error(t, err, "creating nil entity should return error")
	assert.Contains(t, err.Error(), "must not be nil")
}

func TestRepoCRUD_BaseRepository_ListAll(t *testing.T) {
	db := newDatabaseTestDB(t)
	br := NewBaseRepository[models.Genre, uint](
		db, "genre",
		func(g models.Genre) string { return g.Name },
		WithNewEntity[models.Genre, uint](func() models.Genre { return models.Genre{} }),
	)

	require.NoError(t, br.Create(context.Background(), &models.Genre{Name: "BaseList-A"}))
	require.NoError(t, br.Create(context.Background(), &models.Genre{Name: "BaseList-B"}))

	all, err := br.ListAll(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(all), 2)
}

func TestRepoCRUD_BaseRepository_FindByID(t *testing.T) {
	db := newDatabaseTestDB(t)
	br := NewBaseRepository[models.Genre, uint](
		db, "genre",
		func(g models.Genre) string { return g.Name },
		WithNewEntity[models.Genre, uint](func() models.Genre { return models.Genre{} }),
	)

	genre := &models.Genre{Name: "FindByID-Genre"}
	require.NoError(t, br.Create(context.Background(), genre))

	found, err := br.FindByID(context.Background(), genre.ID)
	require.NoError(t, err)
	assert.Equal(t, "FindByID-Genre", found.Name)
}

func TestRepoCRUD_BaseRepository_FindByID_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	br := NewBaseRepository[models.Genre, uint](
		db, "genre",
		func(g models.Genre) string { return g.Name },
		WithNewEntity[models.Genre, uint](func() models.Genre { return models.Genre{} }),
	)

	_, err := br.FindByID(context.Background(), 99999)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRepoCRUD_BaseRepository_Delete(t *testing.T) {
	db := newDatabaseTestDB(t)
	br := NewBaseRepository[models.Genre, uint](
		db, "genre",
		func(g models.Genre) string { return g.Name },
		WithNewEntity[models.Genre, uint](func() models.Genre { return models.Genre{} }),
	)

	genre := &models.Genre{Name: "Delete-Genre"}
	require.NoError(t, br.Create(context.Background(), genre))

	require.NoError(t, br.Delete(context.Background(), genre.ID))

	_, err := br.FindByID(context.Background(), genre.ID)
	require.Error(t, err)
}

func TestRepoCRUD_BaseRepository_Count(t *testing.T) {
	db := newDatabaseTestDB(t)
	br := NewBaseRepository[models.Genre, uint](
		db, "genre",
		func(g models.Genre) string { return g.Name },
		WithNewEntity[models.Genre, uint](func() models.Genre { return models.Genre{} }),
	)

	require.NoError(t, br.Create(context.Background(), &models.Genre{Name: "Count-A"}))
	require.NoError(t, br.Create(context.Background(), &models.Genre{Name: "Count-B"}))

	count, err := br.Count(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, int64(2))
}

func TestRepoCRUD_BaseRepository_StringID_FindByID(t *testing.T) {
	db := newDatabaseTestDB(t)
	br := NewBaseRepository[models.Job, string](
		db, "job",
		func(j models.Job) string { return j.ID },
		WithNewEntity[models.Job, string](func() models.Job { return models.Job{} }),
	)

	job := &models.Job{ID: "string-id-job", Status: models.JobStatusPending, StartedAt: time.Now().UTC()}
	require.NoError(t, br.Create(context.Background(), job))

	found, err := br.FindByID(context.Background(), "string-id-job")
	require.NoError(t, err)
	assert.Equal(t, "string-id-job", found.ID)
}

func TestRepoCRUD_BaseRepository_StringID_Delete(t *testing.T) {
	db := newDatabaseTestDB(t)
	br := NewBaseRepository[models.Job, string](
		db, "job",
		func(j models.Job) string { return j.ID },
		WithNewEntity[models.Job, string](func() models.Job { return models.Job{} }),
	)

	job := &models.Job{ID: "string-del-job", Status: models.JobStatusPending, StartedAt: time.Now().UTC()}
	require.NoError(t, br.Create(context.Background(), job))

	require.NoError(t, br.Delete(context.Background(), "string-del-job"))

	_, err := br.FindByID(context.Background(), "string-del-job")
	require.Error(t, err)
}

// ============================================================================
// 7. DB.Close — double-close
// ============================================================================

func TestRepoCRUD_DB_Close_DoubleClose(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)

	require.NoError(t, db.Close())
	// Second close should not panic
	_ = db.Close()
}

// ============================================================================
// 8. MovieRepository — saveMovieWithAssociations, ensureGenresExistTx
// ============================================================================

func TestRepoCRUD_Movie_SaveMovieWithAssociations(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	movie := &models.Movie{
		ContentID: "SAVE-ASSOC-001",
		ID:        "SAVE-ASSOC-001",
		Title:     "Save Assoc Test",
		Genres: []models.Genre{
			{Name: "SAGenre1"},
			{Name: "SAGenre2"},
		},
		Actresses: []models.Actress{
			{DMMID: 99001, JapaneseName: "SA女優"},
		},
	}
	require.NoError(t, repo.upserter.saveMovieWithAssociations(db.DB, movie))

	found, err := repo.FindByContentID(context.Background(), "SAVE-ASSOC-001")
	require.NoError(t, err)
	assert.Equal(t, "Save Assoc Test", found.Title)
	require.Len(t, found.Genres, 2)
	require.Len(t, found.Actresses, 1)
	assert.Equal(t, "SA女優", found.Actresses[0].JapaneseName)
}

func TestRepoCRUD_Movie_EnsureGenresExistTx_EmptySlice(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	err := repo.upserter.ensureGenresExistTx(db.DB, []models.Genre{})
	require.NoError(t, err)
}

func TestRepoCRUD_Movie_EnsureGenresExistTx_MixedExistingAndNew(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)
	genreRepo := newGenreRepository(db)

	existing, err := genreRepo.FindOrCreate(context.Background(), "EG-Existing")
	require.NoError(t, err)

	genres := []models.Genre{
		{Name: "EG-Existing"},
		{Name: "EG-New"},
	}
	require.NoError(t, repo.upserter.ensureGenresExistTx(db.DB, genres))

	assert.Equal(t, existing.ID, genres[0].ID, "existing genre should reuse ID")
	assert.NotZero(t, genres[1].ID, "new genre should get an ID")

	// No duplicates
	all, err := genreRepo.List(context.Background())
	require.NoError(t, err)
	names := make(map[string]int)
	for _, g := range all {
		names[g.Name]++
	}
	for name, count := range names {
		assert.Equal(t, 1, count, "genre %q should appear exactly once", name)
	}
}

// ============================================================================
// 9. BatchFileOperationRepository — Create, FindByID, FindByBatchJobID, Update
// ============================================================================

func TestRepoCRUD_BFO_CreateAndFindByID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	op := &models.BatchFileOperation{
		BatchJobID:    "bfo-crud-job",
		MovieID:       "BFO-001",
		OriginalPath:  "/old/path",
		NewPath:       "/new/path",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))
	assert.NotZero(t, op.ID)

	found, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	assert.Equal(t, "bfo-crud-job", found.BatchJobID)
	assert.Equal(t, models.OperationTypeMove, found.OperationType)
}

func TestRepoCRUD_BFO_FindByID_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	_, err := repo.FindByID(context.Background(), 99999)
	require.Error(t, err)
}

func TestRepoCRUD_BFO_FindByBatchJobID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Create(context.Background(), &models.BatchFileOperation{
			BatchJobID:    "bfo-list-job",
			MovieID:       "BFO-LIST",
			OriginalPath:  "/old",
			NewPath:       "/new",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		}))
	}

	ops, err := repo.FindByBatchJobID(context.Background(), "bfo-list-job")
	require.NoError(t, err)
	assert.Len(t, ops, 3)
}

func TestRepoCRUD_BFO_FindByBatchJobIDAndRevertStatus(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.BatchFileOperation{
		BatchJobID:    "bfo-status-job",
		OriginalPath:  "/old1",
		NewPath:       "/new1",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}))
	require.NoError(t, repo.Create(context.Background(), &models.BatchFileOperation{
		BatchJobID:    "bfo-status-job",
		OriginalPath:  "/old2",
		NewPath:       "/new2",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusReverted,
	}))

	applied, err := repo.FindByBatchJobIDAndRevertStatus(context.Background(), "bfo-status-job", models.RevertStatusApplied)
	require.NoError(t, err)
	assert.Len(t, applied, 1)
	assert.Equal(t, models.RevertStatusApplied, applied[0].RevertStatus)

	reverted, err := repo.FindByBatchJobIDAndRevertStatus(context.Background(), "bfo-status-job", models.RevertStatusReverted)
	require.NoError(t, err)
	assert.Len(t, reverted, 1)
	assert.Equal(t, models.RevertStatusReverted, reverted[0].RevertStatus)
}

func TestRepoCRUD_BFO_Update(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	op := &models.BatchFileOperation{
		BatchJobID:    "bfo-update-job",
		OriginalPath:  "/old",
		NewPath:       "/new",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))

	op.RevertStatus = models.RevertStatusReverted
	now := time.Now().UTC()
	op.RevertedAt = &now
	require.NoError(t, repo.Update(context.Background(), op))

	found, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	assert.Equal(t, models.RevertStatusReverted, found.RevertStatus)
	assert.NotNil(t, found.RevertedAt)
}

func TestRepoCRUD_BFO_UpdateRevertStatus(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	op := &models.BatchFileOperation{
		BatchJobID:    "bfo-revert-job",
		OriginalPath:  "/old",
		NewPath:       "/new",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.Background(), op))

	require.NoError(t, repo.UpdateRevertStatus(context.Background(), op.ID, models.RevertStatusReverted))

	found, err := repo.FindByID(context.Background(), op.ID)
	require.NoError(t, err)
	assert.Equal(t, models.RevertStatusReverted, found.RevertStatus)
	assert.NotNil(t, found.RevertedAt, "RevertStatusReverted should set reverted_at")
}

func TestRepoCRUD_BFO_CountByBatchJobID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Create(context.Background(), &models.BatchFileOperation{
			BatchJobID:    "bfo-count-job",
			OriginalPath:  "/old",
			NewPath:       "/new",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		}))
	}

	count, err := repo.CountByBatchJobID(context.Background(), "bfo-count-job")
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

func TestRepoCRUD_BFO_CountByBatchJobIDs(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.BatchFileOperation{
		BatchJobID: "bfo-cbj-a", OriginalPath: "/o", NewPath: "/n",
		OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied,
	}))
	require.NoError(t, repo.Create(context.Background(), &models.BatchFileOperation{
		BatchJobID: "bfo-cbj-b", OriginalPath: "/o", NewPath: "/n",
		OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied,
	}))

	m, err := repo.CountByBatchJobIDs(context.Background(), []string{"bfo-cbj-a", "bfo-cbj-b"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), m["bfo-cbj-a"])
	assert.Equal(t, int64(1), m["bfo-cbj-b"])

	// Empty input
	m, err = repo.CountByBatchJobIDs(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, m, 0)
}

func TestRepoCRUD_BFO_CountRevertedByBatchJobIDs(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.BatchFileOperation{
		BatchJobID: "bfo-crj-a", OriginalPath: "/o", NewPath: "/n",
		OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusReverted,
	}))
	require.NoError(t, repo.Create(context.Background(), &models.BatchFileOperation{
		BatchJobID: "bfo-crj-a", OriginalPath: "/o2", NewPath: "/n2",
		OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied,
	}))

	m, err := repo.CountRevertedByBatchJobIDs(context.Background(), []string{"bfo-crj-a"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), m["bfo-crj-a"])

	// Empty input
	m, err = repo.CountRevertedByBatchJobIDs(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, m, 0)
}

// ============================================================================
// 10. WordReplacementRepository — Upsert, List, FindByOriginal, Delete, SeedDefaultWordReplacements
// ============================================================================

func TestRepoCRUD_WordReplacement_Upsert_CreatePath(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewWordReplacementRepository(db)

	wr := &models.WordReplacement{
		Original:    "WR-Create",
		Replacement: "Created",
	}
	require.NoError(t, repo.Upsert(context.Background(), wr))
	assert.NotZero(t, wr.ID)
}

func TestRepoCRUD_WordReplacement_Upsert_UpdatePath(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewWordReplacementRepository(db)

	wr := &models.WordReplacement{
		Original:    "WR-Update",
		Replacement: "Original",
	}
	require.NoError(t, repo.Create(context.Background(), wr))
	originalID := wr.ID

	updated := &models.WordReplacement{
		Original:    "WR-Update",
		Replacement: "Updated",
	}
	require.NoError(t, repo.Upsert(context.Background(), updated))
	assert.Equal(t, originalID, updated.ID)

	found, err := repo.FindByOriginal(context.Background(), "WR-Update")
	require.NoError(t, err)
	assert.Equal(t, "Updated", found.Replacement)
}

func TestRepoCRUD_WordReplacement_FindByOriginal_NotFound(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewWordReplacementRepository(db)

	_, err := repo.FindByOriginal(context.Background(), "NonExistent")
	require.Error(t, err)
}

func TestRepoCRUD_WordReplacement_List(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewWordReplacementRepository(db)

	for _, wr := range []*models.WordReplacement{
		{Original: "WR-List1", Replacement: "R1"},
		{Original: "WR-List2", Replacement: "R2"},
	} {
		require.NoError(t, repo.Create(context.Background(), wr))
	}

	list, err := repo.List(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list), 2)
}

func TestRepoCRUD_WordReplacement_Delete(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewWordReplacementRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.WordReplacement{
		Original: "WR-Del", Replacement: "R",
	}))

	require.NoError(t, repo.Delete(context.Background(), "WR-Del"))

	_, err := repo.FindByOriginal(context.Background(), "WR-Del")
	require.Error(t, err)
}

func TestRepoCRUD_WordReplacement_DeleteByID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewWordReplacementRepository(db)

	wr := &models.WordReplacement{Original: "WR-DelID", Replacement: "R"}
	require.NoError(t, repo.Create(context.Background(), wr))

	require.NoError(t, repo.DeleteByID(context.Background(), wr.ID))

	_, err := repo.FindByOriginal(context.Background(), "WR-DelID")
	require.Error(t, err)
}

func TestRepoCRUD_WordReplacement_FindByID(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewWordReplacementRepository(db)

	wr := &models.WordReplacement{Original: "WR-FindID", Replacement: "R"}
	require.NoError(t, repo.Create(context.Background(), wr))

	found, err := repo.FindByID(context.Background(), wr.ID)
	require.NoError(t, err)
	assert.Equal(t, "WR-FindID", found.Original)
}

func TestRepoCRUD_WordReplacement_GetReplacementMap(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewWordReplacementRepository(db)

	require.NoError(t, repo.Create(context.Background(), &models.WordReplacement{
		Original: "WR-Map1", Replacement: "MapVal1",
	}))
	require.NoError(t, repo.Create(context.Background(), &models.WordReplacement{
		Original: "WR-Map2", Replacement: "MapVal2",
	}))

	m, err := repo.GetReplacementMap(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "MapVal1", m["WR-Map1"])
	assert.Equal(t, "MapVal2", m["WR-Map2"])
}

func TestRepoCRUD_WordReplacement_SeedDefaultWordReplacements(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewWordReplacementRepository(db)

	SeedDefaultWordReplacements(context.Background(), repo)

	replacements, err := repo.List(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(replacements), 10)

	// Idempotent
	SeedDefaultWordReplacements(context.Background(), repo)
	replacements2, err := repo.List(context.Background())
	require.NoError(t, err)
	assert.Equal(t, len(replacements), len(replacements2), "seeding twice should not create duplicates")
}

// ============================================================================
// 11. ActressTranslationRepository — UpsertTx existing record update path
// ============================================================================

func TestRepoCRUD_ActressTranslation_UpsertTx_NewRecord(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newActressTranslationRepository(db)

	actress := &models.Actress{DMMID: 88001, JapaneseName: "AT新規"}
	require.NoError(t, db.Create(actress).Error)

	tx := db.Begin()
	translation := &models.ActressTranslation{
		ActressID:    actress.ID,
		Language:     "en",
		FirstName:    "New",
		LastName:     "Actress",
		JapaneseName: "AT新規",
	}
	require.NoError(t, repo.UpsertTx(tx, translation))
	require.NoError(t, tx.Commit().Error)
	assert.NotZero(t, translation.ID)
}

func TestRepoCRUD_ActressTranslation_UpsertTx_ExistingRecordUpdate(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newActressTranslationRepository(db)

	actress := &models.Actress{DMMID: 88002, JapaneseName: "AT更新"}
	require.NoError(t, db.Create(actress).Error)

	// Create initial
	tx := db.Begin()
	initial := &models.ActressTranslation{
		ActressID:    actress.ID,
		Language:     "en",
		FirstName:    "Original",
		LastName:     "Name",
		JapaneseName: "AT更新",
	}
	require.NoError(t, repo.UpsertTx(tx, initial))
	require.NoError(t, tx.Commit().Error)
	firstID := initial.ID

	// Update via UpsertTx (existing record path)
	tx = db.Begin()
	updated := &models.ActressTranslation{
		ActressID:    actress.ID,
		Language:     "en",
		FirstName:    "Updated",
		LastName:     "Name",
		JapaneseName: "AT更新",
	}
	require.NoError(t, repo.UpsertTx(tx, updated))
	require.NoError(t, tx.Commit().Error)
	assert.Equal(t, firstID, updated.ID, "ID should be preserved from existing record")

	found, err := repo.FindByActressAndLanguage(context.Background(), actress.ID, "en")
	require.NoError(t, err)
	assert.Equal(t, "Updated", found.FirstName)
}

func TestRepoCRUD_ActressTranslation_FindAllByActress(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newActressTranslationRepository(db)

	actress := &models.Actress{DMMID: 88003, JapaneseName: "AT多言語"}
	require.NoError(t, db.Create(actress).Error)

	tx := db.Begin()
	require.NoError(t, repo.UpsertTx(tx, &models.ActressTranslation{
		ActressID: actress.ID, Language: "en", FirstName: "English",
	}))
	require.NoError(t, repo.UpsertTx(tx, &models.ActressTranslation{
		ActressID: actress.ID, Language: "ja", FirstName: "日本語",
	}))
	require.NoError(t, tx.Commit().Error)

	translations, err := repo.FindAllByActress(context.Background(), actress.ID)
	require.NoError(t, err)
	assert.Len(t, translations, 2)
}

func TestRepoCRUD_ActressTranslation_FindByActressIDsAndLanguage(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newActressTranslationRepository(db)

	a1 := &models.Actress{DMMID: 88004, JapaneseName: "ATBatch1"}
	a2 := &models.Actress{DMMID: 88005, JapaneseName: "ATBatch2"}
	require.NoError(t, db.Create(a1).Error)
	require.NoError(t, db.Create(a2).Error)

	tx := db.Begin()
	require.NoError(t, repo.UpsertTx(tx, &models.ActressTranslation{
		ActressID: a1.ID, Language: "en", FirstName: "One",
	}))
	require.NoError(t, repo.UpsertTx(tx, &models.ActressTranslation{
		ActressID: a2.ID, Language: "en", FirstName: "Two",
	}))
	require.NoError(t, repo.UpsertTx(tx, &models.ActressTranslation{
		ActressID: a2.ID, Language: "ja", FirstName: "二",
	}))
	require.NoError(t, tx.Commit().Error)

	// Batch query
	result, err := repo.FindByActressIDsAndLanguage(context.Background(), []uint{a1.ID, a2.ID}, "en")
	require.NoError(t, err)
	assert.Len(t, result, 2) // one for each actress

	// Empty IDs
	result, err = repo.FindByActressIDsAndLanguage(context.Background(), nil, "en")
	require.NoError(t, err)
	assert.Len(t, result, 0)
}

func TestRepoCRUD_ActressTranslation_Delete(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newActressTranslationRepository(db)

	actress := &models.Actress{DMMID: 88006, JapaneseName: "AT削除"}
	require.NoError(t, db.Create(actress).Error)

	tx := db.Begin()
	require.NoError(t, repo.UpsertTx(tx, &models.ActressTranslation{
		ActressID: actress.ID, Language: "en", FirstName: "ToDelete",
	}))
	require.NoError(t, tx.Commit().Error)

	require.NoError(t, repo.Delete(context.Background(), actress.ID, "en"))

	_, err := repo.FindByActressAndLanguage(context.Background(), actress.ID, "en")
	require.Error(t, err)
}

// ============================================================================
// 12. GenreTranslationRepository — UpsertTx existing record update path
// ============================================================================

func TestRepoCRUD_GenreTranslation_UpsertTx_NewRecord(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newGenreTranslationRepository(db)

	genre := &models.Genre{Name: "GT-New"}
	require.NoError(t, db.Create(genre).Error)

	tx := db.Begin()
	translation := &models.GenreTranslation{
		GenreID:    genre.ID,
		Language:   "en",
		Name:       "New Name",
		SourceName: "src",
	}
	require.NoError(t, repo.UpsertTx(tx, translation))
	require.NoError(t, tx.Commit().Error)
	assert.NotZero(t, translation.ID)
}

func TestRepoCRUD_GenreTranslation_UpsertTx_ExistingRecordUpdate(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newGenreTranslationRepository(db)

	genre := &models.Genre{Name: "GT-Update"}
	require.NoError(t, db.Create(genre).Error)

	// Create initial
	tx := db.Begin()
	initial := &models.GenreTranslation{
		GenreID:    genre.ID,
		Language:   "en",
		Name:       "Original Name",
		SourceName: "src1",
	}
	require.NoError(t, repo.UpsertTx(tx, initial))
	require.NoError(t, tx.Commit().Error)
	firstID := initial.ID

	// Update via UpsertTx (existing record path)
	tx = db.Begin()
	updated := &models.GenreTranslation{
		GenreID:    genre.ID,
		Language:   "en",
		Name:       "Updated Name",
		SourceName: "src2",
	}
	require.NoError(t, repo.UpsertTx(tx, updated))
	require.NoError(t, tx.Commit().Error)
	assert.Equal(t, firstID, updated.ID, "ID should be preserved from existing record")

	found, err := repo.FindByGenreAndLanguage(context.Background(), genre.ID, "en")
	require.NoError(t, err)
	assert.Equal(t, "Updated Name", found.Name)
	assert.Equal(t, "src2", found.SourceName)
}

func TestRepoCRUD_GenreTranslation_FindAllByGenre(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newGenreTranslationRepository(db)

	genre := &models.Genre{Name: "GT-Multi"}
	require.NoError(t, db.Create(genre).Error)

	tx := db.Begin()
	require.NoError(t, repo.UpsertTx(tx, &models.GenreTranslation{
		GenreID: genre.ID, Language: "en", Name: "English", SourceName: "src",
	}))
	require.NoError(t, repo.UpsertTx(tx, &models.GenreTranslation{
		GenreID: genre.ID, Language: "ja", Name: "日本語", SourceName: "src",
	}))
	require.NoError(t, tx.Commit().Error)

	translations, err := repo.FindAllByGenre(context.Background(), genre.ID)
	require.NoError(t, err)
	assert.Len(t, translations, 2)
}

func TestRepoCRUD_GenreTranslation_FindByGenreIDsAndLanguage(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newGenreTranslationRepository(db)

	g1 := &models.Genre{Name: "GT-Batch1"}
	g2 := &models.Genre{Name: "GT-Batch2"}
	require.NoError(t, db.Create(g1).Error)
	require.NoError(t, db.Create(g2).Error)

	tx := db.Begin()
	require.NoError(t, repo.UpsertTx(tx, &models.GenreTranslation{
		GenreID: g1.ID, Language: "en", Name: "One", SourceName: "s",
	}))
	require.NoError(t, repo.UpsertTx(tx, &models.GenreTranslation{
		GenreID: g2.ID, Language: "en", Name: "Two", SourceName: "s",
	}))
	require.NoError(t, tx.Commit().Error)

	result, err := repo.FindByGenreIDsAndLanguage(context.Background(), []uint{g1.ID, g2.ID}, "en")
	require.NoError(t, err)
	assert.Len(t, result, 2)

	// Empty IDs
	result, err = repo.FindByGenreIDsAndLanguage(context.Background(), nil, "en")
	require.NoError(t, err)
	assert.Len(t, result, 0)
}

func TestRepoCRUD_GenreTranslation_Delete(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newGenreTranslationRepository(db)

	genre := &models.Genre{Name: "GT-Del"}
	require.NoError(t, db.Create(genre).Error)

	tx := db.Begin()
	require.NoError(t, repo.UpsertTx(tx, &models.GenreTranslation{
		GenreID: genre.ID, Language: "en", Name: "ToDelete", SourceName: "s",
	}))
	require.NoError(t, tx.Commit().Error)

	require.NoError(t, repo.Delete(context.Background(), genre.ID, "en"))

	_, err := repo.FindByGenreAndLanguage(context.Background(), genre.ID, "en")
	require.Error(t, err)
}
