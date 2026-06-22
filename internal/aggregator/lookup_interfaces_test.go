package aggregator

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/assert"
)

// Compile-time interface satisfaction checks.
// These verify that the database repositories satisfy the narrow lookup
// interfaces without requiring an adapter.

func TestGenreReplacementRepository_SatisfiesgenreLookup(t *testing.T) {
	var _ genreLookup = (*database.GenreReplacementRepository)(nil)
	assert.True(t, true, "GenreReplacementRepository satisfies genreLookup")
}

func TestWordReplacementRepository_SatisfieswordLookup(t *testing.T) {
	var _ wordLookup = (*database.WordReplacementRepository)(nil)
	assert.True(t, true, "WordReplacementRepository satisfies wordLookup")
}

func TestActressAliasRepository_SatisfiesaliasLookup(t *testing.T) {
	var _ aliasLookup = (*database.ActressAliasRepository)(nil)
	assert.True(t, true, "ActressAliasRepository satisfies aliasLookup")
}
