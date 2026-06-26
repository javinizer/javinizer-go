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

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/semaphore"

	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

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

type GoogleProvider struct {
	cfg        Config
	httpClient httpclient.HTTPClient
}

func NewGoogleProvider(cfg Config, httpClient httpclient.HTTPClient) *GoogleProvider {
	return &GoogleProvider{cfg: cfg, httpClient: httpClient}
}

func (p *GoogleProvider) Name() string { return "google" }

func (p *GoogleProvider) Translate(ctx context.Context, sourceLang, targetLang string, texts []string) (*translationResult, error) {
	if p == nil {
		return nil, fmt.Errorf("nil receiver: *GoogleProvider")
	}
	httpClient := p.httpClient
	cfg := p.cfg

	mode := models.GoogleMode(strings.ToLower(strings.TrimSpace(string(cfg.Google.Mode))))
	if mode == "" {
		mode = models.GoogleModeFree
	}

	if mode == models.GoogleModePaid {
		return translateWithGooglePaid(ctx, httpClient, cfg, sourceLang, targetLang, texts)
	}
	return translateWithGoogleFree(ctx, httpClient, cfg, sourceLang, targetLang, texts)
}

func translateWithGooglePaid(ctx context.Context, httpClient httpclient.HTTPClient, cfg Config, sourceLang, targetLang string, texts []string) (*translationResult, error) {
	apiKey := strings.TrimSpace(cfg.Google.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("google api_key is required for paid mode")
	}

	if len(texts) == 0 {
		return &translationResult{}, nil
	}

	baseURL := strings.TrimRight(strings.TrimSpace(cfg.Google.BaseURL), "/")
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

	resp, err := httpClient.Do(req)
	if err != nil {
		// Wrap raw transport errors as a typed provider error so the request URL
		// (which carries the API key in paid mode) is not leaked through logs/errors.
		logging.Debugf("google paid translation request failed: %v", err)
		return nil, &translationError{
			Kind:    TranslationErrorProvider,
			Message: "google paid translation request failed",
		}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if err != nil {
		logging.Debugf("google paid translation response read failed: %v", err)
		return nil, &translationError{
			Kind:    TranslationErrorProvider,
			Message: "google paid translation response read failed",
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Status-only message: do not embed the provider response body, which can
		// contain diagnostics or echo the translated text.
		return nil, &translationError{
			Kind:       TranslationErrorHTTPStatus,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("google paid translation failed with status %d", resp.StatusCode),
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

	return &translationResult{Texts: result}, nil
}

type googleFreeResult struct {
	index int
	text  string
	err   error
}

func translateWithGoogleFree(ctx context.Context, httpClient httpclient.HTTPClient, cfg Config, sourceLang, targetLang string, texts []string) (*translationResult, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.Google.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://translate.googleapis.com"
	}

	if len(texts) == 0 {
		return &translationResult{}, nil
	}

	sl := sourceLang
	if sl == "" {
		sl = "auto"
	}

	maxWorkers := 5
	if len(texts) < maxWorkers {
		maxWorkers = len(texts)
	}

	eg, egCtx := errgroup.WithContext(ctx)
	sem := semaphore.NewWeighted(int64(maxWorkers))

	results := make([]googleFreeResult, len(texts))

	var firstErr error
	var errOnce sync.Once

	for i, text := range texts {
		select {
		case <-ctx.Done():
			_ = eg.Wait()
			return nil, ctx.Err()
		default:
		}

		if err := sem.Acquire(egCtx, 1); err != nil {
			_ = eg.Wait()
			break
		}

		i := i
		text := text

		eg.Go(func() error {
			defer sem.Release(1)
			result := performGoogleFreeTranslation(egCtx, httpClient, baseURL, sl, targetLang, text)
			results[i] = googleFreeResult{
				index: i,
				text:  result.text,
				err:   result.err,
			}
			if result.err != nil {
				errOnce.Do(func() {
					firstErr = result.err
				})
			}
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	if firstErr != nil {
		return nil, firstErr
	}

	result := make([]string, len(texts))
	for i, r := range results {
		result[i] = r.text
	}

	return &translationResult{Texts: result}, nil
}

func performGoogleFreeTranslation(ctx context.Context, httpClient httpclient.HTTPClient, baseURL, sourceLang, targetLang, text string) googleFreeResult {
	u, err := url.Parse(baseURL + "/translate_a/single")
	if err != nil {
		return googleFreeResult{err: &translationError{Kind: TranslationErrorProvider, Message: "google free translation: invalid base URL"}}
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
		return googleFreeResult{err: &translationError{Kind: TranslationErrorProvider, Message: "google free translation: failed to build request"}}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		// Wrap raw transport errors so the request URL (which carries the q= text)
		// is not leaked through logs/errors.
		logging.Debugf("google free translation request failed: %v", err)
		return googleFreeResult{err: &translationError{Kind: TranslationErrorProvider, Message: "google free translation request failed"}}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxTranslationResponseSize))
	if readErr != nil {
		logging.Debugf("google free translation response read failed: %v", readErr)
		return googleFreeResult{err: &translationError{Kind: TranslationErrorProvider, Message: "google free translation response read failed"}}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logging.Debugf("Translation (google-free): HTTP %d (text length=%d, body length=%d)", resp.StatusCode, len(text), len(respBody))
		return googleFreeResult{err: &translationError{
			Kind:       TranslationErrorHTTPStatus,
			StatusCode: resp.StatusCode,
			Message:    fmt.Sprintf("google free translation failed with status %d", resp.StatusCode),
		}}
	}

	translated, err := parseGoogleFreeResponse(respBody)
	if err != nil {
		logging.Debugf("Translation (google-free): parse error: %v (text length=%d, body length=%d)", err, len(text), len(respBody))
		return googleFreeResult{err: err}
	}
	if translated == "" {
		logging.Debugf("Translation (google-free): empty result (text length=%d, body length=%d)", len(text), len(respBody))
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
