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
	"github.com/javinizer/javinizer-go/internal/logging"
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
	// Normalize before comparing the auto sentinel so "AUTO" / " auto " / padded
	// variants are treated as auto-detect and not sent as source_lang.
	normalizedSourceLang := strings.ToLower(strings.TrimSpace(sourceLang))
	if normalizedSourceLang != "" && normalizedSourceLang != sourceLangAuto {
		reqBody.SourceLang = strings.ToUpper(normalizedSourceLang)
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return nil, &translationError{Kind: TranslationErrorProvider, Message: "deepl translation: failed to marshal request"}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v2/translate", bytes.NewReader(jsonBody))
	if err != nil {
		return nil, &translationError{Kind: TranslationErrorProvider, Message: "deepl translation: failed to build request"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "DeepL-Auth-Key "+apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		// Wrap raw transport errors so the request URL/headers (which carry the
		// API key) are not leaked through logs/errors.
		logging.Debugf("deepl translation request failed: %v", err)
		return nil, &translationError{Kind: TranslationErrorProvider, Message: "deepl translation request failed"}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if err != nil {
		logging.Debugf("deepl translation response read failed: %v", err)
		return nil, &translationError{Kind: TranslationErrorProvider, Message: "deepl translation response read failed"}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Status-only message: do not embed the provider response body, which can
		// contain diagnostics.
		return nil, &translationError{
			Kind:       TranslationErrorHTTPStatus,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("deepl translation failed with status %d", resp.StatusCode),
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
