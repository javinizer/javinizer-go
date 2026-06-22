package genre

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// GenreDeps holds the repositories that genre handlers need.
// Replaces the removed GenreService — handlers take this directly,
// matching the Deps pattern used in the actress and movie packages.
//
// GenreDeps composes two domain-oriented sub-structs from database.Repositories:
// ReplacementRepos (genre + genre-replacement + word-replacement) and
// TranslationRepos (genre-translation). This gives genre handlers access to
// exactly the repos they need without depending on the full Repositories bag.
//
// GenreDeps is a pure data-access module — it does not orchestrate
// side-effects like cache invalidation. Handlers that mutate data
// must call the cache-invalidation function explicitly.
type GenreDeps struct {
	database.ReplacementRepos
	database.TranslationRepos
}

// NewGenreDeps creates a GenreDeps from domain-oriented sub-structs.
func NewGenreDeps(
	replacement database.ReplacementRepos,
	translation database.TranslationRepos,
) GenreDeps {
	return GenreDeps{
		ReplacementRepos: replacement,
		TranslationRepos: translation,
	}
}

// safeFindTranslationsByIDsAndLanguage returns translations for a batch of genres,
// returning nil if the translation repo is not configured.
func (d GenreDeps) safeFindTranslationsByIDsAndLanguage(ctx context.Context, genreIDs []uint, lang string) (map[uint][]models.GenreTranslation, error) {
	if d.GenreTranslationRepo == nil {
		return nil, nil
	}
	return d.GenreTranslationRepo.FindByGenreIDsAndLanguage(ctx, genreIDs, lang)
}
