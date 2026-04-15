package config

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/javinizer/javinizer-go/internal/types"
	"gopkg.in/yaml.v3"
)

// UserAgentString is a custom User-Agent string type that marshals/unmarshals as a plain string.
// Type definition moved to configutil for Phase 1 refactoring.
type UserAgentString struct {
	Value string
}

// Permission constants are centralized to ensure consistency across the codebase
const (
	// CurrentConfigVersion tracks compatibility breakpoints for on-disk config.
	// Do not bump for additive/default-only fields; those are handled by loading
	// into DefaultConfig() and idempotent normalization rules.
	CurrentConfigVersion = 3

	// DirPermConfig is the permission mode for configuration directories (owner + group read/execute)
	DirPermConfig = 0755
	// DirPermTemp is the permission mode for temporary/sensitive directories (owner-only access)
	DirPermTemp = 0700
	// FilePermConfig is the permission mode for configuration files
	FilePermConfig = 0644

	// DefaultUserAgent is the true/identifying UA for Javinizer.
	DefaultUserAgent = "Javinizer (+https://github.com/javinizer/Javinizer)"

	// DefaultFakeUserAgent is a browser-like UA for scraper-hostile sites.
	// Used as fallback when scraper UserAgent is not set.
	DefaultFakeUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"

	// DefaultTempDir is the default temporary directory for transient files (e.g., temp posters).
	// This constant is referenced by both config defaults and database migrations to ensure consistency.
	DefaultTempDir = "data/temp"
)

// Config represents the application configuration
type Config struct {
	ConfigVersion int               `yaml:"config_version" json:"config_version"`
	Server        ServerConfig      `yaml:"server" json:"server"`
	API           APIConfig         `yaml:"api" json:"api"`
	System        SystemConfig      `yaml:"system" json:"system"`
	Scrapers      ScrapersConfig    `yaml:"scrapers" json:"scrapers"`
	Metadata      MetadataConfig    `yaml:"metadata" json:"metadata"`
	Matching      MatchingConfig    `yaml:"file_matching" json:"file_matching"`
	Output        OutputConfig      `yaml:"output" json:"output"`
	Database      DatabaseConfig    `yaml:"database" json:"database"`
	Logging       LoggingConfig     `yaml:"logging" json:"logging"`
	Performance   PerformanceConfig `yaml:"performance" json:"performance"`
	MediaInfo     MediaInfoConfig   `yaml:"mediainfo" json:"mediainfo"`
}

// ServerConfig holds API server configuration
type ServerConfig struct {
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`
}

// APIConfig holds API-specific configuration
type APIConfig struct {
	Security SecurityConfig `yaml:"security" json:"security"`
}

// SecurityConfig holds API security settings for path validation and resource limits
type SecurityConfig struct {
	AllowedDirectories []string        `yaml:"allowed_directories" json:"allowed_directories"`
	DeniedDirectories  []string        `yaml:"denied_directories" json:"denied_directories"`
	MaxFilesPerScan    int             `yaml:"max_files_per_scan" json:"max_files_per_scan"`
	ScanTimeoutSeconds int             `yaml:"scan_timeout_seconds" json:"scan_timeout_seconds"`
	AllowedOrigins     []string        `yaml:"allowed_origins" json:"allowed_origins"`
	AllowUNC           bool            `yaml:"allow_unc" json:"allow_unc"`
	AllowedUNCServers  []string        `yaml:"allowed_unc_servers" json:"allowed_unc_servers"`
	RateLimit          RateLimitConfig `yaml:"rate_limit" json:"rate_limit"`
	TrustedProxies     []string        `yaml:"trusted_proxies" json:"trusted_proxies"`
	ForceSecureCookies bool            `yaml:"force_secure_cookies" json:"force_secure_cookies"`
}

type RateLimitConfig struct {
	RequestsPerMinute int `yaml:"requests_per_minute" json:"requests_per_minute"`
}

// SystemConfig holds system-level settings
type SystemConfig struct {
	// Umask for file creation (e.g., "002" for rwxrwxr-x)
	// Can be overridden with UMASK environment variable
	Umask string `yaml:"umask" json:"umask"`
	// VersionCheckEnabled enables checking for new releases
	VersionCheckEnabled bool `yaml:"version_check_enabled" json:"version_check_enabled"`
	// VersionCheckIntervalHours is the interval between version checks in hours
	VersionCheckIntervalHours int `yaml:"version_check_interval_hours" json:"version_check_interval_hours"`
	// TempDir is the base directory for temporary files (default: "data/temp").
	// Can be overridden with JAVINIZER_TEMP_DIR environment variable.
	// Subdirectory "posters/{jobID}" is created for batch job temp posters.
	TempDir string `yaml:"temp_dir" json:"temp_dir"`
}

// MarshalYAML keeps Config marshaling explicit and ensures ScrapersConfig custom
// marshaling is always applied.
func (c *Config) MarshalYAML() (interface{}, error) {
	m := map[string]any{
		"config_version": c.ConfigVersion,
		"server":         c.Server,
		"api":            c.API,
		"system":         c.System,
		"metadata":       c.Metadata,
		"file_matching":  c.Matching,
		"output":         c.Output,
		"database":       c.Database,
		"logging":        c.Logging,
		"performance":    c.Performance,
		"mediainfo":      c.MediaInfo,
	}

	scrapersYAML, err := c.Scrapers.MarshalYAML()
	if err != nil {
		return nil, fmt.Errorf("failed to marshal scrapers: %w", err)
	}
	m["scrapers"] = scrapersYAML

	return m, nil
}

// UnmarshalYAML delegates to yaml.v3 node decoding and lets field-level
// unmarshalers (e.g. ScrapersConfig) handle their own logic.
func (c *Config) UnmarshalYAML(node *yaml.Node) error {
	if node == nil || node.Kind == 0 {
		return nil
	}

	type configAlias Config
	if err := node.Decode((*configAlias)(c)); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return nil
}

// MatchingConfig holds file matching configuration
type MatchingConfig struct {
	Extensions      []string `yaml:"extensions" json:"extensions"`
	MinSizeMB       int      `yaml:"min_size_mb" json:"min_size_mb"`
	ExcludePatterns []string `yaml:"exclude_patterns" json:"exclude_patterns"`
	RegexEnabled    bool     `yaml:"regex_enabled" json:"regex_enabled"`
	RegexPattern    string   `yaml:"regex_pattern" json:"regex_pattern"`
}

// OutputConfig holds output/organization settings
type OutputConfig struct {
	FolderFormat        string              `yaml:"folder_format" json:"folder_format"`
	FileFormat          string              `yaml:"file_format" json:"file_format"`
	SubfolderFormat     []string            `yaml:"subfolder_format" json:"subfolder_format"`
	Delimiter           string              `yaml:"delimiter" json:"delimiter"`
	MaxTitleLength      int                 `yaml:"max_title_length" json:"max_title_length"`
	MaxPathLength       int                 `yaml:"max_path_length" json:"max_path_length"`
	MoveSubtitles       bool                `yaml:"move_subtitles" json:"move_subtitles"`
	SubtitleExtensions  []string            `yaml:"subtitle_extensions" json:"subtitle_extensions"`
	OperationMode       types.OperationMode `yaml:"operation_mode" json:"operation_mode"`
	RenameFolderInPlace bool                `yaml:"rename_folder_in_place" json:"rename_folder_in_place"`
	MoveToFolder        bool                `yaml:"move_to_folder" json:"move_to_folder"` // Move/copy files to organized folders (default: true)
	RenameFile          bool                `yaml:"rename_file" json:"rename_file"`       // Rename files using file_format template (default: true)
	AllowRevert         bool                `yaml:"allow_revert" json:"allow_revert"`     // Enable revert operations (default: false — opt-in for safety)
	GroupActress        bool                `yaml:"group_actress" json:"group_actress"`   // Replace multiple actresses with "@Group" in templates (default: false)
	PosterFormat        string              `yaml:"poster_format" json:"poster_format"`
	FanartFormat        string              `yaml:"fanart_format" json:"fanart_format"`
	TrailerFormat       string              `yaml:"trailer_format" json:"trailer_format"`
	ScreenshotFormat    string              `yaml:"screenshot_format" json:"screenshot_format"`
	ScreenshotFolder    string              `yaml:"screenshot_folder" json:"screenshot_folder"`
	ScreenshotPadding   int                 `yaml:"screenshot_padding" json:"screenshot_padding"`
	ActressFolder       string              `yaml:"actress_folder" json:"actress_folder"`
	ActressFormat       string              `yaml:"actress_format" json:"actress_format"`
	DownloadCover       bool                `yaml:"download_cover" json:"download_cover"`
	DownloadPoster      bool                `yaml:"download_poster" json:"download_poster"`
	DownloadExtrafanart bool                `yaml:"download_extrafanart" json:"download_extrafanart"`
	DownloadTrailer     bool                `yaml:"download_trailer" json:"download_trailer"`
	DownloadActress     bool                `yaml:"download_actress" json:"download_actress"`
	DownloadTimeout     int                 `yaml:"download_timeout" json:"download_timeout"` // Timeout in seconds for HTTP downloads (default: 60)
	DownloadProxy       ProxyConfig         `yaml:"download_proxy" json:"download_proxy"`     // Separate proxy for downloads (optional)
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Type     string `yaml:"type" json:"type"`           // sqlite (currently only supported backend)
	DSN      string `yaml:"dsn" json:"dsn"`             // Data Source Name
	LogLevel string `yaml:"log_level" json:"log_level"` // Database query logging: silent, error, warn, info (default: silent)
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level      string `yaml:"level" json:"level"`               // debug, info, warn, error
	Format     string `yaml:"format" json:"format"`             // json, text
	Output     string `yaml:"output" json:"output"`             // stdout, file path
	MaxSizeMB  int    `yaml:"max_size_mb" json:"max_size_mb"`   // Max size in MB before rotation (0 = no rotation)
	MaxBackups int    `yaml:"max_backups" json:"max_backups"`   // Max number of old log files to keep (0 = unlimited)
	MaxAgeDays int    `yaml:"max_age_days" json:"max_age_days"` // Max age in days to keep log files (0 = no limit)
	Compress   bool   `yaml:"compress" json:"compress"`         // Compress rotated files
}

// PerformanceConfig holds performance and concurrency settings
type PerformanceConfig struct {
	MaxWorkers     int `yaml:"max_workers" json:"max_workers"`         // Maximum concurrent workers (default: 5)
	WorkerTimeout  int `yaml:"worker_timeout" json:"worker_timeout"`   // Timeout per task in seconds (default: 300)
	BufferSize     int `yaml:"buffer_size" json:"buffer_size"`         // Channel buffer size (default: 100)
	UpdateInterval int `yaml:"update_interval" json:"update_interval"` // UI update interval in milliseconds (default: 100)
}

// MediaInfoConfig holds MediaInfo functionality configuration
type MediaInfoConfig struct {
	CLIEnabled bool   `yaml:"cli_enabled" json:"cli_enabled"` // Enable MediaInfo CLI fallback (default: false)
	CLIPath    string `yaml:"cli_path" json:"cli_path"`       // Path to mediainfo binary (default: "mediainfo")
	CLITimeout int    `yaml:"cli_timeout" json:"cli_timeout"` // Timeout in seconds for CLI execution (default: 30)
}

// Validate checks configuration values for validity
func (c *Config) Validate() error {
	// Always normalize to pick up any changes from YAML load that haven't been applied yet.
	// NormalizeScraperConfigs is idempotent and preserves existing Overrides values.
	c.Scrapers.NormalizeScraperConfigs()

	// Validate database settings
	dbType := strings.ToLower(strings.TrimSpace(c.Database.Type))
	if dbType == "" {
		// Backward compatibility: treat empty type as sqlite default.
		dbType = "sqlite"
	}
	if dbType != "sqlite" {
		return fmt.Errorf("database.type must be 'sqlite' (currently only sqlite is supported)")
	}

	if strings.TrimSpace(c.Database.DSN) == "" {
		return fmt.Errorf("database.dsn is required")
	}

	// Validate scraper timeouts
	if c.Scrapers.TimeoutSeconds < 1 || c.Scrapers.TimeoutSeconds > 300 {
		return fmt.Errorf("scrapers.timeout_seconds must be between 1 and 300")
	}
	if c.Scrapers.RequestTimeoutSeconds < 1 || c.Scrapers.RequestTimeoutSeconds > 600 {
		return fmt.Errorf("scrapers.request_timeout_seconds must be between 1 and 600")
	}

	// CONF-04: Generic scraper config validation — uses flatConfigs map for interface dispatch.
	// NO hardcoded scraper-name branches.
	for name, sc := range c.Scrapers.Overrides {
		// Interface dispatch via flatConfigs map (no switch on scraper name)
		if validator, ok := c.Scrapers.flatConfigs[name]; ok {
			if err := validator.ValidateConfig(sc); err != nil {
				return err
			}
		}
	}

	// Validate proxy profiles (global + per-scraper)
	if err := validateProxyProfileConfig(c); err != nil {
		return err
	}

	// Validate FlareSolverr config (global)
	if err := validateFlareSolverrConfig("scrapers.flaresolverr", c.Scrapers.FlareSolverr); err != nil {
		return err
	}

	// NEW: Validate Browser config (global)
	if err := validateBrowserConfig("scrapers.browser", c.Scrapers.Browser); err != nil {
		return err
	}

	// Validate referer URL format
	referer := strings.TrimSpace(c.Scrapers.Referer)
	if referer == "" {
		// Backward compatibility with old configs that omitted referer.
		referer = "https://www.dmm.co.jp/"
	}
	u, err := url.Parse(referer)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("scrapers.referer must be a valid http(s) URL with a host")
	}

	if err := c.validateTranslationConfig(); err != nil {
		return err
	}

	// Validate performance settings
	if c.Performance.MaxWorkers < 1 || c.Performance.MaxWorkers > 100 {
		return fmt.Errorf("performance.max_workers must be between 1 and 100")
	}
	if c.Performance.WorkerTimeout < 10 || c.Performance.WorkerTimeout > 3600 {
		return fmt.Errorf("performance.worker_timeout must be between 10 and 3600")
	}
	if c.Performance.UpdateInterval < 10 || c.Performance.UpdateInterval > 5000 {
		return fmt.Errorf("performance.update_interval must be between 10 and 5000")
	}

	// Validate update settings
	// Allow 0 to mean "use default" (handled by DefaultConfig and migrations)
	if c.System.VersionCheckIntervalHours != 0 && (c.System.VersionCheckIntervalHours < 1 || c.System.VersionCheckIntervalHours > 168) {
		return fmt.Errorf("system.version_check_interval_hours must be between 1 and 168 (1 week), or 0 for default")
	}

	// Validate logging rotation settings
	if c.Logging.MaxSizeMB < 0 {
		return fmt.Errorf("logging.max_size_mb must be >= 0")
	}
	if c.Logging.MaxBackups < 0 {
		return fmt.Errorf("logging.max_backups must be >= 0")
	}
	if c.Logging.MaxAgeDays < 0 {
		return fmt.Errorf("logging.max_age_days must be >= 0")
	}

	return nil
}

func (c *Config) validateTranslationConfig() error {
	t := c.Metadata.Translation

	provider := strings.ToLower(strings.TrimSpace(t.Provider))
	if provider == "" {
		provider = "openai"
	}

	targetLanguage := strings.TrimSpace(t.TargetLanguage)
	if targetLanguage == "" {
		targetLanguage = "ja"
	}

	timeoutSeconds := t.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 60
	}

	openAIBaseURL := strings.TrimSpace(t.OpenAI.BaseURL)
	if openAIBaseURL == "" {
		openAIBaseURL = "https://api.openai.com/v1"
	}

	openAIModel := strings.TrimSpace(t.OpenAI.Model)
	if openAIModel == "" {
		openAIModel = "gpt-4o-mini"
	}

	deepLMode := strings.ToLower(strings.TrimSpace(t.DeepL.Mode))
	if deepLMode == "" {
		deepLMode = "free"
	}

	googleMode := strings.ToLower(strings.TrimSpace(t.Google.Mode))
	if googleMode == "" {
		googleMode = "free"
	}

	if !t.Enabled {
		return nil
	}

	if timeoutSeconds < 5 || timeoutSeconds > 300 {
		return fmt.Errorf("metadata.translation.timeout_seconds must be between 5 and 300")
	}

	if targetLanguage == "" {
		return fmt.Errorf("metadata.translation.target_language is required when translation is enabled")
	}

	switch provider {
	case "openai":
		if openAIModel == "" {
			return fmt.Errorf("metadata.translation.openai.model is required when provider=openai")
		}
		if err := validateHTTPBaseURL("metadata.translation.openai.base_url", openAIBaseURL); err != nil {
			return err
		}
	case "deepl":
		if deepLMode != "free" && deepLMode != "pro" {
			return fmt.Errorf("metadata.translation.deepl.mode must be either 'free' or 'pro'")
		}
		if strings.TrimSpace(t.DeepL.BaseURL) != "" {
			if err := validateHTTPBaseURL("metadata.translation.deepl.base_url", t.DeepL.BaseURL); err != nil {
				return err
			}
		}
	case "google":
		if googleMode != "free" && googleMode != "paid" {
			return fmt.Errorf("metadata.translation.google.mode must be either 'free' or 'paid'")
		}
		if strings.TrimSpace(t.Google.BaseURL) != "" {
			if err := validateHTTPBaseURL("metadata.translation.google.base_url", t.Google.BaseURL); err != nil {
				return err
			}
		}
	case "openai-compatible":
		if strings.TrimSpace(t.OpenAICompatible.BaseURL) == "" {
			return fmt.Errorf("metadata.translation.openai_compatible.base_url is required when provider=openai-compatible")
		}
		if err := validateHTTPBaseURL("metadata.translation.openai_compatible.base_url", t.OpenAICompatible.BaseURL); err != nil {
			return err
		}
		if strings.TrimSpace(t.OpenAICompatible.Model) == "" {
			return fmt.Errorf("metadata.translation.openai_compatible.model is required when provider=openai-compatible")
		}
		switch t.OpenAICompatible.NormalizedBackendType() {
		case "", "vllm", "ollama", "llama.cpp", "other":
		default:
			return fmt.Errorf("metadata.translation.openai_compatible.backend_type must be one of: auto, vllm, ollama, llama.cpp, other")
		}
	case "anthropic":
		if strings.TrimSpace(t.Anthropic.BaseURL) == "" {
			return fmt.Errorf("metadata.translation.anthropic.base_url is required when provider=anthropic")
		}
		if err := validateHTTPBaseURL("metadata.translation.anthropic.base_url", t.Anthropic.BaseURL); err != nil {
			return err
		}
		if strings.TrimSpace(t.Anthropic.Model) == "" {
			return fmt.Errorf("metadata.translation.anthropic.model is required when provider=anthropic")
		}
	default:
		return fmt.Errorf("metadata.translation.provider must be one of: openai, openai-compatible, anthropic, deepl, google")
	}

	// REGV-04: Validate API key presence at config time
	switch provider {
	case "openai":
		if strings.TrimSpace(t.OpenAI.APIKey) == "" {
			return fmt.Errorf("metadata.translation.openai.api_key is required when provider=openai")
		}
	case "deepl":
		if strings.TrimSpace(t.DeepL.APIKey) == "" {
			return fmt.Errorf("metadata.translation.deepl.api_key is required when provider=deepl")
		}
	case "google":
		// Google free mode doesn't require API key; paid mode does
		if googleMode == "paid" && strings.TrimSpace(t.Google.APIKey) == "" {
			return fmt.Errorf("metadata.translation.google.api_key is required when provider=google and mode=paid")
		}
	case "openai-compatible":
		// API key is optional for self-hosted endpoints
	case "anthropic":
		if strings.TrimSpace(t.Anthropic.APIKey) == "" {
			return fmt.Errorf("metadata.translation.anthropic.api_key is required when provider=anthropic")
		}
	}

	return nil
}
