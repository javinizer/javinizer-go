package translation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

const (
	providerOpenAI           = "openai"
	providerOpenAICompatible = "openai-compatible"
	providerDeepL            = "deepl"
	providerGoogle           = "google"
	providerAnthropic        = "anthropic"

	maxTranslationResponseSize = 10 * 1024 * 1024
)

type Service struct {
	cfg        config.TranslationConfig
	httpClient *http.Client
}

func New(cfg config.TranslationConfig) *Service {
	return &Service{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 0,
		},
	}
}

func (s *Service) TranslateMovie(ctx context.Context, movie *models.Movie, settingsHash string) (*models.MovieTranslation, string, error) {
	if s == nil || movie == nil || !s.cfg.Enabled {
		return nil, "", nil
	}

	targetLang := normalizeLanguage(s.cfg.TargetLanguage)
	sourceLang := normalizeLanguage(s.cfg.SourceLanguage)
	if targetLang == "" {
		return nil, "", fmt.Errorf("target language is required")
	}
	if sourceLang == "" {
		sourceLang = sourceLangAuto
	}

	if sourceLang != sourceLangAuto && sourceLang == targetLang {
		return nil, "", nil
	}

	type pendingText struct {
		text      string
		fieldName string
		apply     func(string)
	}

	requests := make([]pendingText, 0)
	translatedRecord := &models.MovieTranslation{
		Language:     targetLang,
		SourceName:   "translation:" + normalizeProvider(s.cfg.Provider),
		SettingsHash: settingsHash,
	}

	queueField := func(raw string, assignRecord func(string), assignMovie func(string), fieldName string) {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return
		}
		requests = append(requests, pendingText{
			text:      trimmed,
			fieldName: fieldName,
			apply: func(translated string) {
				assignRecord(translated)
				if s.cfg.ApplyToPrimary {
					assignMovie(translated)
				}
			},
		})
	}

	fields := s.cfg.Fields
	if fields.Title {
		queueField(movie.Title, func(v string) { translatedRecord.Title = v }, func(v string) { movie.Title = v }, "title")
	}
	if fields.OriginalTitle {
		queueField(movie.OriginalTitle, func(v string) { translatedRecord.OriginalTitle = v }, func(v string) { movie.OriginalTitle = v }, "original_title")
	}
	if fields.Description {
		queueField(movie.Description, func(v string) { translatedRecord.Description = v }, func(v string) { movie.Description = v }, "description")
	}
	if fields.Director {
		queueField(movie.Director, func(v string) { translatedRecord.Director = v }, func(v string) { movie.Director = v }, "director")
	}
	if fields.Maker {
		queueField(movie.Maker, func(v string) { translatedRecord.Maker = v }, func(v string) { movie.Maker = v }, "maker")
	}
	if fields.Label {
		queueField(movie.Label, func(v string) { translatedRecord.Label = v }, func(v string) { movie.Label = v }, "label")
	}
	if fields.Series {
		queueField(movie.Series, func(v string) { translatedRecord.Series = v }, func(v string) { movie.Series = v }, "series")
	}
	if fields.Genres {
		for i := range movie.Genres {
			idx := i
			queueField(movie.Genres[idx].Name, func(string) {}, func(v string) {
				movie.Genres[idx].Name = v
			}, fmt.Sprintf("genre[%d]", i))
		}
	}
	if fields.Actresses {
		for i := range movie.Actresses {
			idx := i
			name := actressDisplayTitle(movie.Actresses[idx])
			if strings.TrimSpace(name) == "" {
				continue
			}
			queueField(name, func(string) {}, func(v string) {
				replaceActressName(&movie.Actresses[idx], v)
			}, fmt.Sprintf("actress[%d]", i))
		}
	}

	if len(requests) == 0 {
		return nil, "", nil
	}

	texts := make([]string, 0, len(requests))
	for _, req := range requests {
		texts = append(texts, req.text)
	}

	translatedTexts, err := s.translateTexts(ctx, sourceLang, targetLang, texts)
	if err != nil {
		logging.Debugf("Translation: translateTexts failed: %v", err)
		warning := sanitizeTranslationWarning(normalizeProvider(s.cfg.Provider), err)
		return nil, warning, err
	}
	if len(translatedTexts) != len(requests) {
		logging.Debugf("Translation: count mismatch - got %d, expected %d", len(translatedTexts), len(requests))
		return nil, "", fmt.Errorf("translation provider returned %d items for %d inputs", len(translatedTexts), len(requests))
	}

	var warnings []string
	for i := range requests {
		raw := translatedTexts[i]
		translated := strings.TrimSpace(raw)
		if translated == "" {
			logging.Debugf("Translation: empty result for %s (original=%q, raw=%q), falling back to original", requests[i].fieldName, requests[i].text, raw)
			warnings = append(warnings, fmt.Sprintf("%s: empty translation, kept original", requests[i].fieldName))
			translated = requests[i].text
		}
		requests[i].apply(translated)
	}

	var warning string
	if len(warnings) > 0 {
		warning = fmt.Sprintf("Translation (%s): %s", normalizeProvider(s.cfg.Provider), strings.Join(warnings, "; "))
		logging.Warnf("Translation: %s", warning)
	}

	return translatedRecord, warning, nil
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func sanitizeTranslationWarning(provider string, err error) string {
	var te *TranslationError
	if errors.As(err, &te) && te.Kind == TranslationErrorHTTPStatus {
		logging.Warnf("Translation (%s): HTTP %d error", provider, te.StatusCode)
		switch {
		case te.StatusCode == 429:
			return "Translation failed: rate limited, try again later"
		case te.StatusCode == 403:
			return "Translation failed: access denied, check API key"
		case te.StatusCode >= 500:
			return "Translation failed: external service error"
		case te.StatusCode >= 400:
			return "Translation failed: request error"
		}
	}
	if errors.As(err, &te) {
		return "Translation failed: service unavailable"
	}
	return "Translation failed: internal error"
}

func normalizeLanguage(language string) string {
	return strings.ToLower(strings.TrimSpace(language))
}

func actressDisplayTitle(actress models.Actress) string {
	if strings.TrimSpace(actress.JapaneseName) != "" {
		return actress.JapaneseName
	}
	full := strings.TrimSpace(strings.TrimSpace(actress.LastName) + " " + strings.TrimSpace(actress.FirstName))
	return full
}

func replaceActressName(actress *models.Actress, translated string) {
	translated = strings.TrimSpace(translated)
	if actress == nil || translated == "" {
		return
	}

	if strings.TrimSpace(actress.JapaneseName) != "" || (strings.TrimSpace(actress.FirstName) == "" && strings.TrimSpace(actress.LastName) == "") {
		actress.JapaneseName = translated
		return
	}

	actress.FirstName = translated
	actress.LastName = ""
}

const maxTranslationRetries = 3

type translationResult struct {
	texts  []string
	rawLLM string
}

func (s *Service) translateTexts(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	provider := normalizeProvider(s.cfg.Provider)

	var lastResult *translationResult
	var lastErr error
	expectedCount := len(texts)

	for attempt := 1; attempt <= maxTranslationRetries; attempt++ {
		var result *translationResult
		var err error

		switch provider {
		case providerOpenAI:
			result, err = s.translateWithOpenAI(ctx, sourceLang, targetLang, texts)
		case providerDeepL:
			result, err = s.translateWithDeepL(ctx, sourceLang, targetLang, texts)
		case providerGoogle:
			result, err = s.translateWithGoogle(ctx, sourceLang, targetLang, texts)
		case providerOpenAICompatible:
			result, err = s.translateWithOpenAICompatible(ctx, sourceLang, targetLang, texts)
		case providerAnthropic:
			result, err = s.translateWithAnthropic(ctx, sourceLang, targetLang, texts)
		default:
			return nil, fmt.Errorf("unsupported translation provider: %s", provider)
		}

		if err == nil {
			if result == nil {
				err = &TranslationError{Kind: TranslationErrorProvider, Message: "translation provider returned no result"}
			} else if len(result.texts) != expectedCount {
				err = &TranslationError{
					Kind:    TranslationErrorCountMismatch,
					Message: fmt.Sprintf("translation provider returned %d items for %d inputs", len(result.texts), expectedCount),
				}
			}
		}

		if err == nil && result != nil {
			return result.texts, nil
		}

		lastResult = result
		lastErr = err

		if attempt < maxTranslationRetries {
			if isRetryableError(err, result) {
				logging.Debugf("Translation: attempt %d/%d failed (%v), retrying...", attempt, maxTranslationRetries, err)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
				}
			} else {
				logging.Debugf("Translation: attempt %d/%d failed with non-retryable error (%v), giving up", attempt, maxTranslationRetries, err)
				break
			}
		}
	}

	if lastResult != nil && lastResult.rawLLM != "" {
		logging.Debugf("Translation: all %d attempts failed. Last LLM output (length=%d):\n%s", maxTranslationRetries, len(lastResult.rawLLM), lastResult.rawLLM)
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, &TranslationError{
		Kind:    TranslationErrorProvider,
		Message: fmt.Sprintf("translation failed after %d attempts", maxTranslationRetries),
	}
}

func isRetryableError(err error, result *translationResult) bool {
	if err == nil {
		return result != nil && len(result.texts) == 0 && result.rawLLM != ""
	}

	var te *TranslationError
	if errors.As(err, &te) {
		switch te.Kind {
		case TranslationErrorCountMismatch, TranslationErrorParse:
			return result != nil && result.rawLLM != ""
		default:
			return false
		}
	}

	return false
}
