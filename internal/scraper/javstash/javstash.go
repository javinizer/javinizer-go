package javstash

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/httpclient"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

const (
	defaultBaseURL = "https://javstash.org/graphql"
)

type Scraper struct {
	client      *resty.Client
	enabled     bool
	apiKey      string
	baseURL     string
	language    string
	rateLimiter *ratelimit.Limiter
	settings    config.ScraperSettings
}

type GraphQLRequest struct {
	Query         string                 `json:"query"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
	OperationName string                 `json:"operationName,omitempty"`
}

type GraphQLResponse struct {
	Data struct {
		SearchScene []Scene `json:"searchScene"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors,omitempty"`
}

type Scene struct {
	ID          string      `json:"id"`
	Code        string      `json:"code"`
	Title       string      `json:"title"`
	ReleaseDate string      `json:"release_date"`
	Duration    int         `json:"duration"`
	Director    string      `json:"director"`
	Details     string      `json:"details"`
	Studio      *Studio     `json:"studio"`
	Performers  []Performer `json:"performers"`
	Tags        []Tag       `json:"tags"`
	Images      []Image     `json:"images"`
	URLs        []URL       `json:"urls"`
}

type Studio struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Performer struct {
	Performer struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"performer"`
}

type Tag struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Image struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

type URL struct {
	URL string `json:"url"`
}

func New(settings config.ScraperSettings, globalProxy *config.ProxyConfig, globalFlareSolverr config.FlareSolverrConfig) *Scraper {
	apiKey := ""
	if v, ok := settings.Extra["api_key"].(string); ok {
		apiKey = strings.TrimSpace(v)
	}
	if apiKey == "" {
		apiKey = os.Getenv("JAVSTASH_API_KEY")
	}

	baseURL := strings.TrimSpace(settings.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	scraperCfg := &config.ScraperSettings{
		Enabled:         settings.Enabled,
		Timeout:         settings.Timeout,
		RateLimit:       settings.RateLimit,
		RetryCount:      settings.RetryCount,
		UserAgent:       settings.UserAgent,
		Proxy:           settings.Proxy,
		UseFlareSolverr: settings.UseFlareSolverr,
		UseBrowser:      settings.UseBrowser,
	}

	client, err := NewHTTPClient(scraperCfg, globalProxy, globalFlareSolverr)
	if err != nil {
		logging.Errorf("Javstash: Failed to create HTTP client: %v, using fallback", err)
		client = httpclient.NewRestyClientNoProxy(time.Duration(settings.Timeout)*time.Second, settings.RetryCount)
	}

	lang := scraperutil.NormalizeLanguage(settings.Language)

	s := &Scraper{
		client:      client,
		enabled:     settings.Enabled,
		apiKey:      apiKey,
		baseURL:     baseURL,
		language:    lang,
		rateLimiter: ratelimit.NewLimiter(time.Duration(settings.RateLimit) * time.Millisecond),
		settings:    settings,
	}

	if settings.RateLimit > 0 {
		logging.Infof("Javstash: Rate limiting enabled with %dms delay between requests", settings.RateLimit)
	}

	return s
}

func (s *Scraper) Name() string {
	return "javstash"
}

func (s *Scraper) IsEnabled() bool {
	return s.enabled
}

func (s *Scraper) Config() *config.ScraperSettings {
	return s.settings.DeepCopy()
}

func (s *Scraper) Close() error {
	return nil
}

func (s *Scraper) ValidateConfig(cfg *config.ScraperSettings) error {
	if cfg == nil {
		return fmt.Errorf("javstash: config is nil")
	}
	if !cfg.Enabled {
		return nil
	}

	apiKey := ""
	if v, ok := cfg.Extra["api_key"].(string); ok {
		apiKey = strings.TrimSpace(v)
	}
	if apiKey == "" {
		apiKey = os.Getenv("JAVSTASH_API_KEY")
	}
	if apiKey == "" {
		return fmt.Errorf("javstash: api_key is required (set in config or JAVSTASH_API_KEY env var)")
	}

	if cfg.RateLimit < 0 {
		return fmt.Errorf("javstash: rate_limit must be non-negative, got %d", cfg.RateLimit)
	}
	if cfg.RetryCount < 0 {
		return fmt.Errorf("javstash: retry_count must be non-negative, got %d", cfg.RetryCount)
	}
	if cfg.Timeout < 0 {
		return fmt.Errorf("javstash: timeout must be non-negative, got %d", cfg.Timeout)
	}
	return nil
}

func (s *Scraper) CanHandleURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "javstash.org" || strings.HasSuffix(host, ".javstash.org")
}

func (s *Scraper) ExtractIDFromURL(urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}
	path := strings.Trim(u.Path, "/")
	parts := strings.Split(path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" && len(parts[i]) > 3 {
			return parts[i], nil
		}
	}
	return "", fmt.Errorf("failed to extract ID from JavStash URL")
}

func (s *Scraper) ScrapeURL(ctx context.Context, rawURL string) (*models.ScraperResult, error) {
	return nil, models.NewScraperNotFoundError("Javstash", "JavStash is a GraphQL-based API and does not support direct URL scraping. Use ID-based search instead.")
}

func (s *Scraper) GetURL(id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("javstash: id cannot be empty or whitespace")
	}
	return fmt.Sprintf("%s/scenes/%s", strings.TrimSuffix(s.baseURL, "/graphql"), id), nil
}

func (s *Scraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("javstash: id cannot be empty or whitespace")
	}
	if s.apiKey == "" {
		return nil, fmt.Errorf("javstash: api_key is required (set in config or JAVSTASH_API_KEY env var)")
	}

	if err := s.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("javstash: rate limit wait failed: %w", err)
	}

	searchTerm := strings.TrimSpace(id)

	query := `query searchScene($term: String!, $limit: Int) {
		searchScene(term: $term, limit: $limit) {
			id
			code
			title
			release_date
			duration
			director
			details
			studio { id name }
			performers { performer { id name } }
			tags { id name }
			images { id url }
			urls { url }
		}
	}`

	req := GraphQLRequest{
		Query: query,
		Variables: map[string]interface{}{
			"term":  searchTerm,
			"limit": 5,
		},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("javstash: failed to marshal request: %w", err)
	}

	resp, err := s.client.R().
		SetHeader("Content-Type", "application/json").
		SetHeader("ApiKey", s.apiKey).
		SetBody(body).
		Post(s.baseURL)

	if err != nil {
		return nil, fmt.Errorf("javstash: request failed: %w", err)
	}

	if resp.StatusCode() == http.StatusUnauthorized {
		return nil, models.NewScraperNotFoundError("Javstash", "invalid API key")
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, models.NewScraperStatusError("Javstash", resp.StatusCode(),
			fmt.Sprintf("Javstash returned status %d", resp.StatusCode()))
	}

	var graphQLResp GraphQLResponse
	if err := json.Unmarshal(resp.Body(), &graphQLResp); err != nil {
		return nil, fmt.Errorf("javstash: failed to parse response: %w", err)
	}

	if len(graphQLResp.Errors) > 0 {
		if strings.Contains(graphQLResp.Errors[0].Message, "Not authorized") {
			return nil, models.NewScraperNotFoundError("Javstash", "API key required for search")
		}
		return nil, fmt.Errorf("javstash: GraphQL error: %s", graphQLResp.Errors[0].Message)
	}

	if len(graphQLResp.Data.SearchScene) == 0 {
		return nil, models.NewScraperNotFoundError("Javstash", fmt.Sprintf("no results for %s", id))
	}

	return s.parseScene(&graphQLResp.Data.SearchScene[0], searchTerm)
}

func (s *Scraper) parseScene(scene *Scene, searchID string) (*models.ScraperResult, error) {
	result := &models.ScraperResult{
		Source:      s.Name(),
		SourceURL:   fmt.Sprintf("%s/scenes/%s", strings.TrimSuffix(s.baseURL, "/graphql"), scene.ID),
		Language:    s.language,
		ID:          searchID,
		ContentID:   scene.Code,
		Title:       scraperutil.CleanString(scene.Title),
		Description: scraperutil.CleanString(scene.Details),
		Runtime:     int(math.Round(float64(scene.Duration) / 60.0)),
		Director:    scraperutil.CleanString(scene.Director),
	}

	if result.ContentID == "" {
		result.ContentID = searchID
	}

	if scene.ReleaseDate != "" {
		t, err := time.Parse("2006-01-02", scene.ReleaseDate)
		if err == nil {
			result.ReleaseDate = &t
		}
	}

	if scene.Studio != nil {
		result.Maker = scraperutil.CleanString(scene.Studio.Name)
	}

	result.Actresses = make([]models.ActressInfo, 0, len(scene.Performers))
	for _, p := range scene.Performers {
		result.Actresses = append(result.Actresses, models.ActressInfo{
			JapaneseName: scraperutil.CleanString(p.Performer.Name),
		})
	}

	result.Genres = make([]string, 0, len(scene.Tags))
	for _, tag := range scene.Tags {
		result.Genres = append(result.Genres, scraperutil.CleanString(tag.Name))
	}

	if len(scene.Images) > 0 {
		for _, img := range scene.Images {
			if strings.Contains(strings.ToLower(img.URL), "poster") ||
				strings.Contains(strings.ToLower(img.URL), "cover") {
				result.PosterURL = img.URL
				result.CoverURL = img.URL
				break
			}
		}
		if result.PosterURL == "" && len(scene.Images) > 0 {
			result.PosterURL = scene.Images[0].URL
			result.CoverURL = scene.Images[0].URL
		}
	}

	if len(scene.URLs) > 0 {
		for _, u := range scene.URLs {
			if strings.Contains(u.URL, "dmm.co.jp") && strings.Contains(u.URL, "cid=") {
				if extracted := extractDMMContentID(u.URL); extracted != "" {
					result.ContentID = extracted
					result.SourceURL = u.URL
					break
				}
			}
		}
		if result.SourceURL == "" && len(scene.URLs) > 0 {
			result.SourceURL = scene.URLs[0].URL
		}
	}

	return result, nil
}

func extractDMMContentID(url string) string {
	idx := strings.Index(url, "cid=")
	if idx == -1 {
		return ""
	}
	start := idx + 4
	end := strings.IndexAny(url[start:], "/&?")
	if end == -1 {
		return url[start:]
	}
	return url[start : start+end]
}
