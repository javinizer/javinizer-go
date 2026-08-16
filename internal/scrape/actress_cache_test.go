package scrape

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnrichActressesFromBuiltinCacheFillsOnlyMissingFields(t *testing.T) {
	previous := lookupBuiltinActress
	lookupBuiltinActress = func(int, string, string, string) (actresscache.Record, bool) {
		return actresscache.Record{
			DMMID:        42,
			FirstName:    "NewFirst",
			LastName:     "NewLast",
			JapaneseName: "新名",
			ThumbURL:     "https://example.test/thumb.jpg",
			Aliases:      []string{"旧名", "別名"},
		}, true
	}
	defer func() { lookupBuiltinActress = previous }()

	movie := &models.Movie{Actresses: []models.Actress{{FirstName: "Existing"}}}
	assert.Equal(t, 1, enrichActressesFromBuiltinCache(movie))
	got := movie.Actresses[0]
	assert.Equal(t, 42, got.DMMID)
	assert.Equal(t, "Existing", got.FirstName)
	assert.Equal(t, "NewLast", got.LastName)
	assert.Equal(t, "新名", got.JapaneseName)
	assert.Equal(t, "https://example.test/thumb.jpg", got.ThumbURL)
	assert.Equal(t, "旧名|別名", got.Aliases)
}

func TestEnrichActressesFromBuiltinCacheSkipsMissesAndNilMovies(t *testing.T) {
	previous := lookupBuiltinActress
	lookupBuiltinActress = func(int, string, string, string) (actresscache.Record, bool) { return actresscache.Record{}, false }
	defer func() { lookupBuiltinActress = previous }()
	assert.Equal(t, 0, enrichActressesFromBuiltinCache(nil))
	assert.Equal(t, 0, enrichActressesFromBuiltinCache(&models.Movie{Actresses: []models.Actress{{JapaneseName: "未知"}}}))
}

func TestEnrichActressesFromBuiltinCacheDoesNotReplaceValidThumbnail(t *testing.T) {
	previous := lookupBuiltinActress
	lookupBuiltinActress = func(int, string, string, string) (actresscache.Record, bool) {
		return actresscache.Record{ThumbURL: "https://example.test/builtin.jpg"}, true
	}
	defer func() { lookupBuiltinActress = previous }()
	movie := &models.Movie{Actresses: []models.Actress{{ThumbURL: "https://example.test/user.jpg"}}}
	require.Equal(t, 0, enrichActressesFromBuiltinCache(movie))
	assert.Equal(t, "https://example.test/user.jpg", movie.Actresses[0].ThumbURL)
}
