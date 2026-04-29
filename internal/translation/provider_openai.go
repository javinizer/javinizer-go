package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
)

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
