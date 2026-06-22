package translation

import (
	"context"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// OpenAIProvider translates text via the OpenAI chat completion API.
type OpenAIProvider struct {
	cfg        Config
	httpClient httpclient.HTTPClient
}

func NewOpenAIProvider(cfg Config, httpClient httpclient.HTTPClient) *OpenAIProvider {
	return &OpenAIProvider{cfg: cfg, httpClient: httpClient}
}

func (p *OpenAIProvider) Name() string { return "openai" }

func (p *OpenAIProvider) Translate(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
	if p == nil {
		return nil, fmt.Errorf("nil receiver: *OpenAIProvider")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(p.cfg.OpenAI.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	apiKey := strings.TrimSpace(p.cfg.OpenAI.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("openai api_key is required")
	}

	model := strings.TrimSpace(p.cfg.OpenAI.Model)
	if model == "" {
		model = "gpt-4o-mini"
	}

	if len(texts) == 0 {
		return &translationResult{}, nil
	}

	systemPrompt, userPrompt, err := buildLLMTranslationPrompts(sourceLang, targetLang, texts)
	if err != nil {
		return nil, err
	}

	adapter := &openAIChatAdapter{
		headers: map[string]string{
			"Authorization": "Bearer " + apiKey,
		},
	}
	return executeLLMChatTranslation(ctx, p.httpClient, adapter, "openai", baseURL, model, systemPrompt, userPrompt, len(texts))
}

// OpenAICompatibleProvider translates text via an OpenAI-compatible chat API
// (vLLM, Ollama, llama.cpp, etc.) with automatic thinking-strategy fallback.
type OpenAICompatibleProvider struct {
	cfg        Config
	httpClient httpclient.HTTPClient
}

func NewOpenAICompatibleProvider(cfg Config, httpClient httpclient.HTTPClient) *OpenAICompatibleProvider {
	return &OpenAICompatibleProvider{cfg: cfg, httpClient: httpClient}
}

func (p *OpenAICompatibleProvider) Name() string { return "openai-compatible" }

func (p *OpenAICompatibleProvider) Translate(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
	if p == nil {
		return nil, fmt.Errorf("nil receiver: *OpenAICompatibleProvider")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(p.cfg.OpenAICompatible.BaseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:11434/v1"
	}

	apiKey := strings.TrimSpace(p.cfg.OpenAICompatible.APIKey)
	model := strings.TrimSpace(p.cfg.OpenAICompatible.Model)
	if model == "" {
		return nil, fmt.Errorf("openai-compatible model is required")
	}

	if len(texts) == 0 {
		return &translationResult{}, nil
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

	thinkingEnabled := p.cfg.OpenAICompatible.EnableThinking
	strategies := buildOpenAICompatibleThinkingStrategies(baseURL, model, p.cfg.OpenAICompatible)

	var lastErr error
	for _, strategy := range strategies {
		request := applyOpenAICompatibleThinkingStrategy(baseRequest, strategy, thinkingEnabled)
		result, err := executeOpenAIChatTranslation(ctx, p.httpClient, openAIChatCallOptions{
			provider:  "openai-compatible",
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
