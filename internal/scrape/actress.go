package scrape

import (
	"context"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

func enrichActressesFromDB(ctx context.Context, scraped *models.Movie, actressRepo database.ActressRepositoryInterface, cfg *Config) int {
	if cfg == nil || !cfg.ActressDBEnabled {
		return 0
	}
	if actressRepo == nil || scraped == nil {
		return 0
	}

	enriched := 0
	for i := range scraped.Actresses {
		actress := &scraped.Actresses[i]
		dbActress, err := lookupActress(ctx, actressRepo, actress)
		if err != nil {
			continue
		}
		if enrichActressFields(actress, dbActress) {
			enriched++
		}
	}
	return enriched
}

func lookupActress(ctx context.Context, actressRepo database.ActressRepositoryInterface, actress *models.Actress) (*models.Actress, error) {
	if actress.DMMID > 0 {
		found, err := actressRepo.FindByDMMID(ctx, actress.DMMID)
		if err == nil {
			return found, nil
		}
		if !database.IsNotFound(err) {
			logging.Debugf("Actress DB lookup by DMMID %d failed: %v", actress.DMMID, err)
		}
	}
	if actress.JapaneseName != "" {
		found, err := actressRepo.FindByJapaneseName(ctx, actress.JapaneseName)
		if err == nil {
			return found, nil
		}
		if !database.IsNotFound(err) {
			logging.Debugf("Actress DB lookup by JapaneseName %s failed: %v", actress.JapaneseName, err)
		}
	}
	if actress.FirstName != "" && actress.LastName != "" {
		found, err := actressRepo.FindByFirstNameLastName(ctx, actress.FirstName, actress.LastName)
		if err == nil {
			return found, nil
		}
		if !database.IsNotFound(err) {
			logging.Debugf("Actress DB lookup by name %s %s failed: %v", actress.LastName, actress.FirstName, err)
		}
	}
	return nil, database.ErrNotFound
}

func enrichActressFields(actress *models.Actress, dbActress *models.Actress) bool {
	changed := false
	if actress.ThumbURL == "" && dbActress.ThumbURL != "" {
		actress.ThumbURL = dbActress.ThumbURL
		changed = true
	}
	if actress.FirstName == "" && dbActress.FirstName != "" {
		actress.FirstName = dbActress.FirstName
		changed = true
	}
	if actress.LastName == "" && dbActress.LastName != "" {
		actress.LastName = dbActress.LastName
		changed = true
	}
	if actress.JapaneseName == "" && dbActress.JapaneseName != "" {
		actress.JapaneseName = dbActress.JapaneseName
		changed = true
	}
	if changed {
		logging.Debugf("Enriched actress %s from database (ThumbURL=%s)", actress.FullName(), actress.ThumbURL)
	}
	return changed
}
