package actress

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// ActressDeps holds the repositories that actress handlers need.
// Replaces the removed ActressService — handlers take this directly,
// matching the Deps pattern used in the genre and movie packages.
//
// ActressDeps composes ContentRepos (for ActressRepo) and
// TranslationRepos (for ActressTranslationRepo), giving handlers
// exactly the repos they need without the full Repositories bag.
type ActressDeps struct {
	database.ContentRepos
	database.TranslationRepos
}

// NewActressDeps creates an ActressDeps from domain-oriented sub-structs.
func NewActressDeps(content database.ContentRepos, translation database.TranslationRepos) ActressDeps {
	return ActressDeps{
		ContentRepos:     content,
		TranslationRepos: translation,
	}
}

// safeFindTranslationsByIDs returns translations for a batch of actresses,
// returning nil if the translation repo is not configured.
func (d ActressDeps) safeFindTranslationsByIDs(ctx context.Context, actressIDs []uint, lang string) (map[uint][]models.ActressTranslation, error) {
	if d.ActressTranslationRepo == nil {
		return nil, nil
	}
	return d.ActressTranslationRepo.FindByActressIDsAndLanguage(ctx, actressIDs, lang)
}

// safeFindTranslationByActress returns a translation for a single actress,
// returning nil if the translation repo is not configured.
func (d ActressDeps) safeFindTranslationByActress(ctx context.Context, actressID uint, lang string) (*models.ActressTranslation, error) {
	if d.ActressTranslationRepo == nil {
		return nil, nil
	}
	return d.ActressTranslationRepo.FindByActressAndLanguage(ctx, actressID, lang)
}
