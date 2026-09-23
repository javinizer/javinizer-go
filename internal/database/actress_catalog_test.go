package database

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestActressCatalogErrors(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	require.NoError(t, db.Close())
	_, err := repo.Count(t.Context())
	require.Error(t, err)
	_, err = repo.ListAll(t.Context())
	require.Error(t, err)
	_, err = repo.List(t.Context(), 0, 0)
	require.Error(t, err)
}

func TestActressCatalogExcludesCandidates(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	verified := models.Actress{FirstName: "Catalog", LastName: "Identity"}
	candidate := models.Actress{FirstName: "Hidden", LastName: "Candidate", Origin: "scrape"}
	require.NoError(t, repo.Create(t.Context(), &verified))
	require.NoError(t, repo.Create(t.Context(), &candidate))
	require.True(t, verified.Verified)
	require.Equal(t, ActressOriginUser, verified.Origin)
	require.False(t, candidate.Verified)

	count, err := repo.Count(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	count, err = repo.CountSearch(t.Context(), "Candidate")
	require.NoError(t, err)
	require.Zero(t, count)

	lists := []func() ([]models.Actress, error){
		func() ([]models.Actress, error) { return repo.ListAll(t.Context()) },
		func() ([]models.Actress, error) { return repo.List(t.Context(), 10, 0) },
		func() ([]models.Actress, error) { return repo.ListSorted(t.Context(), 10, 0, "name", "asc") },
		func() ([]models.Actress, error) { return repo.SearchPaged(t.Context(), "", 10, 0) },
		func() ([]models.Actress, error) { return repo.SearchPagedSorted(t.Context(), "", 10, 0, "name", "asc") },
		func() ([]models.Actress, error) { return repo.Search(t.Context(), "") },
		func() ([]models.Actress, error) { return repo.Search(t.Context(), "Catalog") },
	}
	for _, list := range lists {
		items, err := list()
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.Equal(t, verified.ID, items[0].ID)
	}
	items, err := repo.SearchPaged(t.Context(), "Hidden", 10, 0)
	require.NoError(t, err)
	require.Empty(t, items)
	items, err = repo.SearchPagedSorted(t.Context(), "Hidden", 10, 0, "name", "asc")
	require.NoError(t, err)
	require.Empty(t, items)
}
