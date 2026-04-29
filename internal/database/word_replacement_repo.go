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

// SeedDefaultWordReplacements populates the word replacement table with uncensor defaults.
// Each rule is upserted, so existing entries are preserved across restarts.
func SeedDefaultWordReplacements(repo *WordReplacementRepository) {
	defaults := []struct {
		Original    string
		Replacement string
	}{
		{"[Recommended For Smartphones] ", ""},
		{"A*****t", "Assault"},
		{"A*****ted", "Assaulted"},
		{"A****p", "Asleep"},
		{"A***e", "Abuse"},
		{"B***d", "Blood"},
		{"B**d", "Bled"},
		{"C***d", "Child"},
		{"D******ed", "Destroyed"},
		{"D******eful", "Shameful"},
		{"D***k", "Drunk"},
		{"D***king", "Drinking"},
		{"D**g", "Drug"},
		{"D**gged", "Drugged"},
		{"F***", "Fuck"},
		{"F*****g", "Forcing"},
		{"F***e", "Force"},
		{"G*********d", "Gang Banged"},
		{"G*******g", "Gang bang"},
		{"G******g", "Gangbang"},
		{"H*********n", "Humiliation"},
		{"H*******ed", "Hypnotized"},
		{"H*******m", "Hypnotism"},
		{"I****t", "Incest"},
		{"I****tuous", "Incestuous"},
		{"K****p", "Kidnap"},
		{"K**l", "Kill"},
		{"K**ler", "Killer"},
		{"K*d", "Kid"},
		{"Ko**ji", "Komyo-ji"},
		{"Lo**ta", "Lolita"},
		{"M******r", "Molester"},
		{"M****t", "Molest"},
		{"M****ted", "Molested"},
		{"M****ter", "Molester"},
		{"M****ting", "Molesting"},
		{"P****h", "Punish"},
		{"P****hment", "Punishment"},
		{"P*A", "PTA"},
		{"R****g", "Raping"},
		{"R**e", "Rape"},
		{"R**ed", "Raped"},
		{"R*pe", "Rape"},
		{"S*********l", "School Girl"},
		{"S*********ls", "School Girls"},
		{"S********l", "Schoolgirl"},
		{"S********n", "Submission"},
		{"S******g", "Sleeping"},
		{"S*****t", "Student"},
		{"S***e", "Slave"},
		{"S***p", "Sleep"},
		{"S**t", "Shit"},
		{"Sch**l", "School"},
		{"Sch**lgirl", "Schoolgirl"},
		{"Sch**lgirls", "Schoolgirls"},
		{"SK**lful", "Skillful"},
		{"SK**ls", "Skills"},
		{"StepB****************r", "Stepbrother and Sister"},
		{"StepM************n", "Stepmother and Son"},
		{"StumB**d", "Stumbled"},
		{"T*****e", "Torture"},
		{"U*********sly", "Unconsciously"},
		{"U**verse", "Universe"},
		{"V*****e", "Violate"},
		{"V*****ed", "Violated"},
		{"V*****es", "Violates"},
		{"V*****t", "Violent"},
		{"Y********l", "Young Girl"},
		{"D******e", "Disgrace"},
	}

	for _, d := range defaults {
		if err := repo.Upsert(&models.WordReplacement{
			Original:    d.Original,
			Replacement: d.Replacement,
		}); err != nil {
			logging.Warnf("failed to seed word replacement %q: %v", d.Original, err)
		}
	}
}
