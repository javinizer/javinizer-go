package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestActressSearchFullNameAcrossCatalogQueries(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	actor := models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", DMMID: 777123, Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actor).Error)
	candidate := models.Actress{FirstName: actor.FirstName, LastName: actor.LastName, JapaneseName: actor.JapaneseName, Verified: false, Origin: ActressOriginScrape}
	require.NoError(t, db.Create(&candidate).Error)
	for _, query := range []string{"Hatano Yui", "Yui Hatano", "hatan", "Yui", "多野", "7712"} {
		t.Run(query, func(t *testing.T) {
			searches := []func() ([]models.Actress, error){
				func() ([]models.Actress, error) { return repo.Search(t.Context(), query) },
				func() ([]models.Actress, error) { return repo.SearchPaged(t.Context(), query, 10, 0) },
				func() ([]models.Actress, error) {
					return repo.SearchPagedSorted(t.Context(), query, 10, 0, "name", "asc")
				},
				func() ([]models.Actress, error) {
					return repo.SearchFiltered(t.Context(), query, "has_dmm", 10, 0, "name", "asc")
				},
			}
			for _, search := range searches {
				results, err := search()
				require.NoError(t, err)
				require.Len(t, results, 1)
				require.Equal(t, actor.ID, results[0].ID)
			}
			count, err := repo.CountSearch(t.Context(), query)
			require.NoError(t, err)
			require.Equal(t, int64(1), count)
			count, err = repo.CountSearchFiltered(t.Context(), query, "has_dmm")
			require.NoError(t, err)
			require.Equal(t, int64(1), count)
			count, err = repo.CountSearchFiltered(t.Context(), query, "missing_dmm")
			require.NoError(t, err)
			require.Zero(t, count)
		})
	}
}

func TestActressSearchFullNamePreservesNullableSingleNames(t *testing.T) {
	db := newCreditTestDB(t)
	for _, tc := range []struct {
		name        string
		first, last any
	}{{"FirstOnly", "FirstOnly", nil}, {"LastOnly", nil, "LastOnly"}, {"JapaneseOnly", nil, nil}} {
		japanese := ""
		if tc.name == "JapaneseOnly" {
			japanese = tc.name
		}
		require.NoError(t, db.Exec("INSERT INTO actresses (first_name, last_name, japanese_name, verified, origin) VALUES (?, ?, ?, 1, 'user')", tc.first, tc.last, japanese).Error)
		results, err := NewActressRepository(db).SearchPaged(t.Context(), tc.name, 10, 0)
		require.NoError(t, err)
		require.Len(t, results, 1)
	}
	// SQL-looking user input remains bound data, not an executable predicate.
	results, err := NewActressRepository(db).SearchPaged(t.Context(), "' OR 1=1 --", 10, 0)
	require.NoError(t, err)
	require.Empty(t, results)
}
