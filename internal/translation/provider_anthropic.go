package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/javinizer/javinizer-go/internal/httpclient"
)

type AnthropicProvider struct {
	cfg        Config
	httpClient httpclient.HTTPClient
}

func NewAnthropicProvider(cfg Config, httpClient httpclient.HTTPClient) *AnthropicProvider {
	return &AnthropicProvider{cfg: cfg, httpClient: httpClient}
}

func (p *AnthropicProvider) Name() string { return "anthropic" }

func (p *AnthropicProvider) Translate(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
	if p == nil {
		return nil, fmt.Errorf("nil receiver: *AnthropicProvider")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(p.cfg.Anthropic.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}

	apiKey := strings.TrimSpace(p.cfg.Anthropic.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("anthropic api_key is required")
	}

	model := strings.TrimSpace(p.cfg.Anthropic.Model)
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	if len(texts) == 0 {
		return &translationResult{}, nil
	}

	systemPrompt, userPrompt, err := buildLLMTranslationPrompts(sourceLang, targetLang, texts)
	if err != nil {
		return nil, err
	}

	adapter := &anthropicChatAdapter{apiKey: apiKey}
	return executeLLMChatTranslation(ctx, p.httpClient, adapter, "anthropic", baseURL, model, systemPrompt, userPrompt, len(texts))
}

// anthropicChatAdapter implements LLMChatAdapter for the Anthropic Messages API.
type anthropicChatAdapter struct {
	apiKey string
}

func (a *anthropicChatAdapter) BuildRequest(ctx context.Context, baseURL, model string, systemPrompt, userPrompt string, textCount int) (*http.Request, error) {
	type anthropicMessage struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	requestBody := map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"system":     systemPrompt,
		"messages":   []anthropicMessage{{Role: "user", Content: userPrompt}},
	}
	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (a *anthropicChatAdapter) DecodeResponse(providerName string, respBody []byte, textCount int) (*translationResult, error) {
	var decoded struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode %s response: %w", providerName, err)
	}
	if len(decoded.Content) == 0 {
		return nil, fmt.Errorf("%s response contained no content blocks", providerName)
	}
	return buildLLMTranslationResult(strings.TrimSpace(decoded.Content[0].Text), textCount)
}
