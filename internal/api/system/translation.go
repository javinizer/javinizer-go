package system

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type DeepLUsageResponse struct {
	CharacterCount int64  `json:"character_count"`
	CharacterLimit int64  `json:"character_limit"`
	StartTime      string `json:"start_time,omitempty"`
	EndTime        string `json:"end_time,omitempty"`
	APIKeyCount    int64  `json:"api_key_character_count,omitempty"`
	APIKeyLimit    int64  `json:"api_key_character_limit,omitempty"`
}

type DeepLUsageRequest struct {
	Mode    string `json:"mode"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

// getTranslationModels godoc
// @Summary Get translation models
// @Description Fetch available models from an OpenAI-compatible base URL
// @Tags system
// @Accept json
// @Produce json
// @Param request body TranslationModelsRequest true "Translation model lookup request"
// @Success 200 {object} TranslationModelsResponse
// @Failure 400 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
// @Router /api/v1/translation/models [post]
func getTranslationModels(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req TranslationModelsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: "Invalid request format"})
			return
		}

		provider := strings.ToLower(strings.TrimSpace(req.Provider))
		if provider != "openai" {
			c.JSON(400, ErrorResponse{Error: "Only provider=openai is supported for model discovery"})
			return
		}

		baseURL := strings.TrimSpace(req.BaseURL)
		if !isValidHTTPURL(baseURL) {
			c.JSON(400, ErrorResponse{Error: "base_url must be a valid http(s) URL"})
			return
		}
		if strings.TrimSpace(req.APIKey) == "" {
			c.JSON(400, ErrorResponse{Error: "api_key is required for model discovery"})
			return
		}

		models, err := fetchOpenAICompatibleModels(c.Request.Context(), baseURL, req.APIKey)
		if err != nil {
			c.JSON(http.StatusBadGateway, ErrorResponse{Error: "Failed to fetch models: " + err.Error()})
			return
		}

		c.JSON(200, TranslationModelsResponse{Models: models})
	}
}

func fetchOpenAICompatibleModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/models"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	var decoded struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("invalid upstream response payload")
	}

	modelSet := make(map[string]struct{})
	for _, item := range decoded.Data {
		model := strings.TrimSpace(item.ID)
		if model == "" {
			continue
		}
		modelSet[model] = struct{}{}
	}

	models := make([]string, 0, len(modelSet))
	for model := range modelSet {
		models = append(models, model)
	}
	sort.Strings(models)
	if len(models) == 0 {
		return nil, fmt.Errorf("no models found")
	}

	return models, nil
}

// getDeepLUsage godoc
// @Summary Get DeepL usage information
// @Description Fetch current character usage and limits from DeepL API
// @Tags system
// @Accept json
// @Produce json
// @Param request body DeepLUsageRequest true "DeepL usage lookup request"
// @Success 200 {object} DeepLUsageResponse
// @Failure 400 {object} ErrorResponse
// @Failure 502 {object} ErrorResponse
// @Router /api/v1/translation/deepl/usage [post]
func getDeepLUsage(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req DeepLUsageRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, ErrorResponse{Error: "Invalid request format"})
			return
		}

		mode := strings.ToLower(strings.TrimSpace(req.Mode))
		if mode == "" {
			mode = "free"
		}
		if mode != "free" && mode != "pro" {
			c.JSON(400, ErrorResponse{Error: "mode must be either 'free' or 'pro'"})
			return
		}

		baseURL := strings.TrimRight(strings.TrimSpace(req.BaseURL), "/")
		if baseURL == "" {
			if mode == "pro" {
				baseURL = "https://api.deepl.com"
			} else {
				baseURL = "https://api-free.deepl.com"
			}
		}

		apiKey := strings.TrimSpace(req.APIKey)
		if apiKey == "" {
			c.JSON(400, ErrorResponse{Error: "api_key is required"})
			return
		}

		usage, err := fetchDeepLUsage(c.Request.Context(), baseURL, apiKey)
		if err != nil {
			c.JSON(http.StatusBadGateway, ErrorResponse{Error: "Failed to fetch usage: " + err.Error()})
			return
		}

		c.JSON(200, usage)
	}
}

func fetchDeepLUsage(ctx context.Context, baseURL, apiKey string) (*DeepLUsageResponse, error) {
	endpoint := baseURL + "/v2/usage"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "DeepL-Auth-Key "+strings.TrimSpace(apiKey))
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("deepl returned status %d: %s", resp.StatusCode, string(body))
	}

	var decoded DeepLUsageResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("invalid deepl usage response: %w", err)
	}

	return &decoded, nil
}
