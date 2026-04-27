package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

	maxTranslationResponseSize = 10 * 1024 * 1024 // 10MB
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
		sourceLang = "auto"
	}

	if sourceLang != "auto" && sourceLang == targetLang {
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
		switch {
		case te.StatusCode == 429:
			return fmt.Sprintf("Translation failed (%s provider): rate limited (HTTP 429), try again later", provider)
		case te.StatusCode == 403:
			return fmt.Sprintf("Translation failed (%s provider): access denied (HTTP 403), check API key", provider)
		case te.StatusCode >= 500:
			return fmt.Sprintf("Translation failed (%s provider): service error (HTTP %d)", provider, te.StatusCode)
		case te.StatusCode >= 400:
			return fmt.Sprintf("Translation failed (%s provider): request error (HTTP %d)", provider, te.StatusCode)
		}
	}
	if errors.As(err, &te) {
		return fmt.Sprintf("Translation failed (%s provider): %s", provider, te.Kind)
	}
	return fmt.Sprintf("Translation failed (%s provider): internal error", provider)
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

	// Keep translated names visible in FullName() output for names that were
	// originally split into first/last name parts.
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
				time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
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

type openAIChatRequest struct {
	Model              string              `json:"model"`
	Temperature        float64             `json:"temperature"`
	Messages           []openAIChatMessage `json:"messages"`
	ChatTemplateKwargs map[string]any      `json:"chat_template_kwargs,omitempty"`
	ReasoningEffort    string              `json:"reasoning_effort,omitempty"`
	EnableThinking     *bool               `json:"enable_thinking,omitempty"`
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

type openAICompatibleThinkingStrategy string

const (
	openAICompatibleThinkingStrategyChatTemplateKwargs openAICompatibleThinkingStrategy = "chat_template_kwargs"
	openAICompatibleThinkingStrategyReasoningEffort    openAICompatibleThinkingStrategy = "reasoning_effort"
	openAICompatibleThinkingStrategyEnableThinking     openAICompatibleThinkingStrategy = "enable_thinking"
	openAICompatibleThinkingStrategyNone               openAICompatibleThinkingStrategy = "none"
)

type openAIChatCallOptions struct {
	provider  string
	baseURL   string
	endpoint  string
	model     string
	headers   map[string]string
	request   openAIChatRequest
	textCount int
	logInput  bool
	logTiming bool
}

func buildLLMTranslationPrompts(sourceLang, targetLang string, texts []string) (string, string, error) {
	systemPrompt := fmt.Sprintf("You are a translation engine. Translate each input item to the requested target language. Preserve order and return ONLY the indexed output markers in ascending order. Do not use JSON. Do not add commentary. Do not omit any index. Keep each translation on a single logical line; if needed, replace internal newlines with spaces. Source language: %s. Target language: %s.", sourceLang, targetLang)

	payloadBytes, err := json.Marshal(texts)
	if err != nil {
		return "", "", err
	}

	var userPrompt strings.Builder
	userPrompt.WriteString("Translate this JSON array of strings: ")
	userPrompt.Write(payloadBytes)
	userPrompt.WriteString("\nReturn output in this exact pattern:\n")
	for i := range texts {
		userPrompt.WriteString(translationCompactOutputMarker(i))
		userPrompt.WriteString("\ntranslated text\n")
	}

	return systemPrompt, strings.TrimSpace(userPrompt.String()), nil
}

func translationCompactOutputMarker(i int) string {
	return fmt.Sprintf("<<<JZ_%d>>>", i)
}

func buildOpenAICompatibleThinkingStrategies(baseURL, model string, cfg config.OpenAICompatibleTranslationConfig) []openAICompatibleThinkingStrategy {
	switch cfg.NormalizedBackendType() {
	case "vllm":
		return []openAICompatibleThinkingStrategy{
			openAICompatibleThinkingStrategyChatTemplateKwargs,
			openAICompatibleThinkingStrategyReasoningEffort,
			openAICompatibleThinkingStrategyEnableThinking,
			openAICompatibleThinkingStrategyNone,
		}
	case "ollama":
		return []openAICompatibleThinkingStrategy{
			openAICompatibleThinkingStrategyReasoningEffort,
			openAICompatibleThinkingStrategyChatTemplateKwargs,
			openAICompatibleThinkingStrategyEnableThinking,
			openAICompatibleThinkingStrategyNone,
		}
	case "llama.cpp":
		return []openAICompatibleThinkingStrategy{
			openAICompatibleThinkingStrategyEnableThinking,
			openAICompatibleThinkingStrategyChatTemplateKwargs,
			openAICompatibleThinkingStrategyReasoningEffort,
			openAICompatibleThinkingStrategyNone,
		}
	case "other":
		return []openAICompatibleThinkingStrategy{
			openAICompatibleThinkingStrategyChatTemplateKwargs,
			openAICompatibleThinkingStrategyReasoningEffort,
			openAICompatibleThinkingStrategyEnableThinking,
			openAICompatibleThinkingStrategyNone,
		}
	}

	switch {
	case looksLikeOllamaBaseURL(baseURL):
		return []openAICompatibleThinkingStrategy{
			openAICompatibleThinkingStrategyReasoningEffort,
			openAICompatibleThinkingStrategyChatTemplateKwargs,
			openAICompatibleThinkingStrategyEnableThinking,
			openAICompatibleThinkingStrategyNone,
		}
	case looksLikeLlamaCppBackend(baseURL, model):
		return []openAICompatibleThinkingStrategy{
			openAICompatibleThinkingStrategyEnableThinking,
			openAICompatibleThinkingStrategyChatTemplateKwargs,
			openAICompatibleThinkingStrategyReasoningEffort,
			openAICompatibleThinkingStrategyNone,
		}
	default:
		return []openAICompatibleThinkingStrategy{
			openAICompatibleThinkingStrategyChatTemplateKwargs,
			openAICompatibleThinkingStrategyReasoningEffort,
			openAICompatibleThinkingStrategyEnableThinking,
			openAICompatibleThinkingStrategyNone,
		}
	}
}

func looksLikeOllamaBaseURL(baseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return false
	}

	host := strings.ToLower(parsed.Host)
	return strings.Contains(host, "ollama") || strings.HasSuffix(host, ":11434")
}

func looksLikeLlamaCppBackend(baseURL, model string) bool {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err == nil {
		host := strings.ToLower(parsed.Host)
		path := strings.ToLower(parsed.Path)
		if strings.Contains(host, "llama") || strings.Contains(path, "llama") {
			return true
		}
	}

	model = strings.ToLower(strings.TrimSpace(model))
	return strings.Contains(model, ".gguf") || strings.Contains(model, "gguf")
}

func applyOpenAICompatibleThinkingStrategy(base openAIChatRequest, strategy openAICompatibleThinkingStrategy, enabled bool) openAIChatRequest {
	req := base
	req.ChatTemplateKwargs = nil
	req.ReasoningEffort = ""
	req.EnableThinking = nil

	switch strategy {
	case openAICompatibleThinkingStrategyChatTemplateKwargs:
		req.ChatTemplateKwargs = map[string]any{
			"enable_thinking": enabled,
			"thinking":        enabled,
		}
	case openAICompatibleThinkingStrategyReasoningEffort:
		if enabled {
			req.ReasoningEffort = "medium"
		} else {
			req.ReasoningEffort = "none"
		}
	case openAICompatibleThinkingStrategyEnableThinking:
		req.EnableThinking = &enabled
	}

	return req
}

func isRetryableThinkingStrategyError(err error) bool {
	if err == nil {
		return false
	}

	var te *TranslationError
	if errors.As(err, &te) && te.Kind == TranslationErrorHTTPStatus {
		return te.StatusCode == 400 || te.StatusCode == 422
	}
	return false
}

func buildLLMTranslationResult(content string, textCount int) (*translationResult, error) {
	parsed, err := parseLLMTranslationPayload(content, textCount)
	if err != nil {
		return &translationResult{rawLLM: content}, &TranslationError{
			Kind:    TranslationErrorParse,
			Message: err.Error(),
		}
	}
	return &translationResult{texts: parsed, rawLLM: content}, nil
}

func decodeOpenAIChatTranslation(provider string, respBody []byte, textCount int) (*translationResult, error) {
	var decoded openAIChatResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode %s response: %w", provider, err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("%s response contained no choices", provider)
	}

	return buildLLMTranslationResult(extractContentString(decoded.Choices[0].Message.Content), textCount)
}

func (s *Service) executeOpenAIChatTranslation(ctx context.Context, opts openAIChatCallOptions) (*translationResult, error) {
	body, err := json.Marshal(opts.request)
	if err != nil {
		return nil, err
	}

	url := opts.baseURL + opts.endpoint
	logging.Debugf("Translation (%s): POST %s model=%s texts=%d", opts.provider, url, opts.model, opts.textCount)
	logging.Debugf("Translation (%s): system prompt: %s", opts.provider, opts.request.Messages[0].Content)
	if opts.logInput && len(opts.request.Messages) > 1 {
		logging.Debugf("Translation (%s): input: %s", opts.provider, opts.request.Messages[1].Content)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, value := range opts.headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Time{}
	if opts.logTiming {
		logging.Debugf("Translation (%s): sending request...", opts.provider)
		start = time.Now()
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		if opts.logTiming {
			return nil, fmt.Errorf("%s request failed after %v: %w", opts.provider, time.Since(start), err)
		}
		return nil, err
	}
	if opts.logTiming {
		logging.Debugf("Translation (%s): response received in %v (status %d)", opts.provider, time.Since(start), resp.StatusCode)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &TranslationError{
			Kind:       TranslationErrorHTTPStatus,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("%s translation failed with status %d: %s", opts.provider, resp.StatusCode, string(respBody)),
		}
	}

	logging.Debugf("Translation (%s): response: %s", opts.provider, string(respBody))
	return decodeOpenAIChatTranslation(opts.provider, respBody, opts.textCount)
}

func (s *Service) translateWithOpenAI(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
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

	systemPrompt, userPrompt, err := buildLLMTranslationPrompts(sourceLang, targetLang, texts)
	if err != nil {
		return nil, err
	}

	return s.executeOpenAIChatTranslation(ctx, openAIChatCallOptions{
		provider: providerOpenAI,
		baseURL:  baseURL,
		endpoint: "/chat/completions",
		model:    model,
		headers: map[string]string{
			"Authorization": "Bearer " + apiKey,
		},
		request: openAIChatRequest{
			Model:       model,
			Temperature: 0,
			Messages: []openAIChatMessage{
				{Role: "system", Content: systemPrompt},
				{Role: "user", Content: userPrompt},
			},
		},
		textCount: len(texts),
	})
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

func (s *Service) translateWithOpenAICompatible(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.OpenAICompatible.BaseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}

	apiKey := strings.TrimSpace(s.cfg.OpenAICompatible.APIKey)
	model := strings.TrimSpace(s.cfg.OpenAICompatible.Model)
	if model == "" {
		return nil, fmt.Errorf("openai-compatible model is required")
	}

	systemPrompt, userPrompt, err := buildLLMTranslationPrompts(sourceLang, targetLang, texts)
	if err != nil {
		return nil, err
	}

	headers := map[string]string{}
	if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
	}

	baseRequest := openAIChatRequest{
		Model:       model,
		Temperature: 0,
		Messages: []openAIChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
	}

	thinkingEnabled := s.cfg.OpenAICompatible.EffectiveEnableThinking()
	strategies := buildOpenAICompatibleThinkingStrategies(baseURL, model, s.cfg.OpenAICompatible)

	var lastErr error
	for _, strategy := range strategies {
		request := applyOpenAICompatibleThinkingStrategy(baseRequest, strategy, thinkingEnabled)
		result, err := s.executeOpenAIChatTranslation(ctx, openAIChatCallOptions{
			provider:  providerOpenAICompatible,
			baseURL:   baseURL,
			endpoint:  "/chat/completions",
			model:     model,
			headers:   headers,
			request:   request,
			textCount: len(texts),
			logInput:  true,
			logTiming: true,
		})
		if err == nil {
			return result, nil
		}

		lastErr = err
		if strategy == openAICompatibleThinkingStrategyNone || !isRetryableThinkingStrategyError(err) {
			return nil, err
		}

		logging.Debugf("Translation (openai-compatible): thinking strategy %q failed (%v), trying fallback", strategy, err)
	}

	return nil, lastErr
}

func (s *Service) translateWithAnthropic(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
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

	systemPrompt, userPrompt, err := buildLLMTranslationPrompts(sourceLang, targetLang, texts)
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
		"messages":   []anthropicMessage{{Role: "user", Content: userPrompt}},
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

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &TranslationError{
			Kind:       TranslationErrorHTTPStatus,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("anthropic translation failed with status %d: %s", resp.StatusCode, string(respBody)),
		}
	}

	logging.Debugf("Translation (anthropic): response: %s", string(respBody))

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

	return buildLLMTranslationResult(strings.TrimSpace(decoded.Content[0].Text), len(texts))
}

type deepLTranslateResponse struct {
	Translations []struct {
		Text string `json:"text"`
	} `json:"translations"`
}

func (s *Service) translateWithDeepL(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
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

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &TranslationError{
			Kind:       TranslationErrorHTTPStatus,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("deepl translation failed with status %d: %s", resp.StatusCode, string(respBody)),
		}
	}

	var decoded deepLTranslateResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode deepl response: %w", err)
	}

	result := make([]string, 0, len(decoded.Translations))
	for _, item := range decoded.Translations {
		result = append(result, item.Text)
	}

	return &translationResult{texts: result}, nil
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

func (s *Service) translateWithGoogle(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
	mode := strings.ToLower(strings.TrimSpace(s.cfg.Google.Mode))
	if mode == "" {
		mode = "free"
	}

	if mode == "paid" {
		return s.translateWithGooglePaid(ctx, sourceLang, targetLang, texts)
	}
	return s.translateWithGoogleFree(ctx, sourceLang, targetLang, texts)
}

func (s *Service) translateWithGooglePaid(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
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

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &TranslationError{
			Kind:       TranslationErrorHTTPStatus,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("google paid translation failed with status %d: %s", resp.StatusCode, string(respBody)),
		}
	}

	var decoded googlePaidTranslateResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode google paid response: %w", err)
	}

	result := make([]string, 0, len(decoded.Data.Translations))
	for _, item := range decoded.Data.Translations {
		result = append(result, html.UnescapeString(item.TranslatedText))
	}

	return &translationResult{texts: result}, nil
}

// googleFreeResult holds the result of a single translation
type googleFreeResult struct {
	index int
	text  string
	err   error
}

func (s *Service) translateWithGoogleFree(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
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

	return &translationResult{texts: result}, nil
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

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if readErr != nil {
		return googleFreeResult{err: readErr}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logging.Debugf("Translation (google-free): HTTP %d for text=%q body=%s", resp.StatusCode, text[:min(100, len(text))], string(respBody[:min(200, len(respBody))]))
		return googleFreeResult{err: &TranslationError{
			Kind:       TranslationErrorHTTPStatus,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("google free translation failed with status %d: %s", resp.StatusCode, string(respBody)),
		}}
	}

	translated, err := parseGoogleFreeResponse(respBody)
	if err != nil {
		logging.Debugf("Translation (google-free): parse error for text=%q: %v (body=%s)", text[:min(100, len(text))], err, string(respBody[:min(200, len(respBody))]))
		return googleFreeResult{err: err}
	}
	if translated == "" {
		logging.Debugf("Translation (google-free): empty result for text=%q (body=%s)", text[:min(100, len(text))], string(respBody[:min(200, len(respBody))]))
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

func normalizeTranslationPayload(payload string) string {
	cleaned := strings.TrimSpace(payload)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	return strings.TrimSpace(cleaned)
}

func parseLLMTranslationPayload(payload string, expectedCount int) ([]string, error) {
	cleaned := normalizeTranslationPayload(payload)
	if expectedCount > 0 && strings.Contains(cleaned, translationCompactOutputMarker(0)) {
		parsed, err := parseCompactTranslationPayload(cleaned, expectedCount)
		if err != nil {
			return nil, err
		}
		logging.Debugf("Translation: parseLLMTranslationPayload parsed %d compact tagged items", len(parsed))
		return parsed, nil
	}
	return parseStringArrayPayload(cleaned)
}

func parseStringArrayPayload(payload string) ([]string, error) {
	cleaned := normalizeTranslationPayload(payload)

	logging.Debugf("Translation: parseStringArrayPayload input length=%d, first 200 chars: %s", len(cleaned), cleaned[:min(200, len(cleaned))])

	if result, err := unmarshalStringArray(cleaned); err == nil {
		logging.Debugf("Translation: parseStringArrayPayload direct unmarshal successful (%d items)", len(result))
		return result, nil
	}

	start := strings.IndexByte(cleaned, '[')
	end := strings.LastIndexByte(cleaned, ']')
	if start >= 0 && end > start {
		candidate := strings.TrimSpace(cleaned[start : end+1])
		if candidate != cleaned {
			if result, err := unmarshalStringArray(candidate); err == nil {
				logging.Debugf("Translation: parseStringArrayPayload extracted JSON array from wrapped content (%d items)", len(result))
				return result, nil
			}
		}
	}

	return nil, fmt.Errorf("failed to parse translated output payload as JSON string array")
}

func parseCompactTranslationPayload(payload string, expectedCount int) ([]string, error) {
	pos := 0
	out := make([]string, 0, expectedCount)

	for i := 0; i < expectedCount; i++ {
		startToken := translationCompactOutputMarker(i)
		start := strings.Index(payload[pos:], startToken)
		if start < 0 {
			return nil, fmt.Errorf("failed to parse compact translation payload: missing output marker %d", i)
		}
		start += pos + len(startToken)

		end := len(payload)
		if i+1 < expectedCount {
			nextToken := translationCompactOutputMarker(i + 1)
			next := strings.Index(payload[start:], nextToken)
			if next < 0 {
				return nil, fmt.Errorf("failed to parse compact translation payload: missing output marker %d", i+1)
			}
			end = start + next
		}

		out = append(out, strings.TrimSpace(payload[start:end]))
		pos = end
	}

	return out, nil
}

func unmarshalStringArray(payload string) ([]string, error) {
	var result []string
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		return nil, err
	}
	return result, nil
}
