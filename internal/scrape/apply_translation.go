package scrape

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/timeout"
	"github.com/javinizer/javinizer-go/internal/translation"
)

// applyTranslation applies metadata translation using the Translator interface.
// Returns a warning string and its machine-readable code if translation partially
// failed, or empty strings on success.
// This is a standalone function — it does not belong to the Aggregator, which is a
// pure merge operation. Translation is an orthogonal concern invoked after aggregation.
func applyTranslation(ctx context.Context, scraped *models.Movie, translator Translator) (string, string, *translation.TranslationOutput) {
	if scraped == nil || translator == nil {
		return "", "", nil
	}
	warning, code, _, output := translator.Translate(ctx, scraped)
	return warning, code, output
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
func (ts *translationService) translateWithContext(ctx context.Context, scraped *models.Movie) (string, string, *translation.TranslationOutput) {
	if scraped == nil {
		return "", "", nil
	}

	resolved := timeout.FromConfig("metadata.translation.timeout_seconds", ts.timeoutSeconds, 60*time.Second)
	logging.Debugf("Translation: starting (provider=%s, source=%s, target=%s, hash=%s, timeout=%s)", ts.provider, ts.sourceLanguage, ts.targetLanguage, ts.settingsHash, resolved)

	transCtx, cancel := context.WithTimeout(ctx, resolved.Duration)
	defer cancel()

	output, warning, code, err := ts.service.TranslateMovie(transCtx, scraped, ts.settingsHash)
	if err != nil {
		ts.logTranslationWarning(transCtx, scraped, code, err)
		return warning, string(code), nil
	}
	if output == nil || output.Movie == nil {
		logging.Debugf("Translation: returned nil record (no fields to translate or source==target)")
		return "", "", nil
	}

	translatedRecord := output.Movie
	logging.Debugf("Translation: appending %s translation (title=%q, hash=%s)", translatedRecord.Language, translatedRecord.Title, translatedRecord.SettingsHash)

	scraped.Translations = mergeOrAppendTranslation(
		scraped.Translations,
		*translatedRecord,
		ts.overwriteExisting,
	)

	logging.Debugf("Translation: movie now has %d translation(s)", len(scraped.Translations))
	if code != "" {
		ts.logTranslationWarning(transCtx, scraped, code, nil)
	}
	return warning, string(code), output
}

// logTranslationWarning is the single per-movie warning emission point for
// translation failures and degradations, covering both the live-scrape and
// cache-hit paths (both call applyTranslation). It logs via WithFields with a
// structured field set; status_code is attached only when the classification
// came from an HTTP status. Context-canceled operations are suppressed per the
// translation-warning-display spec, while non-translation errors (e.g.
// misconfiguration) keep the legacy unstructured Warn for visibility.
func (ts *translationService) logTranslationWarning(ctx context.Context, scraped *models.Movie, code translation.TranslationWarningCode, err error) {
	id := scraped.ID
	if id == "" {
		id = scraped.ContentID
	}
	if code == "" {
		if err != nil && ctx.Err() == nil {
			logging.Warnf("[%s] Metadata translation failed: %v", id, err)
		}
		return
	}
	fields := logrus.Fields{
		"provider":     ts.provider,
		"mode":         ts.service.ProviderMode(),
		"source_lang":  ts.sourceLanguage,
		"target_lang":  ts.targetLanguage,
		"warning_code": string(code),
		"movie_id":     id,
	}
	if status, ok := translation.WarningStatusCode(ctx, err); ok {
		fields["status_code"] = status
	}
	logging.WithFields(fields).Warn("Metadata translation warning")
}

// newTranslationHTTPClient creates the shared HTTP client for translation providers.
func newTranslationHTTPClient() *http.Client {
	return &http.Client{
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
