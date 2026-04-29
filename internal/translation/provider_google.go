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

	"github.com/javinizer/javinizer-go/internal/logging"
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
			result := s.performGoogleFreeTranslation(egCtx, baseURL, sl, targetLang, text)
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
