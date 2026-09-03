package database

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

// WordReplacementRepository persists word replacement records.
type WordReplacementRepository struct {
	*BaseRepository[models.WordReplacement, uint]
}

// NewWordReplacementRepository creates a word replacement repository backed by the given database.
func NewWordReplacementRepository(db *DB) *WordReplacementRepository {
	return &WordReplacementRepository{
		BaseRepository: NewBaseRepository[models.WordReplacement, uint](
			db, "word replacement",
			func(g models.WordReplacement) string { return g.Original },
			WithNewEntity[models.WordReplacement, uint](func() models.WordReplacement { return models.WordReplacement{} }),
		),
	}
}

// normalizeMatchMode validates and normalizes the entry's match mode:
// empty normalizes to literal; unknown values are rejected. (#228)
func normalizeMatchMode(replacement *models.WordReplacement) error {
	if replacement.MatchMode == "" {
		replacement.MatchMode = models.MatchModeLiteral
		return nil
	}
	if !models.IsValidMatchMode(replacement.MatchMode) {
		return fmt.Errorf("invalid match_mode %q", replacement.MatchMode)
	}
	return nil
}

// Create inserts a new word replacement record.
func (r *WordReplacementRepository) Create(ctx context.Context, replacement *models.WordReplacement) error {
	if err := normalizeMatchMode(replacement); err != nil {
		return err
	}
	return r.BaseRepository.Create(ctx, replacement)
}

// Upsert inserts the word replacement or updates the existing record matched by its original text.
// The stored mode is the (normalized) incoming mode: import-style callers that
// omit a mode write literal; the API update path preserves-on-omit is handled
// at the handler level, not here. (#228)
func (r *WordReplacementRepository) Upsert(ctx context.Context, replacement *models.WordReplacement) error {
	if err := normalizeMatchMode(replacement); err != nil {
		return err
	}
	existing, err := r.FindByOriginal(ctx, replacement.Original)
	if err != nil {
		if !IsNotFound(err) {
			return err
		}
		return r.Create(ctx, replacement)
	}

	replacement.ID = existing.ID
	replacement.CreatedAt = existing.CreatedAt
	if err := r.GetDB().WithContext(ctx).Save(replacement).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("word replacement %s", replacement.Original), err)
	}
	return nil
}

// FindByOriginal returns the word replacement matching the given original text.
func (r *WordReplacementRepository) FindByOriginal(ctx context.Context, original string) (*models.WordReplacement, error) {
	var replacement models.WordReplacement
	err := r.GetDB().WithContext(ctx).First(&replacement, "original = ?", original).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("word replacement %s", original), err)
	}
	return &replacement, nil
}

// List returns all stored word replacements.
func (r *WordReplacementRepository) List(ctx context.Context) ([]models.WordReplacement, error) {
	return r.ListAll(ctx)
}

// FindByID returns the word replacement with the given identifier.
func (r *WordReplacementRepository) FindByID(ctx context.Context, id uint) (*models.WordReplacement, error) {
	return r.BaseRepository.FindByID(ctx, id)
}

// DeleteByID removes the word replacement with the given identifier.
func (r *WordReplacementRepository) DeleteByID(ctx context.Context, id uint) error {
	return r.BaseRepository.Delete(ctx, id)
}

// Delete removes the word replacement matching the given original text.
func (r *WordReplacementRepository) Delete(ctx context.Context, original string) error {
	if err := r.GetDB().WithContext(ctx).Delete(&models.WordReplacement{}, "original = ?", original).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("word replacement %s", original), err)
	}
	return nil
}

// GetReplacementEntries returns all stored entries including their match mode. (#228)
func (r *WordReplacementRepository) GetReplacementEntries(ctx context.Context) ([]models.WordReplacement, error) {
	return r.List(ctx)
}

// GetReplacementMap returns a map of original text to replacement text for all stored entries.
func (r *WordReplacementRepository) GetReplacementMap(ctx context.Context) (map[string]string, error) {
	replacements, err := r.List(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, r := range replacements {
		result[r.Original] = r.Replacement
	}
	return result, nil
}

// IsDefaultWordReplacement reports whether the given original text is one of the built-in default replacements.
func IsDefaultWordReplacement(original string) bool {
	_, ok := defaultOrigins[original]
	return ok
}

var defaultOrigins map[string]struct{}
var defaultWordReplacements []models.WordReplacement

func init() {
	defaultWordReplacements = []models.WordReplacement{
		{Original: "[Recommended For Smartphones] ", Replacement: ""},
		{Original: "A*****t", Replacement: "Assault"},
		{Original: "A*****ted", Replacement: "Assaulted"},
		{Original: "A****p", Replacement: "Asleep"},
		{Original: "A***e", Replacement: "Abuse"},
		{Original: "B***d", Replacement: "Blood"},
		{Original: "B**d", Replacement: "Bled"},
		{Original: "C***d", Replacement: "Child"},
		{Original: "D******ed", Replacement: "Destroyed"},
		{Original: "D******eful", Replacement: "Shameful"},
		{Original: "D***k", Replacement: "Drunk"},
		{Original: "D***king", Replacement: "Drinking"},
		{Original: "D**g", Replacement: "Drug"},
		{Original: "D**gged", Replacement: "Drugged"},
		{Original: "F***", Replacement: "Fuck"},
		{Original: "F*****g", Replacement: "Forcing"},
		{Original: "F***e", Replacement: "Force"},
		{Original: "G*********d", Replacement: "Gang Banged"},
		{Original: "G*******g", Replacement: "Gang bang"},
		{Original: "G******g", Replacement: "Gangbang"},
		{Original: "H*********n", Replacement: "Humiliation"},
		{Original: "H*******ed", Replacement: "Hypnotized"},
		{Original: "H*******m", Replacement: "Hypnotism"},
		{Original: "I****t", Replacement: "Incest"},
		{Original: "I****tuous", Replacement: "Incestuous"},
		{Original: "K****p", Replacement: "Kidnap"},
		{Original: "K**l", Replacement: "Kill"},
		{Original: "K**ler", Replacement: "Killer"},
		{Original: "K*d", Replacement: "Kid"},
		{Original: "Ko**ji", Replacement: "Komyo-ji"},
		{Original: "Lo**ta", Replacement: "Lolita"},
		{Original: "M******r", Replacement: "Molester"},
		{Original: "M****t", Replacement: "Molest"},
		{Original: "M****ted", Replacement: "Molested"},
		{Original: "M****ter", Replacement: "Molester"},
		{Original: "M****ting", Replacement: "Molesting"},
		{Original: "P****h", Replacement: "Punish"},
		{Original: "P****hment", Replacement: "Punishment"},
		{Original: "P*A", Replacement: "PTA"},
		{Original: "R****g", Replacement: "Raping"},
		{Original: "R**e", Replacement: "Rape"},
		{Original: "R**ed", Replacement: "Raped"},
		{Original: "R*pe", Replacement: "Rape"},
		{Original: "S*********l", Replacement: "School Girl"},
		{Original: "S*********ls", Replacement: "School Girls"},
		{Original: "S********l", Replacement: "Schoolgirl"},
		{Original: "S********n", Replacement: "Submission"},
		{Original: "S******g", Replacement: "Sleeping"},
		{Original: "S*****t", Replacement: "Student"},
		{Original: "S***e", Replacement: "Slave"},
		{Original: "S***p", Replacement: "Sleep"},
		{Original: "S**t", Replacement: "Shit"},
		{Original: "Sch**l", Replacement: "School"},
		{Original: "Sch**lgirl", Replacement: "Schoolgirl"},
		{Original: "Sch**lgirls", Replacement: "Schoolgirls"},
		{Original: "SK**lful", Replacement: "Skillful"},
		{Original: "SK**ls", Replacement: "Skills"},
		{Original: "StepB****************r", Replacement: "Stepbrother and Sister"},
		{Original: "StepM************n", Replacement: "Stepmother and Son"},
		{Original: "StumB**d", Replacement: "Stumbled"},
		{Original: "T*****e", Replacement: "Torture"},
		{Original: "U*********sly", Replacement: "Unconsciously"},
		{Original: "U**verse", Replacement: "Universe"},
		{Original: "V*****e", Replacement: "Violate"},
		{Original: "V*****ed", Replacement: "Violated"},
		{Original: "V*****es", Replacement: "Violates"},
		{Original: "V*****t", Replacement: "Violent"},
		{Original: "Y********l", Replacement: "Young Girl"},
		{Original: "D******e", Replacement: "Disgrace"},
	}

	defaultOrigins = make(map[string]struct{}, len(defaultWordReplacements))
	for i := range defaultWordReplacements {
		defaultOrigins[defaultWordReplacements[i].Original] = struct{}{}
	}
}

// SeedDefaultWordReplacements upserts the built-in default word replacements into the repository.
func SeedDefaultWordReplacements(ctx context.Context, repo WordReplacementRepositoryInterface) {
	for i := range defaultWordReplacements {
		r := defaultWordReplacements[i]
		if err := repo.Upsert(ctx, &r); err != nil {
			logging.Warnf("failed to seed word replacement %q: %v", r.Original, err)
		}
	}
}
