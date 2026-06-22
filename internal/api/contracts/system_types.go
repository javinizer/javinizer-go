package contracts

import "github.com/javinizer/javinizer-go/internal/models"

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string   `json:"status" example:"ok"`
	Scrapers  []string `json:"scrapers" example:"r18dev,dmm"`
	Version   string   `json:"version" example:"v1.2.3"`
	Commit    string   `json:"commit" example:"abc123def456"`
	BuildDate string   `json:"build_date" example:"2026-02-23T00:00:00Z"`
}

// AuthStatusResponse represents authentication state for first-run/login gating.
type AuthStatusResponse struct {
	Initialized   bool   `json:"initialized" example:"true"`
	Authenticated bool   `json:"authenticated" example:"false"`
	Username      string `json:"username,omitempty" example:"admin"`
}

// AuthCredentialsRequest represents username/password login/setup payload.
type AuthCredentialsRequest struct {
	Username   string `json:"username" binding:"required" example:"admin"`
	Password   string `json:"password" binding:"required" example:"your-password"`
	RememberMe bool   `json:"remember_me,omitempty" example:"true"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error  string   `json:"error" example:"Movie not found"`
	Errors []string `json:"errors,omitempty"`
}

// ProxyTestRequest represents a proxy connectivity test request.
type ProxyTestRequest struct {
	Mode         string                    `json:"mode" binding:"required,oneof=direct flaresolverr"` // direct or flaresolverr
	Proxy        models.ProxyConfig        `json:"proxy"`
	FlareSolverr models.FlareSolverrConfig `json:"flaresolverr"`         // FlareSolverr config (separate from ProxyConfig)
	TargetURL    string                    `json:"target_url,omitempty"` // Optional override target URL
}

// ProxyTestResponse represents proxy connectivity test results.
type ProxyTestResponse struct {
	Success           bool   `json:"success"`
	Mode              string `json:"mode"`
	TargetURL         string `json:"target_url"`
	StatusCode        int    `json:"status_code,omitempty"`
	DurationMS        int64  `json:"duration_ms"`
	Message           string `json:"message"`
	ProxyURL          string `json:"proxy_url,omitempty"`          // Redacted proxy URL
	FlareSolverrURL   string `json:"flaresolverr_url,omitempty"`   // FlareSolverr endpoint used
	VerificationToken string `json:"verification_token,omitempty"` // Token for save authorization
	TokenExpiresAt    int64  `json:"token_expires_at,omitempty"`   // Unix timestamp when token expires
}

type TranslationModelsRequest struct {
	Provider string `json:"provider" binding:"required"` // openai (OpenAI-compatible only for now)
	BaseURL  string `json:"base_url" binding:"required"` // API base URL (e.g., https://api.openai.com/v1)
	APIKey   string `json:"api_key,omitempty"`           // Provider API key
}

// TranslationModelsResponse represents the model discovery response.
type TranslationModelsResponse struct {
	Models []string `json:"models"`
}

// ScraperOption is an alias for models.ScraperOption
type ScraperOption = models.ScraperOption

// ScraperChoice is an alias for models.ScraperChoice
type ScraperChoice = models.ScraperChoice

// ScraperInfo represents information about a scraper
type ScraperInfo struct {
	Name         string          `json:"name" example:"r18dev"`
	DisplayTitle string          `json:"display_title" example:"R18.dev"`
	Enabled      bool            `json:"enabled" example:"true"`
	Options      []ScraperOption `json:"options,omitempty"`
}

// AvailableScrapersResponse represents the list of available scrapers
type AvailableScrapersResponse struct {
	Scrapers []ScraperInfo `json:"scrapers"`
}
