package translation

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

const maxTranslationResponseSize = 10 * 1024 * 1024

type Service struct {
	cfg       Config
	providers map[string]TranslatorProvider
	randMu    sync.Mutex
	rand      *rand.Rand // per-instance random source for retry jitter; protected by randMu
}

func New(cfg Config, providers ...TranslatorProvider) *Service {
	m := make(map[string]TranslatorProvider, len(providers))
	for _, p := range providers {
		m[p.Name()] = p
	}
	return &Service{
		cfg:       cfg,
		providers: m,
		rand:      rand.New(rand.NewSource(time.Now().UnixNano())), //nolint:gosec // non-crypto rand is fine for retry jitter
	}
}

// TranslationOutput holds all translation data produced by TranslateMovie.
// It carries genre/actress translation data as return values rather than
// mutating *models.Movie, making the translation seam explicit.
type TranslationOutput struct {
	Movie               *models.MovieTranslation
	GenreTranslations   []models.GenreTranslationData
	ActressTranslations []models.ActressTranslationData
}

// TranslationPlan captures the fields to translate as data (no closures), enabling
// inspection, batching, and deterministic application. It replaces the previous
// closure-based pendingField approach so that plan → execute is a two-phase seam.
type TranslationPlan struct {
	TargetLang     string
	SourceLang     string
	SourceLabel    string
	ApplyToPrimary bool // when true, translated values are also written back to scraped movie
	Fields         []TranslationField
}

// TranslationField describes a single field to translate, identified by name
// and index. The result is applied via ApplyPlan rather than closures.
type TranslationField struct {
	FieldName string
	Index     int // -1 for scalar fields; >=0 for array elements (genres, actresses)
	Text      string
}

// TranslationResultMap maps each field key (FieldName or FieldName[idx]) to
// its translated text, enabling deterministic ApplyPlan without closures.
type TranslationResultMap map[string]string

// fieldKey returns the map key for a translation field.
func fieldKey(f TranslationField) string {
	if f.Index >= 0 {
		return fmt.Sprintf("%s[%d]", f.FieldName, f.Index)
	}
	return f.FieldName
}

// BuildTranslationPlan creates a TranslationPlan from the movie based on config.
// Fields are captured as data, not closures, making the plan inspectable and testable.
func (s *Service) BuildTranslationPlan(scraped *models.Movie, targetLang, sourceLang, sourceLabel string) TranslationPlan {
	fields := s.cfg.Fields
	var planFields []TranslationField

	queue := func(fieldName, text string, idx int) {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return
		}
		planFields = append(planFields, TranslationField{
			FieldName: fieldName,
			Index:     idx,
			Text:      trimmed,
		})
	}

	if fields.Title {
		queue("title", scraped.Title, -1)
	}
	if fields.OriginalTitle {
		queue("original_title", scraped.OriginalTitle, -1)
	}
	if fields.Description {
		queue("description", scraped.Description, -1)
	}
	if fields.Director {
		queue("director", scraped.Director, -1)
	}
	if fields.Maker {
		queue("maker", scraped.Maker, -1)
	}
	if fields.Label {
		queue("label", scraped.Label, -1)
	}
	if fields.Series {
		queue("series", scraped.Series, -1)
	}
	if fields.Genres {
		for i := range scraped.Genres {
			queue("genre", scraped.Genres[i].Name, i)
		}
	}
	if fields.Actresses {
		for i := range scraped.Actresses {
			name := actressDisplayTitle(scraped.Actresses[i])
			if strings.TrimSpace(name) == "" {
				continue
			}
			queue("actress", name, i)
		}
	}

	return TranslationPlan{
		TargetLang:     targetLang,
		SourceLang:     sourceLang,
		SourceLabel:    sourceLabel,
		ApplyToPrimary: s.cfg.ApplyToPrimary,
		Fields:         planFields,
	}
}

// ApplyPlan applies translated results back to the movie and builds translation records.
// It uses the plan's field descriptors (no closures) to route each result to the
// correct movie field and translation record.
func ApplyPlan(scraped *models.Movie, plan TranslationPlan, results TranslationResultMap, translatedRecord *models.MovieTranslation, state *translationState) string {
	var warnings []string
	for _, field := range plan.Fields {
		key := fieldKey(field)
		translated, ok := results[key]
		if !ok || strings.TrimSpace(translated) == "" {
			logging.Debugf("Translation: empty result for %s (original=%q), falling back to original", key, field.Text)
			warnings = append(warnings, fmt.Sprintf("%s: empty translation, kept original", key))
			translated = field.Text
		}

		// Route to the correct field on scraped movie and translation record
		applyTranslatedField(scraped, field, translated, translatedRecord, state, plan)
	}

	if len(warnings) > 0 {
		return strings.Join(warnings, "; ")
	}
	return ""
}

// applyTranslatedField routes a translated value to the correct movie field and
// translation record based on the field descriptor.
func applyTranslatedField(scraped *models.Movie, field TranslationField, translated string, translatedRecord *models.MovieTranslation, state *translationState, plan TranslationPlan) {
	if plan.SourceLabel != "" {
		translatedRecord.SourceName = plan.SourceLabel
	}

	switch field.FieldName {
	case "title":
		translatedRecord.Title = translated
		if plan.ApplyToPrimary {
			scraped.Title = translated
		}
	case "original_title":
		translatedRecord.OriginalTitle = translated
		if plan.ApplyToPrimary {
			scraped.OriginalTitle = translated
		}
	case "description":
		translatedRecord.Description = translated
		if plan.ApplyToPrimary {
			scraped.Description = translated
		}
	case "director":
		translatedRecord.Director = translated
		if plan.ApplyToPrimary {
			scraped.Director = translated
		}
	case "maker":
		translatedRecord.Maker = translated
		if plan.ApplyToPrimary {
			scraped.Maker = translated
		}
	case "label":
		translatedRecord.Label = translated
		if plan.ApplyToPrimary {
			scraped.Label = translated
		}
	case "series":
		translatedRecord.Series = translated
		if plan.ApplyToPrimary {
			scraped.Series = translated
		}
	case "genre":
		state.genreTranslations = append(state.genreTranslations, models.GenreTranslationData{
			GenreIndex: field.Index,
			Language:   plan.TargetLang,
			Name:       translated,
			SourceName: plan.SourceLabel,
		})
		if plan.ApplyToPrimary {
			scraped.Genres[field.Index].Name = translated
		}
	case "actress":
		first, last := models.SplitFullName(translated)
		jName := ""
		if strings.TrimSpace(scraped.Actresses[field.Index].JapaneseName) != "" || (strings.TrimSpace(scraped.Actresses[field.Index].FirstName) == "" && strings.TrimSpace(scraped.Actresses[field.Index].LastName) == "") {
			jName = translated
			first = ""
			last = ""
		}
		state.actressTranslations = append(state.actressTranslations, models.ActressTranslationData{
			ActressIndex: field.Index,
			Language:     plan.TargetLang,
			FirstName:    first,
			LastName:     last,
			JapaneseName: jName,
			DisplayName:  translated,
			SourceName:   plan.SourceLabel,
		})
		if plan.ApplyToPrimary {
			replaceActressName(&scraped.Actresses[field.Index], translated)
		}
	}
}

// GroupByProvider groups translation fields by their provider for batch dispatch.
// Currently all fields share the same provider, but this seam enables future
// per-field provider routing (e.g., DeepL for titles, LLM for descriptions).
func GroupByProvider(plan TranslationPlan, providerName string) map[string][]TranslationField {
	groups := make(map[string][]TranslationField)
	groups[providerName] = plan.Fields
	return groups
}

// translationState holds mutable state shared between field collection and result application.
type translationState struct {
	genreTranslations   []models.GenreTranslationData
	actressTranslations []models.ActressTranslationData
}

// TranslateMovie translates selected movie metadata fields from source to target language.
// It returns a TranslationOutput carrying the translated record and genre/actress
// translation data, rather than mutating *models.Movie in-place.
func (s *Service) TranslateMovie(ctx context.Context, scraped *models.Movie, settingsHash string) (*TranslationOutput, string, error) {
	if s == nil {
		return nil, "", fmt.Errorf("translation: TranslateMovie called on nil Service")
	}
	if scraped == nil || !s.cfg.Enabled {
		return (*TranslationOutput)(nil), "", nil
	}

	targetLang := normalizeLanguage(s.cfg.TargetLanguage)
	sourceLang := normalizeLanguage(s.cfg.SourceLanguage)
	if targetLang == "" {
		return (*TranslationOutput)(nil), "", fmt.Errorf("target language is required")
	}
	if sourceLang == "" {
		sourceLang = sourceLangAuto
	}

	if sourceLang != sourceLangAuto && sourceLang == targetLang {
		return (*TranslationOutput)(nil), "", nil
	}

	sourceLabel := "translation:" + normalizeProvider(s.cfg.Provider)
	translatedRecord := &models.MovieTranslation{
		Language:     targetLang,
		SourceName:   sourceLabel,
		SettingsHash: settingsHash,
	}

	// Build translation plan (data-only, no closures)
	plan := s.BuildTranslationPlan(scraped, targetLang, sourceLang, sourceLabel)

	if len(plan.Fields) == 0 {
		return &TranslationOutput{}, "", nil
	}

	// Dispatch batch translation
	texts := make([]string, 0, len(plan.Fields))
	for _, f := range plan.Fields {
		texts = append(texts, f.Text)
	}

	translatedTexts, err := s.translateTexts(ctx, sourceLang, targetLang, texts)
	if err != nil {
		logging.Debugf("Translation: translateTexts failed: %v", err)
		warning := sanitizeTranslationWarning(normalizeProvider(s.cfg.Provider), err)
		return nil, warning, err
	}
	if len(translatedTexts) != len(plan.Fields) {
		logging.Debugf("Translation: count mismatch - got %d, expected %d", len(translatedTexts), len(plan.Fields))
		return nil, "", fmt.Errorf("translation provider returned %d items for %d inputs", len(translatedTexts), len(plan.Fields))
	}

	// Build result map and apply
	results := make(TranslationResultMap, len(plan.Fields))
	for i, f := range plan.Fields {
		results[fieldKey(f)] = translatedTexts[i]
	}

	state := &translationState{}
	warningDetail := ApplyPlan(scraped, plan, results, translatedRecord, state)

	var warning string
	if warningDetail != "" {
		warning = fmt.Sprintf("Translation (%s): %s", normalizeProvider(s.cfg.Provider), warningDetail)
		logging.Warnf("Translation: %s", warning)
	}

	return &TranslationOutput{
		Movie:               translatedRecord,
		GenreTranslations:   state.genreTranslations,
		ActressTranslations: state.actressTranslations,
	}, warning, nil
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func sanitizeTranslationWarning(provider string, err error) string {
	var te *translationError
	if errors.As(err, &te) && te.Kind == TranslationErrorHTTPStatus {
		logging.Warnf("Translation (%s): HTTP %d error", provider, te.StatusCode)
		switch {
		case te.StatusCode == 429:
			return "Translation failed: rate limited, try again later"
		case te.StatusCode == 401:
			return "Translation failed: unauthorized, check API key"
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
	return strings.TrimSpace(strings.TrimSpace(actress.LastName) + " " + strings.TrimSpace(actress.FirstName))
}

// replaceActressName updates an actress's name fields with the translated string.
// The behavior depends on the current name state:
//   - If the actress has a non-empty JapaneseName, or both FirstName and LastName
//     are empty (single-token name), the translated string overwrites JapaneseName
//     and clears FirstName/LastName. This preserves the convention that Japanese-name
//     actresses store their display name in JapaneseName.
//   - Otherwise, the translated string is assigned to FirstName and LastName is cleared.
//     Multi-token names should be pre-split by the caller using models.SplitFullName.
//
// Note: When a name with a space is assigned to FirstName, it may confuse downstream
// display name construction that concatenates "LastName FirstName". Callers should
// pre-split using models.SplitFullName for multi-token translated names.
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

// translationResult holds translated texts and optional raw LLM output returned by a TranslatorProvider.
type translationResult struct {
	Texts  []string
	RawLLM string
}

func (s *Service) translateTexts(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	providerName := normalizeProvider(s.cfg.Provider)
	provider, ok := s.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("unsupported translation provider: %s", s.cfg.Provider)
	}
	return s.translateWithProvider(ctx, provider, sourceLang, targetLang, texts)
}

func (s *Service) translateWithProvider(ctx context.Context, provider TranslatorProvider, sourceLang, targetLang string, texts []string) ([]string, error) {
	var lastResult *translationResult
	var lastErr error
	expectedCount := len(texts)

	for attempt := 1; attempt <= maxTranslationRetries; attempt++ {
		result, err := provider.Translate(ctx, sourceLang, targetLang, texts)

		if err == nil {
			if result == nil {
				err = &translationError{Kind: TranslationErrorProvider, Message: "translation provider returned no result"}
			} else if len(result.Texts) != expectedCount {
				err = &translationError{
					Kind:    TranslationErrorCountMismatch,
					Message: fmt.Sprintf("translation provider returned %d items for %d inputs", len(result.Texts), expectedCount),
				}
			}
		}

		if err == nil && result != nil {
			return result.Texts, nil
		}

		lastResult = result
		lastErr = err

		if attempt < maxTranslationRetries {
			if isRetryableError(err, result) {
				logging.Debugf("Translation: attempt %d/%d failed (%v), retrying...", attempt, maxTranslationRetries, err)
				expBackoff := float64(time.Millisecond) * 100 * math.Pow(2, float64(attempt-1))
				if expBackoff > float64(2*time.Second) {
					expBackoff = float64(2 * time.Second)
				}
				s.randMu.Lock()
				sleep := time.Duration(s.rand.Float64() * expBackoff) //nolint:gosec // non-crypto rand is fine for retry jitter
				s.randMu.Unlock()
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(sleep):
				}
			} else {
				logging.Debugf("Translation: attempt %d/%d failed with non-retryable error (%v), giving up", attempt, maxTranslationRetries, err)
				break
			}
		}
	}

	if lastResult != nil && lastResult.RawLLM != "" {
		logging.Debugf("Translation: all %d attempts failed. Last LLM output (length=%d):\n%s", maxTranslationRetries, len(lastResult.RawLLM), lastResult.RawLLM)
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, &translationError{
		Kind:    TranslationErrorProvider,
		Message: fmt.Sprintf("translation failed after %d attempts", maxTranslationRetries),
	}
}

func isRetryableError(err error, result *translationResult) bool {
	if err == nil {
		return result != nil && len(result.Texts) == 0 && result.RawLLM != ""
	}

	var te *translationError
	if errors.As(err, &te) {
		switch te.Kind {
		case TranslationErrorCountMismatch, TranslationErrorParse:
			return result != nil && result.RawLLM != ""
		default:
			return false
		}
	}

	return false
}
