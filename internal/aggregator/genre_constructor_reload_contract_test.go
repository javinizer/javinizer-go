package aggregator_test

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// Injected through the existing public constructor's repository contract.
// Failed reads deliberately include data: an error must never publish that map.
type genreCacheContractRepository struct {
	t        *testing.T
	mapping  map[string]string
	err      error
	contexts []context.Context
}

func (r *genreCacheContractRepository) GetReplacementMap(ctx context.Context) (map[string]string, error) {
	r.contexts = append(r.contexts, ctx)
	if err := ctx.Err(); err != nil {
		return r.mapping, err
	}
	return r.mapping, r.err
}
func (r *genreCacheContractRepository) Create(context.Context, *models.GenreReplacement) error {
	r.t.Fatal("auto-add disabled: cache reads must not create replacements")
	return nil
}

func TestGenreConstructorAndReloadPreserveCacheOnRepositoryFailures(t *testing.T) {
	cfg := &aggregator.MetadataConfig{IgnoreGenres: []string{"^Hidden"}}
	cfg.GenreReplacement.Enabled = true
	repo := &genreCacheContractRepository{t: t, mapping: map[string]string{"Original": "Uncommitted"}, err: errors.New("repository unavailable")}
	processor := aggregator.NewGenreProcessor(cfg, repo)
	require.NotNil(t, processor)
	require.Len(t, repo.contexts, 1)
	require.NoError(t, repo.contexts[0].Err())
	require.Nil(t, repo.contexts[0].Done(), "constructor uses an unbounded background context")
	_, deadline := repo.contexts[0].Deadline()
	require.False(t, deadline)
	engine := aggregator.New(&aggregator.Config{Metadata: cfg, ScrapersPriority: []string{"contract"}}, processor, nil, nil)
	input := &models.ScraperResult{Source: "contract", ID: "ABC-001", Title: "Original title", Genres: []string{"Original", "Hidden sample"}}
	assertGenre := func(want string) {
		t.Helper()
		movie, _, err := engine.Aggregate([]*models.ScraperResult{input})
		require.NoError(t, err)
		require.NotNil(t, movie)
		require.Equal(t, []models.Genre{{Name: want}}, movie.Genres)
		require.Equal(t, []string{"Original", "Hidden sample"}, input.Genres)
	}
	assertGenre("Original") // Failed startup read still compiles filtering regexes.

	repo.mapping, repo.err = map[string]string{"Original": "Committed"}, nil
	live := context.WithValue(t.Context(), genreCacheContextKey{}, "reload request")
	processor.Reload(live)
	require.Equal(t, live, repo.contexts[1], "Reload forwards its caller's context")
	assertGenre("Committed")

	repo.mapping, repo.err = map[string]string{"Original": "Uncommitted replacement"}, errors.New("read failed")
	processor.Reload(live)
	assertGenre("Committed")

	repo.err = nil
	canceled, cancel := context.WithCancel(live)
	cancel()
	processor.Reload(canceled)
	require.Equal(t, canceled, repo.contexts[3])
	require.ErrorIs(t, repo.contexts[3].Err(), context.Canceled)
	assertGenre("Committed")

	repo.mapping = map[string]string{"Original": "Recovered"}
	processor.Reload(live)
	require.Len(t, repo.contexts, 5)
	assertGenre("Recovered")
}

type genreCacheContextKey struct{}

func TestGenreConstructorSkipsUnavailableOrDisabledRepositories(t *testing.T) {
	repo := &genreCacheContractRepository{t: t, err: errors.New("must not be read")}
	require.Nil(t, aggregator.NewGenreProcessor(nil, repo))
	disabled := &aggregator.MetadataConfig{}
	processor := aggregator.NewGenreProcessor(disabled, repo)
	require.NotNil(t, processor)
	require.Empty(t, repo.contexts)
	enabled := &aggregator.MetadataConfig{}
	enabled.GenreReplacement.Enabled = true
	noRepo := aggregator.NewGenreProcessor(enabled, nil)
	require.NotNil(t, noRepo)
	noRepo.Reload(t.Context())
	engine := aggregator.New(&aggregator.Config{Metadata: enabled, ScrapersPriority: []string{"contract"}}, noRepo, nil, nil)
	movie, _, err := engine.Aggregate([]*models.ScraperResult{{Source: "contract", ID: "ABC-001", Genres: []string{"Original"}}})
	require.NoError(t, err)
	require.Equal(t, []models.Genre{{Name: "Original"}}, movie.Genres)
	require.Empty(t, repo.contexts)
}
