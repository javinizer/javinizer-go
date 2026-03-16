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

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

const (
	providerOpenAI = "openai"
	providerDeepL  = "deepl"
	providerGoogle = "google"
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
// translation work was performed.
func (s *Service) TranslateMovie(ctx context.Context, movie *models.Movie) (*models.MovieTranslation, error) {
	if s == nil || movie == nil || !s.cfg.Enabled {
		return nil, nil
	}

	targetLang := normalizeLanguage(s.cfg.TargetLanguage)
	sourceLang := normalizeLanguage(s.cfg.SourceLanguage)
	if targetLang == "" {
		return nil, fmt.Errorf("target language is required")
	}
	if sourceLang == "" {
		sourceLang = "auto"
	}

	if sourceLang != "auto" && sourceLang == targetLang {
		return nil, nil
	}

	type pendingText struct {
		text  string
		apply func(string)
	}

	requests := make([]pendingText, 0)
	translatedRecord := &models.MovieTranslation{
		Language:   targetLang,
		SourceName: "translation:" + normalizeProvider(s.cfg.Provider),
	}

	queueField := func(raw string, assignRecord func(string), assignMovie func(string)) {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return
		}
		requests = append(requests, pendingText{
			text: trimmed,
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
		queueField(movie.Title, func(v string) { translatedRecord.Title = v }, func(v string) { movie.Title = v })
	}
	if fields.OriginalTitle {
		queueField(movie.OriginalTitle, func(v string) { translatedRecord.OriginalTitle = v }, func(v string) { movie.OriginalTitle = v })
	}
	if fields.Description {
		queueField(movie.Description, func(v string) { translatedRecord.Description = v }, func(v string) { movie.Description = v })
	}
	if fields.Director {
		queueField(movie.Director, func(v string) { translatedRecord.Director = v }, func(v string) { movie.Director = v })
	}
	if fields.Maker {
		queueField(movie.Maker, func(v string) { translatedRecord.Maker = v }, func(v string) { movie.Maker = v })
	}
	if fields.Label {
		queueField(movie.Label, func(v string) { translatedRecord.Label = v }, func(v string) { movie.Label = v })
	}
	if fields.Series {
		queueField(movie.Series, func(v string) { translatedRecord.Series = v }, func(v string) { movie.Series = v })
	}
	if fields.Genres {
		for i := range movie.Genres {
			idx := i
			queueField(movie.Genres[idx].Name, func(string) {}, func(v string) {
				movie.Genres[idx].Name = v
			})
		}
	}
	if fields.Actresses {
		for i := range movie.Actresses {
			idx := i
			name := actressDisplayName(movie.Actresses[idx])
			if strings.TrimSpace(name) == "" {
				continue
			}
			queueField(name, func(string) {}, func(v string) {
				replaceActressName(&movie.Actresses[idx], v)
			})
		}
	}

	if len(requests) == 0 {
		return nil, nil
	}

	texts := make([]string, 0, len(requests))
	for _, req := range requests {
		texts = append(texts, req.text)
	}

	translatedTexts, err := s.translateTexts(ctx, sourceLang, targetLang, texts)
	if err != nil {
		return nil, err
	}
	if len(translatedTexts) != len(requests) {
		return nil, fmt.Errorf("translation provider returned %d items for %d inputs", len(translatedTexts), len(requests))
	}

	for i := range requests {
		translated := strings.TrimSpace(translatedTexts[i])
		if translated == "" {
			translated = requests[i].text
		}
		requests[i].apply(translated)
	}

	return translatedRecord, nil
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func normalizeLanguage(language string) string {
	return strings.ToLower(strings.TrimSpace(language))
}

func actressDisplayName(actress models.Actress) string {
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

func (s *Service) translateTexts(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	provider := normalizeProvider(s.cfg.Provider)
	switch provider {
	case providerOpenAI:
		return s.translateWithOpenAI(ctx, sourceLang, targetLang, texts)
	case providerDeepL:
		return s.translateWithDeepL(ctx, sourceLang, targetLang, texts)
	case providerGoogle:
		return s.translateWithGoogle(ctx, sourceLang, targetLang, texts)
	default:
		return nil, fmt.Errorf("unsupported translation provider: %s", provider)
	}
}

type openAIChatRequest struct {
	Model       string              `json:"model"`
	Temperature float64             `json:"temperature"`
	Messages    []openAIChatMessage `json:"messages"`
}

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIChatMessage `json:"message"`
	} `json:"choices"`
}

func (s *Service) translateWithOpenAI(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
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

	systemPrompt := "You are a translation engine. Translate each input item to the requested target language. Preserve order and return ONLY a valid JSON array of translated strings."
	if sourceLang != "auto" {
		systemPrompt += " Source language: " + sourceLang + "."
	}
	systemPrompt += " Target language: " + targetLang + "."

	payloadBytes, err := json.Marshal(texts)
	if err != nil {
		return nil, err
	}

	requestBody := openAIChatRequest{
		Model:       model,
		Temperature: 0,
		Messages: []openAIChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: "Translate this JSON array of strings: " + string(payloadBytes)},
		},
	}

	body, err := json.Marshal(requestBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai translation failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var decoded openAIChatResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode openai response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return nil, fmt.Errorf("openai response contained no choices")
	}

	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	return parseStringArrayPayload(content)
}

type deepLTranslateResponse struct {
	Translations []struct {
		Text string `json:"text"`
	} `json:"translations"`
}

func (s *Service) translateWithDeepL(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
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

	form := url.Values{}
	form.Set("auth_key", apiKey)
	form.Set("target_lang", strings.ToUpper(targetLang))
	if sourceLang != "" && sourceLang != "auto" {
		form.Set("source_lang", strings.ToUpper(sourceLang))
	}
	for _, text := range texts {
		form.Add("text", text)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v2/translate", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("deepl translation failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var decoded deepLTranslateResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode deepl response: %w", err)
	}

	result := make([]string, 0, len(decoded.Translations))
	for _, item := range decoded.Translations {
		result = append(result, item.Text)
	}

	return result, nil
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

func (s *Service) translateWithGoogle(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	mode := strings.ToLower(strings.TrimSpace(s.cfg.Google.Mode))
	if mode == "" {
		mode = "free"
	}

	if mode == "paid" {
		return s.translateWithGooglePaid(ctx, sourceLang, targetLang, texts)
	}
	return s.translateWithGoogleFree(ctx, sourceLang, targetLang, texts)
}

func (s *Service) translateWithGooglePaid(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
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

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("google paid translation failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var decoded googlePaidTranslateResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("failed to decode google paid response: %w", err)
	}

	result := make([]string, 0, len(decoded.Data.Translations))
	for _, item := range decoded.Data.Translations {
		result = append(result, html.UnescapeString(item.TranslatedText))
	}

	return result, nil
}

func (s *Service) translateWithGoogleFree(ctx context.Context, sourceLang, targetLang string, texts []string) ([]string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.Google.BaseURL), "/")
	if baseURL == "" {
		baseURL = "https://translate.googleapis.com"
	}

	result := make([]string, 0, len(texts))
	sl := sourceLang
	if sl == "" {
		sl = "auto"
	}

	for _, text := range texts {
		u, err := url.Parse(baseURL + "/translate_a/single")
		if err != nil {
			return nil, err
		}
		query := u.Query()
		query.Set("client", "gtx")
		query.Set("sl", sl)
		query.Set("tl", targetLang)
		query.Set("dt", "t")
		query.Set("q", text)
		u.RawQuery = query.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}

		resp, err := s.httpClient.Do(req)
		if err != nil {
			return nil, err
		}

		respBody, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("google free translation failed with status %d: %s", resp.StatusCode, string(respBody))
		}

		translated, err := parseGoogleFreeResponse(respBody)
		if err != nil {
			return nil, err
		}
		result = append(result, translated)
	}

	return result, nil
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

func parseStringArrayPayload(payload string) ([]string, error) {
	cleaned := strings.TrimSpace(payload)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	start := strings.Index(cleaned, "[")
	end := strings.LastIndex(cleaned, "]")
	if start >= 0 && end > start {
		cleaned = cleaned[start : end+1]
	}

	var out []string
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return nil, fmt.Errorf("failed to parse translated output payload: %w", err)
	}
	return out, nil
}
