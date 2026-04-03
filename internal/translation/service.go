package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

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
)

// Service translates aggregated movie metadata using a configured provider.
type Service struct {
	cfg        config.TranslationConfig
	httpClient *http.Client
}

// New creates a new translation service for the provided config.
func New(cfg config.TranslationConfig) *Service {
	return &Service{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 0, // Use context timeout at call-site
		},
	}
}

// TranslateMovie translates configured textual fields and optionally applies them
// to primary movie fields. It returns a target-language translation record when
// translation work was performed. The settingsHash parameter identifies which
// translation settings were used to generate this translation.
func (s *Service) TranslateMovie(ctx context.Context, movie *models.Movie, settingsHash string) (*models.MovieTranslation, error) {
	if s == nil || movie == nil || !s.cfg.Enabled {
		return nil, nil
	}

	targetLang := normalizeLanguage(s.cfg.TargetLanguage)
	sourceLang := normalizeLanguage(s.cfg.SourceLanguage)
	if targetLang == "" {
		return nil, fmt.Errorf("target language is required")
	}
	if sourceLang == "" {
		sourceLang = "auto"
	}

	if sourceLang != "auto" && sourceLang == targetLang {
		return nil, nil
	}

	type pendingText struct {
		text  string
		apply func(string)
	}

	requests := make([]pendingText, 0)
	translatedRecord := &models.MovieTranslation{
		Language:     targetLang,
		SourceName:   "translation:" + normalizeProvider(s.cfg.Provider),
		SettingsHash: settingsHash,
	}

	queueField := func(raw string, assignRecord func(string), assignMovie func(string)) {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return
		}
		requests = append(requests, pendingText{
			text: trimmed,
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
		queueField(movie.Title, func(v string) { translatedRecord.Title = v }, func(v string) { movie.Title = v })
	}
	if fields.OriginalTitle {
		queueField(movie.OriginalTitle, func(v string) { translatedRecord.OriginalTitle = v }, func(v string) { movie.OriginalTitle = v })
	}
	if fields.Description {
		queueField(movie.Description, func(v string) { translatedRecord.Description = v }, func(v string) { movie.Description = v })
	}
	if fields.Director {
		queueField(movie.Director, func(v string) { translatedRecord.Director = v }, func(v string) { movie.Director = v })
	}
	if fields.Maker {
		queueField(movie.Maker, func(v string) { translatedRecord.Maker = v }, func(v string) { movie.Maker = v })
	}
	if fields.Label {
		queueField(movie.Label, func(v string) { translatedRecord.Label = v }, func(v string) { movie.Label = v })
	}
	if fields.Series {
		queueField(movie.Series, func(v string) { translatedRecord.Series = v }, func(v string) { movie.Series = v })
	}
	if fields.Genres {
		for i := range movie.Genres {
			idx := i
			queueField(movie.Genres[idx].Name, func(string) {}, func(v string) {
				movie.Genres[idx].Name = v
			})
		}
	}
	if fields.Actresses {
		for i := range movie.Actresses {
			idx := i
			name := actressDisplayName(movie.Actresses[idx])
			if strings.TrimSpace(name) == "" {
				continue
			}
			queueField(name, func(string) {}, func(v string) {
				replaceActressName(&movie.Actresses[idx], v)
			})
		}
	}

	if len(requests) == 0 {
		return nil, nil
	}

	texts := make([]string, 0, len(requests))
	for _, req := range requests {
		texts = append(texts, req.text)
	}

	translatedTexts, err := s.translateTexts(ctx, sourceLang, targetLang, texts)
	if err != nil {
		return nil, err
	}
	if len(translatedTexts) != len(requests) {
		return nil, fmt.Errorf("translation provider returned %d items for %d inputs", len(translatedTexts), len(requests))
	}

	for i := range requests {
		translated := strings.TrimSpace(translatedTexts[i])
		if translated == "" {
			translated = requests[i].text
		}
		requests[i].apply(translated)
	}

	return translatedRecord, nil
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func normalizeLanguage(language string) string {
	return strings.ToLower(strings.TrimSpace(language))
}

func actressDisplayName(actress models.Actress) string {
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

	// Keep translated names visible in FullName() output for names that were
	// originally split into first/last name parts.
	actress.FirstName = translated
	actress.LastName = ""
}

func (s *Service) translateTexts(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	provider := normalizeProvider(s.cfg.Provider)
	switch provider {
	case providerOpenAI:
		return s.translateWithOpenAI(ctx, sourceLang, targetLang, texts)
	case providerDeepL:
		return s.translateWithDeepL(ctx, sourceLang, targetLang, texts)
	case providerGoogle:
		return s.translateWithGoogle(ctx, sourceLang, targetLang, texts)
	case providerOpenAICompatible:
		return s.translateWithOpenAICompatible(ctx, sourceLang, targetLang, texts)
	case providerAnthropic:
		return s.translateWithAnthropic(ctx, sourceLang, targetLang, texts)
	default:
		return nil, fmt.Errorf("unsupported translation provider: %s", provider)
	}
}

type openAIChatRequest struct {
	Model              string              `json:"model"`
	Temperature        float64             `json:"temperature"`
	Messages           []openAIChatMessage `json:"messages"`
	ChatTemplateKwargs map[string]any      `json:"chat_template_kwargs,omitempty"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (s *Service) translateWithOpenAI(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.OpenAI.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	apiKey := strings.TrimSpace(s.cfg.OpenAI.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("openai api_key is required")
	}

	model := strings.TrimSpace(s.cfg.OpenAI.Model)
	if model == "" {
		model = "gpt-4o-mini"
	}

	systemPrompt := fmt.Sprintf("You are a translation engine. Translate each input item to the requested target language. Preserve order and return ONLY a single valid JSON array of translated strings. Do NOT split into multiple arrays. Do NOT add any text outside the JSON array. Source language: %s. Target language: %s.", sourceLang, targetLang)

	payloadBytes, err := json.Marshal(texts)
	if err != nil {
		return nil, err
	}

	requestBody := openAIChatRequest{
		Model:       model,
		Temperature: 0,
		Messages: []openAIChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: "Translate this JSON array of strings: " + string(payloadBytes)},
		},
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	logging.Debugf("Translation (openai): POST %s model=%s texts=%d", baseURL+"/chat/completions", model, len(texts))
	logging.Debugf("Translation (openai): system prompt: %s", systemPrompt)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai translation failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var decoded openAIChatResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode openai response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("openai response contained no choices")
	}

	content := extractContentString(decoded.Choices[0].Message.Content)
	return parseStringArrayPayload(content)
}

func extractContentString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return string(raw)
}

func (s *Service) translateWithOpenAICompatible(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.OpenAICompatible.BaseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}

	apiKey := strings.TrimSpace(s.cfg.OpenAICompatible.APIKey)
	model := strings.TrimSpace(s.cfg.OpenAICompatible.Model)
	if model == "" {
		return nil, fmt.Errorf("openai-compatible model is required")
	}

	systemPrompt := fmt.Sprintf("You are a translation engine. Translate each input item to the requested target language. Preserve order and return ONLY a single valid JSON array of translated strings. Do NOT split into multiple arrays. Do NOT add any text outside the JSON array. Source language: %s. Target language: %s.", sourceLang, targetLang)

	payloadBytes, err := json.Marshal(texts)
	if err != nil {
		return nil, err
	}

	requestBody := openAIChatRequest{
		Model:       model,
		Temperature: 0,
		Messages: []openAIChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: "Translate this JSON array of strings: " + string(payloadBytes)},
		},
		ChatTemplateKwargs: map[string]any{"enable_thinking": false},
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	logging.Debugf("Translation (openai-compatible): POST %s model=%s texts=%d", baseURL+"/chat/completions", model, len(texts))
	logging.Debugf("Translation (openai-compatible): system prompt: %s", systemPrompt)
	logging.Debugf("Translation (openai-compatible): input: %s", string(payloadBytes))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	logging.Debugf("Translation (openai-compatible): sending request...")
	start := time.Now()
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai-compatible request failed after %v: %w", time.Since(start), err)
	}
	logging.Debugf("Translation (openai-compatible): response received in %v (status %d)", time.Since(start), resp.StatusCode)
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai-compatible translation failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	logging.Debugf("Translation (openai-compatible): response: %s", string(respBody))

	var decoded openAIChatResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode openai-compatible response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("openai-compatible response contained no choices")
	}

	content := extractContentString(decoded.Choices[0].Message.Content)
	return parseStringArrayPayload(content)
}

func (s *Service) translateWithAnthropic(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.Anthropic.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	apiKey := strings.TrimSpace(s.cfg.Anthropic.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic api_key is required")
	}

	model := strings.TrimSpace(s.cfg.Anthropic.Model)
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	systemPrompt := fmt.Sprintf("You are a translation engine. Translate each input item to the requested target language. Preserve order and return ONLY a single valid JSON array of translated strings. Do NOT split into multiple arrays. Do NOT add any text outside the JSON array. Source language: %s. Target language: %s.", sourceLang, targetLang)

	payloadBytes, err := json.Marshal(texts)
	if err != nil {
		return nil, err
	}

	type anthropicMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	requestBody := map[string]interface{}{
		"model":      model,
		"max_tokens": 4096,
		"system":     systemPrompt,
		"messages":   []anthropicMessage{{Role: "user", Content: "Translate this JSON array of strings: " + string(payloadBytes)}},
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	logging.Debugf("Translation (anthropic): POST %s model=%s texts=%d", baseURL+"/v1/messages", model, len(texts))
	logging.Debugf("Translation (anthropic): system prompt: %s", systemPrompt)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("content-type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("anthropic translation failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var decoded struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode anthropic response: %w", err)
	}
	if len(decoded.Content) == 0 {
		return nil, fmt.Errorf("anthropic response contained no content blocks")
	}

	content := strings.TrimSpace(decoded.Content[0].Text)
	return parseStringArrayPayload(content)
}

type deepLTranslateResponse struct {
	Translations []struct {
		Text string `json:"text"`
	} `json:"translations"`
}

func (s *Service) translateWithDeepL(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	mode := strings.ToLower(strings.TrimSpace(s.cfg.DeepL.Mode))
	if mode == "" {
		mode = "free"
	}

	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.DeepL.BaseURL), "/")
	if baseURL == "" {
		if mode == "pro" {
			baseURL = "https://api.deepl.com"
		} else {
			baseURL = "https://api-free.deepl.com"
		}
	}

	apiKey := strings.TrimSpace(s.cfg.DeepL.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("deepl api_key is required")
	}

	type deepLRequest struct {
		Text       []string `json:"text"`
		TargetLang string   `json:"target_lang"`
		SourceLang string   `json:"source_lang,omitempty"`
	}

	reqBody := deepLRequest{
		Text:       texts,
		TargetLang: strings.ToUpper(targetLang),
	}
	if sourceLang != "" && sourceLang != "auto" {
		reqBody.SourceLang = strings.ToUpper(sourceLang)
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v2/translate", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "DeepL-Auth-Key "+apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("deepl translation failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var decoded deepLTranslateResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode deepl response: %w", err)
	}

	result := make([]string, 0, len(decoded.Translations))
	for _, item := range decoded.Translations {
		result = append(result, item.Text)
	}

	return result, nil
}

type googlePaidTranslateRequest struct {
	Q      []string `json:"q"`
	Target string   `json:"target"`
	Source string   `json:"source,omitempty"`
	Format string   `json:"format"`
}

type googlePaidTranslateResponse struct {
	Data struct {
		Translations []struct {
			TranslatedText string `json:"translatedText"`
		} `json:"translations"`
	} `json:"data"`
}

func (s *Service) translateWithGoogle(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	mode := strings.ToLower(strings.TrimSpace(s.cfg.Google.Mode))
	if mode == "" {
		mode = "free"
	}

	if mode == "paid" {
		return s.translateWithGooglePaid(ctx, sourceLang, targetLang, texts)
	}
	return s.translateWithGoogleFree(ctx, sourceLang, targetLang, texts)
}

func (s *Service) translateWithGooglePaid(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	apiKey := strings.TrimSpace(s.cfg.Google.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("google api_key is required for paid mode")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.Google.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://translation.googleapis.com"
	}

	requestBody := googlePaidTranslateRequest{
		Q:      texts,
		Target: targetLang,
		Format: "text",
	}
	if sourceLang != "" && sourceLang != "auto" {
		requestBody.Source = sourceLang
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	translateURL := baseURL + "/language/translate/v2?key=" + url.QueryEscape(apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, translateURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("google paid translation failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var decoded googlePaidTranslateResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode google paid response: %w", err)
	}

	result := make([]string, 0, len(decoded.Data.Translations))
	for _, item := range decoded.Data.Translations {
		result = append(result, html.UnescapeString(item.TranslatedText))
	}

	return result, nil
}

// googleFreeResult holds the result of a single translation
type googleFreeResult struct {
	index int
	text  string
	err   error
}

func (s *Service) translateWithGoogleFree(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.Google.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://translate.googleapis.com"
	}

	sl := sourceLang
	if sl == "" {
		sl = "auto"
	}

	// Use errgroup to manage concurrent goroutines with a semaphore for bounded parallelism
	// max 5 concurrent requests to avoid overwhelming the API
	maxWorkers := 5
	if len(texts) < maxWorkers {
		maxWorkers = len(texts)
	}

	eg, egCtx := errgroup.WithContext(ctx)
	sem := semaphore.NewWeighted(int64(maxWorkers))

	// Result slice to collect translation results by index
	results := make([]googleFreeResult, len(texts))

	// Track if any goroutine has seen an error
	var firstErr error
	var errOnce sync.Once

	// Launch goroutines for each text to translate
	for i, text := range texts {
		// Check context before starting each goroutine
		select {
		case <-ctx.Done():
			// Context was canceled externally, stop launching new goroutines
			// Wait for remaining goroutines to complete
			_ = eg.Wait()
			return nil, ctx.Err()
		default:
		}

		// Acquire semaphore slot before launching goroutine
		if err := sem.Acquire(egCtx, 1); err != nil {
			// Context was canceled, stop launching new goroutines
			_ = eg.Wait()
			break
		}

		i := i // capture loop variable for closure
		text := text

		eg.Go(func() error {
			defer sem.Release(1)
			result := s.performGoogleFreeTranslation(egCtx, baseURL, sl, targetLang, text)
			results[i] = googleFreeResult{
				index: i,
				text:  result.text,
				err:   result.err,
			}
			// Track first error without canceling other goroutines
			if result.err != nil {
				errOnce.Do(func() {
					firstErr = result.err
				})
			}
			// Don't return error to avoid canceling other goroutines
			// This ensures all requests complete and we get all results
			return nil
		})
	}

	// Wait for all goroutines to complete
	if err := eg.Wait(); err != nil {
		return nil, err
	}

	// If any request failed, return the first error
	if firstErr != nil {
		return nil, firstErr
	}

	// Extract results in original order
	result := make([]string, len(texts))
	for i, r := range results {
		result[i] = r.text
	}

	return result, nil
}

func (s *Service) performGoogleFreeTranslation(ctx context.Context, baseURL, sourceLang, targetLang, text string) googleFreeResult {
	u, err := url.Parse(baseURL + "/translate_a/single")
	if err != nil {
		return googleFreeResult{err: err}
	}
	query := u.Query()
	query.Set("client", "gtx")
	query.Set("sl", sourceLang)
	query.Set("tl", targetLang)
	query.Set("dt", "t")
	query.Set("q", text)
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return googleFreeResult{err: err}
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return googleFreeResult{err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return googleFreeResult{err: readErr}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return googleFreeResult{err: fmt.Errorf("google free translation failed with status %d: %s", resp.StatusCode, string(respBody))}
	}

	translated, err := parseGoogleFreeResponse(respBody)
	if err != nil {
		return googleFreeResult{err: err}
	}
	return googleFreeResult{text: translated}
}

func parseGoogleFreeResponse(payload []byte) (string, error) {
	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return "", fmt.Errorf("failed to decode google free response: %w", err)
	}

	root, ok := decoded.([]any)
	if !ok || len(root) == 0 {
		return "", fmt.Errorf("unexpected google free response shape")
	}
	segments, ok := root[0].([]any)
	if !ok {
		return "", fmt.Errorf("unexpected google free translation payload")
	}

	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		segmentArray, ok := segment.([]any)
		if !ok || len(segmentArray) == 0 {
			continue
		}
		piece, ok := segmentArray[0].(string)
		if !ok {
			continue
		}
		parts = append(parts, piece)
	}

	if len(parts) == 0 {
		return "", fmt.Errorf("google free translation returned empty text")
	}

	return strings.Join(parts, ""), nil
}

func parseStringArrayPayload(payload string) ([]string, error) {
	cleaned := strings.TrimSpace(payload)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	logging.Debugf("Translation: parseStringArrayPayload input length=%d, first 200 chars: %s", len(cleaned), cleaned[:min(200, len(cleaned))])

	var out []string
	pos := 0
	arrayNum := 0
	for pos < len(cleaned) {
		start := strings.Index(cleaned[pos:], "[")
		if start < 0 {
			break
		}
		start += pos

		if start+1 < len(cleaned) {
			nextChar := cleaned[start+1]
			if nextChar != '"' && nextChar != ' ' && nextChar != '\n' && nextChar != '\t' && nextChar != '[' {
				pos = start + 1
				continue
			}
		}

		depth := 0
		end := -1
		inString := false
		escaped := false
		for i := start; i < len(cleaned); i++ {
			ch := cleaned[i]
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' && inString {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = !inString
				continue
			}
			if inString {
				continue
			}
			if ch == '[' {
				depth++
			} else if ch == ']' {
				depth--
				if depth == 0 {
					end = i
					break
				}
			}
		}

		if end < 0 {
			logging.Debugf("Translation: parseStringArrayPayload array %d unclosed, stopping at pos=%d", arrayNum+1, pos)
			break
		}

		jsonStr := cleaned[start : end+1]
		if len(jsonStr) > 2 && jsonStr[1] == '[' {
			logging.Debugf("Translation: parseStringArrayPayload array %d is nested, skipping", arrayNum+1)
			pos = end + 1
			arrayNum++
			continue
		}

		arrayNum++
		var arr []string
		if err := json.Unmarshal([]byte(jsonStr), &arr); err != nil {
			logging.Debugf("Translation: parseStringArrayPayload array %d unmarshal failed: %v", arrayNum, err)
			pos = end + 1
			continue
		}
		logging.Debugf("Translation: parseStringArrayPayload array %d has %d items", arrayNum, len(arr))
		out = append(out, arr...)
		pos = end + 1
	}

	logging.Debugf("Translation: parseStringArrayPayload found %d arrays, total %d items", arrayNum, len(out))

	if len(out) == 0 {
		if arrayNum == 0 {
			out = extractQuotedStrings(cleaned)
			if len(out) > 0 {
				logging.Debugf("Translation: parseStringArrayPayload extracted %d strings via fallback", len(out))
				return out, nil
			}
		}
		return nil, fmt.Errorf("failed to parse translated output payload: no valid JSON arrays found")
	}
	return out, nil
}

func extractQuotedStrings(s string) []string {
	var results []string
	inString := false
	escaped := false
	var current strings.Builder

	for i := 0; i < len(s); i++ {
		ch := s[i]
		if escaped {
			escaped = false
			if inString {
				current.WriteByte(ch)
			}
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			if inString {
				results = append(results, current.String())
				current.Reset()
			}
			inString = !inString
			continue
		}
		if inString {
			current.WriteByte(ch)
		}
	}

	return results
}
