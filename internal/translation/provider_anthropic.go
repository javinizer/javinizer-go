package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/javinizer/javinizer-go/internal/logging"
)

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
