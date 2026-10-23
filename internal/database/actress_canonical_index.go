package database

import (
	"sort"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
)

// syncActressCanonicalKeysTx refreshes a derived index inside its caller's
// transaction. SQLite serializes writers; queue entries and keys commit or roll
// back together with the authoritative names. SQL triggers also cover callers
// that bypass GORM hooks. Batches bound migration/backfill memory and parameters.
// Changing canonical representation/normalization rules requires a migration
// that re-enqueues every identity; persisted keys are derived, versioned data.
func syncActressCanonicalKeysTx(tx *gorm.DB) error {
	// Chained handles (for example Unscoped) must not retain lookup table/WHERE
	// state between queue operations. Preserve context and transaction connection.
	tx = tx.Session(&gorm.Session{})
	for {
		var ids []uint
		if err := tx.Table("actress_canonical_keys_dirty").Order("actress_id").Limit(256).Pluck("actress_id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		var actresses []models.Actress
		if err := tx.Where("id IN ?", ids).Find(&actresses).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM actress_canonical_keys WHERE actress_id IN ?", ids).Error; err != nil {
			return err
		}
		for i := range actresses {
			keys := sortedCanonicalActressKeys(&actresses[i])
			for _, key := range keys {
				if err := tx.Exec("INSERT INTO actress_canonical_keys (name_key, actress_id) VALUES (?, ?)", key, actresses[i].ID).Error; err != nil {
					return err
				}
			}
		}
		if err := tx.Exec("DELETE FROM actress_canonical_keys_dirty WHERE actress_id IN ?", ids).Error; err != nil {
			return err
		}
	}
}

func sortedCanonicalActressKeys(actress *models.Actress) []string {
	set := canonicalActressRepresentationKeys(actress)
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// canonicalActressesByKeysTx unions all canonical representations, not aliases.
// Chunking stays below SQLite's bind limit and de-duplicates cross-key matches.
// The predicate must be a trusted SQL fragment, never user input.
func canonicalActressesByKeysTx(tx *gorm.DB, keys []string, predicate string, args ...interface{}) ([]models.Actress, error) {
	tx = tx.Session(&gorm.Session{})
	if err := syncActressCanonicalKeysTx(tx); err != nil {
		return nil, err
	}
	var matches []models.Actress
	seen := make(map[uint]bool)
	for start := 0; start < len(keys); start += 256 {
		end := start + 256
		if end > len(keys) {
			end = len(keys)
		}
		query := tx.Where("id IN (SELECT actress_id FROM actress_canonical_keys WHERE name_key IN ?)", keys[start:end])
		if predicate != "" {
			query = query.Where(predicate, args...)
		}
		var batch []models.Actress
		if err := query.Find(&batch).Error; err != nil {
			return nil, err
		}
		for _, actress := range batch {
			if !seen[actress.ID] {
				matches = append(matches, actress)
				seen[actress.ID] = true
			}
		}
	}
	return matches, nil
}
