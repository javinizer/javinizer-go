package aggregator

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/models"
)

// GenreLookup provides the minimal genre-replacement operations needed by
// GenreProcessor. The database-backed repository satisfies this interface
// implicitly — no adapter is required.
type genreLookup interface {
	// GetReplacementMap returns all genre replacement mappings from the store.
	GetReplacementMap(ctx context.Context) (map[string]string, error)

	// Create persists a new genre replacement entry.
	Create(ctx context.Context, replacement *models.GenreReplacement) error
}

// WordLookup provides the minimal word-replacement operations needed by
// WordProcessor. The database-backed repository satisfies this interface
// implicitly — no adapter is required.
type wordLookup interface {
	// GetReplacementMap returns all word replacement mappings from the store.
	GetReplacementMap(ctx context.Context) (map[string]string, error)
}

// AliasLookup provides the minimal actress-alias operations needed by
// AliasResolver. The database-backed repository satisfies this interface
// implicitly — no adapter is required.
type aliasLookup interface {
	// GetAliasMap returns all actress alias mappings from the store.
	GetAliasMap(ctx context.Context) (map[string]string, error)
}
