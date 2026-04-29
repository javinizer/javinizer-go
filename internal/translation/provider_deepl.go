package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

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
