package translation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/models"
)

const sourceLangAuto = "auto"

type deepLTranslateResponse struct {
	Translations []struct {
		Text string `json:"text"`
	} `json:"translations"`
}

type DeepLProvider struct {
	cfg        Config
	httpClient httpclient.HTTPClient
}

func NewDeepLProvider(cfg Config, httpClient httpclient.HTTPClient) *DeepLProvider {
	return &DeepLProvider{cfg: cfg, httpClient: httpClient}
}

func (p *DeepLProvider) Name() string { return "deepl" }

func (p *DeepLProvider) Translate(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
	if p == nil {
		return nil, fmt.Errorf("nil receiver: *DeepLProvider")
	}
	mode := models.DeepLMode(strings.ToLower(strings.TrimSpace(string(p.cfg.DeepL.Mode))))
	if mode == "" {
		mode = models.DeepLModeFree
	}

	baseURL := strings.TrimRight(strings.TrimSpace(p.cfg.DeepL.BaseURL), "/")
	if baseURL == "" {
		if mode == models.DeepLModePro {
			baseURL = "https://api.deepl.com"
		} else {
			baseURL = "https://api-free.deepl.com"
		}
	}

	apiKey := strings.TrimSpace(p.cfg.DeepL.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("deepl api_key is required")
	}

	if len(texts) == 0 {
		return &translationResult{}, nil
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

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &translationError{
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

	return &translationResult{Texts: result}, nil
}
