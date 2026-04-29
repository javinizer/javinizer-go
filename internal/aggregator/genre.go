package aggregator

import (
	"context"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
)

func (a *Aggregator) ApplyConfiguredTranslation(movie *models.Movie) string {
	if a == nil || movie == nil || a.config == nil {
		logging.Debugf("Translation: skipped (nil aggregator, movie, or config)")
		return ""
	}

	translationCfg := a.config.Metadata.Translation
	if !translationCfg.Enabled {
		logging.Debugf("Translation: skipped (disabled)")
		return ""
	}

	settingsHash := translationCfg.SettingsHash()
	logging.Debugf("Translation: starting (provider=%s, source=%s, target=%s, hash=%s)", translationCfg.Provider, translationCfg.SourceLanguage, translationCfg.TargetLanguage, settingsHash)

	timeout := translationCfg.TimeoutSeconds
	if timeout <= 0 {
		timeout = 60
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	service := translation.New(translationCfg)
	translatedRecord, warning, err := service.TranslateMovie(ctx, movie, settingsHash)
	if err != nil {
		id := movie.ID
		if id == "" {
			id = movie.ContentID
		}
		logging.Warnf("[%s] Metadata translation failed: %v", id, err)
		return warning
	}
	if translatedRecord == nil {
		logging.Debugf("Translation: returned nil record (no fields to translate or source==target)")
		return ""
	}

	logging.Debugf("Translation: appending %s translation (title=%q, hash=%s)", translatedRecord.Language, translatedRecord.Title, translatedRecord.SettingsHash)

	movie.Translations = mergeOrAppendTranslation(
		movie.Translations,
		*translatedRecord,
		translationCfg.OverwriteExistingTarget,
	)

	logging.Debugf("Translation: movie now has %d translation(s)", len(movie.Translations))
	return warning
}

// applyGenreReplacement applies genre replacement if one exists
func (a *Aggregator) applyGenreReplacement(original string) string {
	// Feature toggle: bypass DB-backed genre replacement entirely when disabled.
	if a == nil || a.config == nil || !a.config.Metadata.GenreReplacement.Enabled {
		return original
	}

	// Check cache first with read lock
	a.genreCacheMutex.RLock()
	replacement, exists := a.genreReplacementCache[original]
	a.genreCacheMutex.RUnlock()

	if exists {
		return replacement
	}

	// Auto-add genre if enabled and repository is available
	if a.config.Metadata.GenreReplacement.AutoAdd && a.genreReplacementRepo != nil {
		// Create identity mapping (genre maps to itself)
		genreReplacement := &models.GenreReplacement{
			Original:    original,
			Replacement: original,
		}

		// Try to create the replacement (will fail silently if already exists due to race condition)
		if err := a.genreReplacementRepo.Create(genreReplacement); err == nil {
			// Successfully added, update cache with write lock
			a.genreCacheMutex.Lock()
			a.genreReplacementCache[original] = original
			a.genreCacheMutex.Unlock()
		}
		// If create failed due to unique constraint (race condition), ignore the error
		// The genre is already in the database from another goroutine
	}

	// Return original if no replacement found
	return original
}

// isGenreIgnored checks if a genre should be ignored
// Supports both exact string matching and regex patterns
func (a *Aggregator) isGenreIgnored(genre string) bool {
	// First, check compiled regex patterns
	for _, re := range a.ignoreGenreRegexes {
		if re.MatchString(genre) {
			return true
		}
	}

	// Fall back to exact string matching for non-regex patterns
	for _, ignored := range a.config.Metadata.IgnoreGenres {
		if genre == ignored {
			return true
		}
	}

	return false
}

// ReloadGenreReplacements reloads the genre replacement cache from database
func (a *Aggregator) ReloadGenreReplacements() {
	a.loadGenreReplacementCache()
}
