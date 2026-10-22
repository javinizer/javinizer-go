package database

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// The catalog is deliberately independent of scraped, unverified candidates.
// Seed directly so NULL/negative/whitespace legacy values can also be characterized.
func TestActressCatalogFilterSemantics(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	rows := []models.Actress{
		{ID: 1, DMMID: 101, FirstName: "Complete", JapaneseName: "完全", ThumbURL: "https://example.com/image"},
		{ID: 2, DMMID: 0, FirstName: "No DMM", JapaneseName: "名前", ThumbURL: "https://example.com/image"},
		{ID: 3, DMMID: 103, FirstName: "No thumb", JapaneseName: "名前", ThumbURL: "  "},
		{ID: 4, DMMID: 104, FirstName: "No Japanese", JapaneseName: "  ", ThumbURL: "https://example.com/image"},
		{ID: 5, DMMID: 105, FirstName: " ", LastName: " ", JapaneseName: "名前", ThumbURL: "https://example.com/image"},
		{ID: 6, DMMID: 106, JapaneseName: " ", ThumbURL: "https://pics.dmm.co.jp/mono/noimage/now_printing.jpg"},
		{ID: 7, DMMID: -1, FirstName: "Negative", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/name"},
		{ID: 8, DMMID: 108, LastName: "Complete", JapaneseName: "名前", ThumbURL: "https://pics.dmm.co.jp/mono/actjpgs/name.jpg"},
		{ID: 9, DMMID: 109, FirstName: "Custom", JapaneseName: "名前", ThumbURL: "https://example.com/mono/actjpgs/name"},
		{ID: 10, DMMID: 110, FirstName: "AWS", JapaneseName: "名前", ThumbURL: "https://awsimgsrc.dmm.com/pics_dig/mono/actjpgs/name?width=125"},
	}
	for i := range rows {
		rows[i].Verified = true
		rows[i].Origin = ActressOriginUser
	}
	require.NoError(t, db.Create(&rows).Error)
	// Each row has an otherwise identical unverified twin: no query may expose it.
	for _, a := range rows {
		a.ID += 100
		if a.DMMID > 0 {
			a.DMMID += 1000
		}
		a.Verified = false
		a.Origin = "scrape"
		require.NoError(t, db.Create(&a).Error)
	}
	expected := map[string][]uint{
		"": {1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, "missing_dmm": {2, 7}, "has_dmm": {1, 3, 4, 5, 6, 8, 9, 10},
		"missing_thumbnail": {3, 6, 7, 10}, "missing_japanese_name": {4, 6, 7}, "japanese_name_only": {5}, "missing_metadata": {3, 4, 5, 6, 10},
	}
	ids := func(as []models.Actress) []uint {
		result := make([]uint, 0, len(as))
		for _, a := range as {
			result = append(result, a.ID)
		}
		return result
	}
	for filter, want := range expected {
		t.Run(filter, func(t *testing.T) {
			count, err := repo.CountFiltered(t.Context(), filter)
			require.NoError(t, err)
			require.EqualValues(t, len(want), count)
			got, err := repo.ListFiltered(t.Context(), filter, 100, 0, "id", "asc")
			require.NoError(t, err)
			require.Equal(t, want, ids(got))
			for _, query := range []string{"", "10", "Complete", "名前", "No", "not found"} {
				matches := []uint{}
				for _, a := range got {
					for _, field := range []string{a.FirstName, a.LastName, a.JapaneseName, strconv.Itoa(a.DMMID)} {
						if strings.Contains(strings.ToLower(field), strings.ToLower(query)) {
							matches = append(matches, a.ID)
							break
						}
					}
				}
				search, err := repo.SearchFiltered(t.Context(), query, filter, 100, 0, "id", "asc")
				require.NoError(t, err)
				require.Equal(t, matches, ids(search))
				total, err := repo.CountSearchFiltered(t.Context(), query, filter)
				require.NoError(t, err)
				require.EqualValues(t, len(search), total)
				for _, a := range search {
					require.True(t, a.Verified)
					require.Contains(t, want, a.ID)
				}
				for offset := 0; offset <= len(search); offset++ {
					page, err := repo.SearchFiltered(t.Context(), query, filter, 1, offset, "id", "asc")
					require.NoError(t, err)
					if offset < len(search) {
						require.Equal(t, []uint{search[offset].ID}, ids(page))
					} else {
						require.Empty(t, page)
					}
				}
			}
			descending, err := repo.ListFiltered(t.Context(), filter, 1, 0, "id", "desc")
			require.NoError(t, err)
			require.Equal(t, want[len(want)-1], descending[0].ID)
		})
	}
	// DMM matching is substring-based and shared by all existing search entry points.
	for _, search := range []func() ([]models.Actress, error){
		func() ([]models.Actress, error) { return repo.Search(t.Context(), "101") },
		func() ([]models.Actress, error) { return repo.SearchPaged(t.Context(), "101", 10, 0) },
		func() ([]models.Actress, error) {
			return repo.SearchPagedSorted(t.Context(), "101", 10, 0, "id", "asc")
		},
	} {
		got, err := search()
		require.NoError(t, err)
		require.Equal(t, []uint{1}, ids(got))
	}
	count, err := repo.CountSearch(t.Context(), "101")
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	// TRIM/COALESCE semantics for migrated NULL values and trimmed filter tokens.
	require.NoError(t, db.Exec("UPDATE actresses SET dmm_id=NULL, first_name=NULL, last_name=NULL, japanese_name=NULL, thumb_url=NULL WHERE id=2").Error)
	for _, filter := range []string{" missing_dmm ", "missing_thumbnail", "missing_japanese_name"} {
		got, err := repo.ListFiltered(t.Context(), filter, 100, 0, "id", "asc")
		require.NoError(t, err)
		require.Contains(t, ids(got), uint(2))
	}
	got, err := repo.ListFiltered(t.Context(), "missing_metadata", 100, 0, "id", "asc")
	require.NoError(t, err)
	require.NotContains(t, ids(got), uint(2))
}

func TestActressCatalogFilterValidationAndErrors(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	listCalls := []func(context.Context, string, string, string) error{
		func(ctx context.Context, f, s, o string) error {
			_, err := repo.ListFiltered(ctx, f, 10, 0, s, o)
			return err
		},
		func(ctx context.Context, f, s, o string) error {
			_, err := repo.SearchFiltered(ctx, "test", f, 10, 0, s, o)
			return err
		},
	}
	for _, call := range listCalls {
		require.ErrorIs(t, call(t.Context(), "bad", "id", "asc"), ErrInvalidLookup)
		require.Error(t, call(t.Context(), "has_dmm", "invalid", "asc"))
		require.NoError(t, call(t.Context(), "has_dmm", "id", "invalid"))
		require.NoError(t, call(t.Context(), " ", "id", "asc"))
	}
	countCalls := []func(context.Context, string) error{
		func(ctx context.Context, f string) error { _, err := repo.CountFiltered(ctx, f); return err },
		func(ctx context.Context, f string) error {
			_, err := repo.CountSearchFiltered(ctx, "test", f)
			return err
		},
	}
	for _, call := range countCalls {
		require.ErrorIs(t, call(t.Context(), "bad"), ErrInvalidLookup)
		require.NoError(t, call(t.Context(), " "))
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, call := range listCalls {
		require.ErrorIs(t, call(ctx, "has_dmm", "id", "asc"), context.Canceled)
	}
	for _, call := range countCalls {
		require.ErrorIs(t, call(ctx, "has_dmm"), context.Canceled)
	}
	require.NoError(t, db.Close())
	for _, call := range listCalls {
		require.Error(t, call(t.Context(), "has_dmm", "id", "asc"))
	}
	for _, call := range countCalls {
		require.Error(t, call(t.Context(), "has_dmm"))
	}
}

// SQLite functions are connection-local; every pooled connection needs the hook.
func TestActressThumbnailFilterOnEveryConnection(t *testing.T) {
	db := newCreditTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	first, err := sqlDB.Conn(t.Context())
	require.NoError(t, err)
	defer first.Close()
	second, err := sqlDB.Conn(t.Context())
	require.NoError(t, err)
	defer second.Close()
	for _, conn := range []*sql.Conn{first, second} {
		for _, test := range []struct {
			raw  any
			want int
		}{{nil, 1}, {"\t\n", 1}, {"https://pics.dmm.co.jp/mono/actjpgs/missing", 1}, {"https://example.com/image", 0}} {
			var got int
			require.NoError(t, conn.QueryRowContext(t.Context(), "SELECT javinizer_missing_actress_thumbnail(COALESCE(?,''))", test.raw).Scan(&got))
			require.Equal(t, test.want, got)
		}
	}
}
