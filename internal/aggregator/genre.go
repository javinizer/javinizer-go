package aggregator

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
	"gorm.io/gorm"
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

		// Try to create the replacement (will fail on unique constraint if already exists)
		a.genreCacheMutex.Lock()
		a.genreReplacementCache[original] = original
		a.genreCacheMutex.Unlock()
		if err := a.genreReplacementRepo.Create(genreReplacement); err != nil {
			// Best-effort: log non-unique-constraint errors
			if !errors.Is(err, gorm.ErrDuplicatedKey) {
				logging.Warnf("genre auto-add failed for %q: %v", original, err)
			}
		}
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

func (a *Aggregator) loadWordReplacementCache() {
	if a.wordReplacementRepo == nil {
		return
	}

	replacementMap, err := a.wordReplacementRepo.GetReplacementMap()
	if err == nil {
		// Pre-sort replacements: longest original first, then alphabetical
		pairs := make([]struct{ orig, repl string }, 0, len(replacementMap))
		for orig, repl := range replacementMap {
			pairs = append(pairs, struct{ orig, repl string }{orig, repl})
		}
		sort.Slice(pairs, func(i, j int) bool {
			if len(pairs[i].orig) != len(pairs[j].orig) {
				return len(pairs[i].orig) > len(pairs[j].orig)
			}
			return pairs[i].orig < pairs[j].orig
		})

		a.wordCacheMutex.Lock()
		a.wordReplacementCache = replacementMap
		a.wordReplacementSorted = pairs
		a.wordCacheMutex.Unlock()
	}
}

func (a *Aggregator) ReloadWordReplacements() {
	a.loadWordReplacementCache()
}

func (a *Aggregator) applyWordReplacement(text string) string {
	if a == nil || a.config == nil || !a.config.Metadata.WordReplacement.Enabled {
		return text
	}

	if text == "" {
		return text
	}

	a.wordCacheMutex.RLock()
	sorted := a.wordReplacementSorted
	a.wordCacheMutex.RUnlock()

	if len(sorted) == 0 {
		return text
	}

	result := text
	for _, p := range sorted {
		if strings.Contains(result, p.orig) {
			result = strings.ReplaceAll(result, p.orig, p.repl)
		}
	}

	return result
}

func (a *Aggregator) applyWordReplacements(movie *models.Movie) {
	if a == nil || a.config == nil || !a.config.Metadata.WordReplacement.Enabled {
		return
	}

	movie.Title = a.applyWordReplacement(movie.Title)
	movie.OriginalTitle = a.applyWordReplacement(movie.OriginalTitle)
	movie.Description = a.applyWordReplacement(movie.Description)
	movie.Director = a.applyWordReplacement(movie.Director)
	movie.Maker = a.applyWordReplacement(movie.Maker)
	movie.Label = a.applyWordReplacement(movie.Label)
	movie.Series = a.applyWordReplacement(movie.Series)

	for i := range movie.Translations {
		t := &movie.Translations[i]
		t.Title = a.applyWordReplacement(t.Title)
		t.OriginalTitle = a.applyWordReplacement(t.OriginalTitle)
		t.Description = a.applyWordReplacement(t.Description)
		t.Director = a.applyWordReplacement(t.Director)
		t.Maker = a.applyWordReplacement(t.Maker)
		t.Label = a.applyWordReplacement(t.Label)
		t.Series = a.applyWordReplacement(t.Series)
	}
}
