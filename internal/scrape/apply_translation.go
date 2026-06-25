package scrape

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/translation"
)

// applyTranslation applies metadata translation using the Translator interface.
// Returns a warning string if translation partially failed, or empty string on success.
// This is a standalone function — it does not belong to the Aggregator, which is a
// pure merge operation. Translation is an orthogonal concern invoked after aggregation.
func applyTranslation(ctx context.Context, scraped *models.Movie, translator Translator) (string, *translation.TranslationOutput) {
	if scraped == nil || translator == nil {
		return "", nil
	}
	warning, _, output := translator.Translate(ctx, scraped)
	return warning, output
}

// translationService wraps a pre-constructed translation.Service to avoid
// creating HTTP clients and providers per invocation (WR-07 fix).
type translationService struct {
	service           *translation.Service
	provider          string
	sourceLanguage    string
	targetLanguage    string
	settingsHash      string
	timeoutSeconds    int
	overwriteExisting bool
}

func newTranslationService(provider string, sourceLanguage string, targetLanguage string, settingsHash string, timeoutSeconds int, overwriteExisting bool, svc *translation.Service) *translationService {
	return &translationService{
		service:           svc,
		provider:          provider,
		sourceLanguage:    sourceLanguage,
		targetLanguage:    targetLanguage,
		settingsHash:      settingsHash,
		timeoutSeconds:    timeoutSeconds,
		overwriteExisting: overwriteExisting,
	}
}

// translateWithContext performs the translation using the provided context.
// This is the context-accepting variant used by the Translator interface.
// The configured Metadata.Translation.TimeoutSeconds (populated from
// METADATA_TRANSLATION_TIMEOUT_SECONDS) bounds the whole translation as a
// context deadline, mirroring main's ApplyConfiguredTranslation which wrapped
// TranslateMovie in context.WithTimeout(TimeoutSeconds||60). A value <= 0
// defaults to 60s; the caller's ctx is always respected as the parent.
func (ts *translationService) translateWithContext(ctx context.Context, scraped *models.Movie) (string, *translation.TranslationOutput) {
	if scraped == nil {
		return "", nil
	}

	logging.Debugf("Translation: starting (provider=%s, source=%s, target=%s, hash=%s)", ts.provider, ts.sourceLanguage, ts.targetLanguage, ts.settingsHash)

	timeout := ts.timeoutSeconds
	if timeout <= 0 {
		timeout = 60
	}
	transCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	output, warning, err := ts.service.TranslateMovie(transCtx, scraped, ts.settingsHash)
	if err != nil {
		id := scraped.ID
		if id == "" {
			id = scraped.ContentID
		}
		logging.Warnf("[%s] Metadata translation failed: %v", id, err)
		return warning, nil
	}
	if output == nil || output.Movie == nil {
		logging.Debugf("Translation: returned nil record (no fields to translate or source==target)")
		return "", nil
	}

	translatedRecord := output.Movie
	logging.Debugf("Translation: appending %s translation (title=%q, hash=%s)", translatedRecord.Language, translatedRecord.Title, translatedRecord.SettingsHash)

	scraped.Translations = mergeOrAppendTranslation(
		scraped.Translations,
		*translatedRecord,
		ts.overwriteExisting,
	)

	logging.Debugf("Translation: movie now has %d translation(s)", len(scraped.Translations))
	return warning, output
}

// newTranslationHTTPClient creates the shared HTTP client for translation providers.
func newTranslationHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  false,
			MaxIdleConnsPerHost: 2,
		},
	}
}

// mergeOrAppendTranslation merges or appends an incoming translation to existing translations.
// Moved from aggregator package — needed by the standalone applyConfiguredTranslation.
func mergeOrAppendTranslation(
	existing []models.MovieTranslation,
	incoming models.MovieTranslation,
	overwrite bool,
) []models.MovieTranslation {
	targetLanguage := strings.ToLower(strings.TrimSpace(incoming.Language))
	if targetLanguage == "" {
		return existing
	}

	for i := range existing {
		if strings.ToLower(strings.TrimSpace(existing[i].Language)) != targetLanguage {
			continue
		}

		if overwrite {
			existing[i] = mergeTranslationFields(existing[i], incoming)
		}
		return existing
	}

	return append(existing, incoming)
}

// mergeTranslationFields merges incoming translation fields into current translation.
func mergeTranslationFields(current, incoming models.MovieTranslation) models.MovieTranslation {
	merged := current
	merged.Language = incoming.Language

	if incoming.Title != "" {
		merged.Title = incoming.Title
	}
	if incoming.OriginalTitle != "" {
		merged.OriginalTitle = incoming.OriginalTitle
	}
	if incoming.Description != "" {
		merged.Description = incoming.Description
	}
	if incoming.Director != "" {
		merged.Director = incoming.Director
	}
	if incoming.Maker != "" {
		merged.Maker = incoming.Maker
	}
	if incoming.Label != "" {
		merged.Label = incoming.Label
	}
	if incoming.Series != "" {
		merged.Series = incoming.Series
	}
	if incoming.SourceName != "" {
		merged.SourceName = incoming.SourceName
	}
	if incoming.SettingsHash != "" {
		merged.SettingsHash = incoming.SettingsHash
	}

	return merged
}
