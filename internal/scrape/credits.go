package scrape

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// BuildCreditsFromScrape implements the credit identity lifecycle contract.
func BuildCreditsFromScrape(movie *models.Movie, actressSources map[string]string, results []*models.ScraperResult) {
	if movie == nil {
		return
	}
	if len(movie.Actresses) == 0 {
		movie.Credits = []models.MovieCredit{}
		return
	}
	resultsBySource := make(map[string]*models.ScraperResult, len(results))
	for _, result := range results {
		if result == nil {
			continue
		}
		source := strings.TrimSpace(result.Source)
		if source == "" {
			continue
		}
		resultsBySource[source] = result
	}

	credits := make([]models.MovieCredit, 0, len(movie.Actresses))
	seen := make(map[string]bool, len(movie.Actresses))
	for i := range movie.Actresses {
		actress := &movie.Actresses[i]
		key := ActressSourceKey(*actress)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true

		source := strings.TrimSpace(actressSources[key])
		creditedName := actress.FullName()
		creditedJP := actress.JapaneseName
		reportedThumb := actress.ThumbURL
		if source != "" {
			if result, ok := resultsBySource[source]; ok {
				for _, info := range result.Actresses {
					if actressInfoMatchesKey(info, key) {
						creditedName = infoFullName(info)
						creditedJP = info.JapaneseName
						reportedThumb = info.ThumbURL
						break
					}
				}
			}
		}

		credit := models.MovieCredit{
			CreditedName:         creditedName,
			CreditedJapaneseName: creditedJP,
			ReportedThumbURL:     reportedThumb,
			Source:               source,
			Origin:               string(models.CreditOriginScrape),
			OrderIndex:           len(credits),
			Scraped:              *actress,
		}
		credits = append(credits, credit)
	}
	movie.Credits = credits
}

// AttachCreditPolicy implements the credit identity lifecycle contract.
func AttachCreditPolicy(movie *models.Movie, cfg *Config) {
	if movie == nil || cfg == nil {
		return
	}
	movie.CreditPolicy = cfg.CollisionPolicy
	movie.TrustedCollisionSources = cfg.TrustedCollisionSources
}

func infoFullName(info models.ActressInfo) string {
	if strings.TrimSpace(info.JapaneseName) != "" && strings.TrimSpace(info.FirstName) == "" && strings.TrimSpace(info.LastName) == "" {
		return strings.TrimSpace(info.JapaneseName)
	}
	if info.LastName != "" && info.FirstName != "" {
		return strings.TrimSpace(info.LastName + " " + info.FirstName)
	}
	if info.FirstName != "" {
		return strings.TrimSpace(info.FirstName)
	}
	return strings.TrimSpace(info.LastName)
}

func actressInfoMatchesKey(info models.ActressInfo, key string) bool {
	for _, infoKey := range actressSourceKeysFromInfo(info) {
		if infoKey == key {
			return true
		}
	}
	return false
}
