package database

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

type WordReplacementRepository struct {
	*BaseRepository[models.WordReplacement, uint]
}

func NewWordReplacementRepository(db *DB) *WordReplacementRepository {
	return &WordReplacementRepository{
		BaseRepository: NewBaseRepository[models.WordReplacement, uint](
			db, "word replacement",
			func(g models.WordReplacement) string { return g.Original },
			WithNewEntity[models.WordReplacement, uint](func() models.WordReplacement { return models.WordReplacement{} }),
		),
	}
}

func (r *WordReplacementRepository) Create(replacement *models.WordReplacement) error {
	return r.BaseRepository.Create(replacement)
}

func (r *WordReplacementRepository) Upsert(replacement *models.WordReplacement) error {
	existing, err := r.FindByOriginal(replacement.Original)
	if err != nil {
		if !isRecordNotFound(err) {
			return err
		}
		return r.Create(replacement)
	}

	replacement.ID = existing.ID
	replacement.CreatedAt = existing.CreatedAt
	if err := r.GetDB().Save(replacement).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("word replacement %s", replacement.Original), err)
	}
	return nil
}

func (r *WordReplacementRepository) FindByOriginal(original string) (*models.WordReplacement, error) {
	var replacement models.WordReplacement
	err := r.GetDB().First(&replacement, "original = ?", original).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("word replacement %s", original), err)
	}
	return &replacement, nil
}

func (r *WordReplacementRepository) List() ([]models.WordReplacement, error) {
	return r.ListAll()
}

func (r *WordReplacementRepository) FindByID(id uint) (*models.WordReplacement, error) {
	return r.BaseRepository.FindByID(id)
}

func (r *WordReplacementRepository) DeleteByID(id uint) error {
	return r.BaseRepository.Delete(id)
}

func (r *WordReplacementRepository) Delete(original string) error {
	if err := r.GetDB().Delete(&models.WordReplacement{}, "original = ?", original).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("word replacement %s", original), err)
	}
	return nil
}

func (r *WordReplacementRepository) GetReplacementMap() (map[string]string, error) {
	replacements, err := r.List()
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, r := range replacements {
		result[r.Original] = r.Replacement
	}
	return result, nil
}

// DefaultWordReplacements returns the full list of default uncensor replacements.
// This is the single source of truth for default word replacement data.
func DefaultWordReplacements() []models.WordReplacement {
	dst := make([]models.WordReplacement, len(defaultWordReplacements))
	copy(dst, defaultWordReplacements)
	return dst
}

// IsDefaultWordReplacement returns true if the given original string matches
// one of the default uncensor replacements.
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

// SeedDefaultWordReplacements populates the word replacement table with uncensor defaults.
// Each rule is upserted, so existing entries are preserved across restarts.
func SeedDefaultWordReplacements(repo *WordReplacementRepository) {
	for i := range defaultWordReplacements {
		r := defaultWordReplacements[i]
		if err := repo.Upsert(&r); err != nil {
			logging.Warnf("failed to seed word replacement %q: %v", r.Original, err)
		}
	}
}
