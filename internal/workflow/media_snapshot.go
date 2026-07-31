package workflow

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

type scrapedMediaSnapshot struct {
	CoverURL         string
	PosterURL        string
	CroppedPosterURL string
	ShouldCropPoster bool
	TrailerURL       string
	Screenshots      []string
	Actresses        []models.Actress
}

func snapshotScrapedMedia(movie *models.Movie) *scrapedMediaSnapshot {
	if movie == nil {
		return nil
	}
	return &scrapedMediaSnapshot{
		CoverURL:         movie.Poster.CoverURL,
		PosterURL:        movie.Poster.PosterURL,
		CroppedPosterURL: movie.Poster.CroppedPosterURL,
		ShouldCropPoster: movie.Poster.ShouldCropPoster,
		TrailerURL:       movie.TrailerURL,
		Screenshots:      append([]string(nil), movie.Screenshots...),
		Actresses:        cloneActresses(movie.Actresses),
	}
}

func (s *scrapedMediaSnapshot) overlay(movie *models.Movie) *models.Movie {
	if movie == nil {
		return nil
	}
	result := movie.Clone()
	if s == nil {
		return result
	}
	result.Poster.CoverURL = s.CoverURL
	result.Poster.PosterURL = s.PosterURL
	result.Poster.CroppedPosterURL = s.CroppedPosterURL
	result.Poster.ShouldCropPoster = s.ShouldCropPoster
	result.TrailerURL = s.TrailerURL
	result.Screenshots = append([]string(nil), s.Screenshots...)
	result.Actresses = overlayActressThumbs(result.Actresses, s.Actresses)
	return result
}

func cloneActresses(actresses []models.Actress) []models.Actress {
	if actresses == nil {
		return nil
	}
	result := make([]models.Actress, len(actresses))
	copy(result, actresses)
	for i := range actresses {
		if actresses[i].Translations != nil {
			result[i].Translations = make([]models.ActressTranslation, len(actresses[i].Translations))
			copy(result[i].Translations, actresses[i].Translations)
		}
	}
	return result
}

func overlayActressThumbs(merged, scraped []models.Actress) []models.Actress {
	result := cloneActresses(merged)
	used := make([]bool, len(scraped))
	for i := range result {
		index := findScrapedActress(result[i], scraped, used)
		if index < 0 {
			result[i].ThumbURL = ""
			continue
		}
		result[i].ThumbURL = scraped[index].ThumbURL
		used[index] = true
	}
	for i := range scraped {
		if !used[i] {
			result = append(result, cloneActresses(scraped[i:i+1])...)
		}
	}
	return result
}

func findScrapedActress(merged models.Actress, scraped []models.Actress, used []bool) int {
	matches := make([]int, 0, len(scraped))
	for i := range scraped {
		if used[i] || !actressJapaneseNameMatch(merged, scraped[i]) {
			continue
		}
		matches = append(matches, i)
	}
	if len(matches) == 0 {
		for i := range scraped {
			if used[i] || !actressRomanizedNameMatch(merged, scraped[i]) {
				continue
			}
			matches = append(matches, i)
		}
	}
	if len(matches) == 0 {
		return -1
	}
	if len(matches) > 1 && merged.DMMID > 0 {
		for _, index := range matches {
			if scraped[index].DMMID > 0 && scraped[index].DMMID == merged.DMMID {
				return index
			}
		}
	}
	return matches[0]
}

func actressJapaneseNameMatch(a, b models.Actress) bool {
	jpA := normalizedActressName(a.JapaneseName)
	jpB := normalizedActressName(b.JapaneseName)
	return jpA != "" && jpA == jpB
}

func actressRomanizedNameMatch(a, b models.Actress) bool {
	firstA := normalizedActressName(a.FirstName)
	lastA := normalizedActressName(a.LastName)
	firstB := normalizedActressName(b.FirstName)
	lastB := normalizedActressName(b.LastName)
	if firstA == "" && lastA == "" || firstB == "" && lastB == "" {
		return false
	}
	return (firstA == firstB && lastA == lastB) || (firstA == lastB && lastA == firstB)
}

func normalizedActressName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
