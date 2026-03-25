package config

import (
	"fmt"
	"net/url"
	"strings"
)

// File and directory permission constants
// Centralized to ensure consistency across the codebase
const (
	// DirPermConfig is the permission mode for configuration directories (owner + group read/execute)
	DirPermConfig = 0755
	// DirPermTemp is the permission mode for temporary/sensitive directories (owner-only access)
	DirPermTemp = 0700
	// FilePermConfig is the permission mode for configuration files
	FilePermConfig = 0644

	// CurrentConfigVersion tracks compatibility breakpoints for on-disk config.
	// Do not bump for additive/default-only fields; those are handled by loading
	// into DefaultConfig() and idempotent normalization rules.
	CurrentConfigVersion = 3

	// DefaultUserAgent is the true/identifying UA for Javinizer.
	DefaultUserAgent = "Javinizer (+https://github.com/javinizer/Javinizer)"

	// DefaultFakeUserAgent is a browser-like UA for scraper-hostile sites.
	DefaultFakeUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
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
	// Allowed directories for scanning/browsing (empty = no allowlist restriction)
	AllowedDirectories []string `yaml:"allowed_directories" json:"allowed_directories"`
	// Denied directories (in addition to built-in system directories)
	DeniedDirectories []string `yaml:"denied_directories" json:"denied_directories"`
	// Maximum number of files to return in a scan
	MaxFilesPerScan int `yaml:"max_files_per_scan" json:"max_files_per_scan"`
	// Timeout for scan operations in seconds
	ScanTimeoutSeconds int `yaml:"scan_timeout_seconds" json:"scan_timeout_seconds"`
	// Allowed origins for CORS and WebSocket connections (empty = same-origin only, "*" = allow all)
	AllowedOrigins []string `yaml:"allowed_origins" json:"allowed_origins"`
}

// SystemConfig holds system-level settings
type SystemConfig struct {
	// Umask for file creation (e.g., "002" for rwxrwxr-x)
	// Can be overridden with UMASK environment variable
	Umask string `yaml:"umask" json:"umask"`
	// UpdateEnabled enables checking for new releases
	UpdateEnabled bool `yaml:"update_enabled" json:"update_enabled"`
	// UpdateCheckIntervalHours is the interval between update checks in hours
	UpdateCheckIntervalHours int `yaml:"update_check_interval_hours" json:"update_check_interval_hours"`
}

// ScrapersConfig holds scraper-specific settings
type ScrapersConfig struct {
	UserAgent             string                `yaml:"user_agent" json:"user_agent"`
	Referer               string                `yaml:"referer" json:"referer"`                                 // Referer header for CDN compatibility (default: https://www.dmm.co.jp/)
	TimeoutSeconds        int                   `yaml:"timeout_seconds" json:"timeout_seconds"`                 // HTTP client timeout in seconds (default: 30)
	RequestTimeoutSeconds int                   `yaml:"request_timeout_seconds" json:"request_timeout_seconds"` // Overall request timeout in seconds (default: 60)
	Priority              []string              `yaml:"priority" json:"priority"`                               // Global scraper priority order
	Proxy                 ProxyConfig           `yaml:"proxy" json:"proxy"`                                     // Default HTTP/SOCKS5 proxy for scraper requests
	R18Dev                R18DevConfig          `yaml:"r18dev" json:"r18dev"`
	DMM                   DMMConfig             `yaml:"dmm" json:"dmm"`
	LibreDMM              LibreDMMConfig        `yaml:"libredmm" json:"libredmm"`
	MGStage               MGStageConfig         `yaml:"mgstage" json:"mgstage"`
	JavLibrary            JavLibraryConfig      `yaml:"javlibrary" json:"javlibrary"`
	JavDB                 JavDBConfig           `yaml:"javdb" json:"javdb"`
	JavBus                JavBusConfig          `yaml:"javbus" json:"javbus"`
	Jav321                Jav321Config          `yaml:"jav321" json:"jav321"`
	TokyoHot              TokyoHotConfig        `yaml:"tokyohot" json:"tokyohot"`
	AVEntertainment       AVEntertainmentConfig `yaml:"aventertainment" json:"aventertainment"`
	DLGetchu              DLGetchuConfig        `yaml:"dlgetchu" json:"dlgetchu"`
	Caribbeancom          CaribbeancomConfig    `yaml:"caribbeancom" json:"caribbeancom"`
	FC2                   FC2Config             `yaml:"fc2" json:"fc2"`
}

// ScraperConfig holds common scraper configuration fields used by the Scraper interface.
// Individual scraper configs embed this and add scraper-specific fields.
type ScraperConfig struct {
	Enabled          bool         `yaml:"enabled" json:"enabled"`
	Language         string       `yaml:"language" json:"language"`                                 // Language code varies by scraper
	RequestDelay     int          `yaml:"request_delay" json:"request_delay"`                     // Delay between requests in milliseconds
	UseFakeUserAgent bool         `yaml:"use_fake_user_agent" json:"use_fake_user_agent"`         // Use browser-like User-Agent header
	FakeUserAgent    string       `yaml:"fake_user_agent" json:"fake_user_agent"`                 // Optional custom fake User-Agent
	Proxy            *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy    *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
}

// R18DevConfig holds R18.dev scraper configuration
type R18DevConfig struct {
	Enabled           bool         `yaml:"enabled" json:"enabled"`
	Language          string       `yaml:"language" json:"language"`                                 // Language code: en, ja (default: en)
	RequestDelay      int          `yaml:"request_delay" json:"request_delay"`                       // Delay between requests in milliseconds (0 = no delay)
	MaxRetries        int          `yaml:"max_retries" json:"max_retries"`                           // Maximum number of retry attempts for rate-limited requests
	RespectRetryAfter bool         `yaml:"respect_retry_after" json:"respect_retry_after"`           // Whether to respect Retry-After header from server
	UseFakeUserAgent  bool         `yaml:"use_fake_user_agent" json:"use_fake_user_agent"`           // Use browser-like User-Agent header for this scraper
	FakeUserAgent     string       `yaml:"fake_user_agent" json:"fake_user_agent"`                   // Optional custom fake User-Agent (defaults to built-in browser UA)
	Proxy             *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy     *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
}

// DMMConfig holds DMM/Fanza scraper configuration
type DMMConfig struct {
	Enabled          bool         `yaml:"enabled" json:"enabled"`
	ScrapeActress    bool         `yaml:"scrape_actress" json:"scrape_actress"`
	EnableBrowser    bool         `yaml:"enable_browser" json:"enable_browser"`                     // Enable browser mode for video.dmm.co.jp (JavaScript rendering)
	BrowserTimeout   int          `yaml:"browser_timeout" json:"browser_timeout"`                   // Timeout in seconds for browser operations (default: 30)
	UseFakeUserAgent bool         `yaml:"use_fake_user_agent" json:"use_fake_user_agent"`           // Use browser-like User-Agent header for this scraper
	FakeUserAgent    string       `yaml:"fake_user_agent" json:"fake_user_agent"`                   // Optional custom fake User-Agent (defaults to built-in browser UA)
	Proxy            *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy    *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
}

// LibreDMMConfig holds LibreDMM scraper configuration
type LibreDMMConfig struct {
	Enabled          bool         `yaml:"enabled" json:"enabled"`
	RequestDelay     int          `yaml:"request_delay" json:"request_delay"`                       // Delay between requests in milliseconds (0 = no delay)
	BaseURL          string       `yaml:"base_url" json:"base_url"`                                 // Base URL for LibreDMM
	UseFakeUserAgent bool         `yaml:"use_fake_user_agent" json:"use_fake_user_agent"`           // Use browser-like User-Agent header for this scraper
	FakeUserAgent    string       `yaml:"fake_user_agent" json:"fake_user_agent"`                   // Optional custom fake User-Agent (defaults to built-in browser UA)
	Proxy            *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy    *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
}

// MGStageConfig holds MGStage scraper configuration
type MGStageConfig struct {
	Enabled          bool         `yaml:"enabled" json:"enabled"`
	RequestDelay     int          `yaml:"request_delay" json:"request_delay"`                       // Delay between requests in milliseconds (0 = no delay)
	UseFakeUserAgent bool         `yaml:"use_fake_user_agent" json:"use_fake_user_agent"`           // Use browser-like User-Agent header for this scraper
	FakeUserAgent    string       `yaml:"fake_user_agent" json:"fake_user_agent"`                   // Optional custom fake User-Agent (defaults to built-in browser UA)
	Proxy            *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy    *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
}

// JavLibraryConfig holds JavLibrary scraper configuration
type JavLibraryConfig struct {
	Enabled          bool         `yaml:"enabled" json:"enabled"`
	Language         string       `yaml:"language" json:"language"`                                 // Language code: en, ja, cn, tw (default: en)
	RequestDelay     int          `yaml:"request_delay" json:"request_delay"`                       // Delay between requests in milliseconds (0 = no delay)
	BaseURL          string       `yaml:"base_url" json:"base_url"`                                 // Base URL for JavLibrary
	CfClearance      string       `yaml:"cf_clearance" json:"cf_clearance"`                         // Cloudflare clearance cookie (deprecated, use FlareSolverr)
	CfBm             string       `yaml:"cf_bm" json:"cf_bm"`                                       // Cloudflare Bot Management cookie (deprecated)
	UserAgent        string       `yaml:"user_agent" json:"user_agent"`                             // Custom user agent (optional)
	UseFakeUserAgent bool         `yaml:"use_fake_user_agent" json:"use_fake_user_agent"`           // Use browser-like User-Agent header for this scraper
	FakeUserAgent    string       `yaml:"fake_user_agent" json:"fake_user_agent"`                   // Optional custom fake User-Agent (defaults to built-in browser UA)
	UseFlareSolverr  bool         `yaml:"use_flaresolverr" json:"use_flaresolverr"`                 // Enable FlareSolverr for Cloudflare bypass
	Proxy            *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy    *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
}

// JavDBConfig holds JavDB scraper configuration
type JavDBConfig struct {
	Enabled          bool         `yaml:"enabled" json:"enabled"`
	RequestDelay     int          `yaml:"request_delay" json:"request_delay"`                       // Delay between requests in milliseconds (0 = no delay)
	BaseURL          string       `yaml:"base_url" json:"base_url"`                                 // Base URL for JavDB
	UseFakeUserAgent bool         `yaml:"use_fake_user_agent" json:"use_fake_user_agent"`           // Use browser-like User-Agent header for this scraper
	FakeUserAgent    string       `yaml:"fake_user_agent" json:"fake_user_agent"`                   // Optional custom fake User-Agent (defaults to built-in browser UA)
	UseFlareSolverr  bool         `yaml:"use_flaresolverr" json:"use_flaresolverr"`                 // Enable FlareSolverr for Cloudflare bypass
	Proxy            *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy    *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
}

// JavBusConfig holds JavBus scraper configuration
type JavBusConfig struct {
	Enabled          bool         `yaml:"enabled" json:"enabled"`
	Language         string       `yaml:"language" json:"language"`                                 // Language code: en, ja, zh (default: zh)
	RequestDelay     int          `yaml:"request_delay" json:"request_delay"`                       // Delay between requests in milliseconds (0 = no delay)
	BaseURL          string       `yaml:"base_url" json:"base_url"`                                 // Base URL for JavBus
	UseFakeUserAgent bool         `yaml:"use_fake_user_agent" json:"use_fake_user_agent"`           // Use browser-like User-Agent header for this scraper
	FakeUserAgent    string       `yaml:"fake_user_agent" json:"fake_user_agent"`                   // Optional custom fake User-Agent (defaults to built-in browser UA)
	Proxy            *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy    *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
}

// Jav321Config holds Jav321 scraper configuration
type Jav321Config struct {
	Enabled          bool         `yaml:"enabled" json:"enabled"`
	RequestDelay     int          `yaml:"request_delay" json:"request_delay"`                       // Delay between requests in milliseconds (0 = no delay)
	BaseURL          string       `yaml:"base_url" json:"base_url"`                                 // Base URL for Jav321
	UseFakeUserAgent bool         `yaml:"use_fake_user_agent" json:"use_fake_user_agent"`           // Use browser-like User-Agent header for this scraper
	FakeUserAgent    string       `yaml:"fake_user_agent" json:"fake_user_agent"`                   // Optional custom fake User-Agent (defaults to built-in browser UA)
	Proxy            *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy    *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
}

// TokyoHotConfig holds TokyoHot scraper configuration
type TokyoHotConfig struct {
	Enabled          bool         `yaml:"enabled" json:"enabled"`
	Language         string       `yaml:"language" json:"language"`                                 // Language code: en, ja, zh (default: en)
	RequestDelay     int          `yaml:"request_delay" json:"request_delay"`                       // Delay between requests in milliseconds (0 = no delay)
	BaseURL          string       `yaml:"base_url" json:"base_url"`                                 // Base URL for TokyoHot
	UseFakeUserAgent bool         `yaml:"use_fake_user_agent" json:"use_fake_user_agent"`           // Use browser-like User-Agent header for this scraper
	FakeUserAgent    string       `yaml:"fake_user_agent" json:"fake_user_agent"`                   // Optional custom fake User-Agent (defaults to built-in browser UA)
	Proxy            *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy    *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
}

// AVEntertainmentConfig holds AVEntertainment scraper configuration
type AVEntertainmentConfig struct {
	Enabled            bool         `yaml:"enabled" json:"enabled"`
	Language           string       `yaml:"language" json:"language"`                                 // Language code: en, ja (default: en)
	RequestDelay       int          `yaml:"request_delay" json:"request_delay"`                       // Delay between requests in milliseconds (0 = no delay)
	BaseURL            string       `yaml:"base_url" json:"base_url"`                                 // Base URL for AVEntertainment
	ScrapeBonusScreens bool         `yaml:"scrape_bonus_screens" json:"scrape_bonus_screens"`         // Append bonus image files (e.g., "特典ファイル") to screenshot URLs
	UseFakeUserAgent   bool         `yaml:"use_fake_user_agent" json:"use_fake_user_agent"`           // Use browser-like User-Agent header for this scraper
	FakeUserAgent      string       `yaml:"fake_user_agent" json:"fake_user_agent"`                   // Optional custom fake User-Agent (defaults to built-in browser UA)
	Proxy              *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy      *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
}

// DLGetchuConfig holds DLgetchu scraper configuration
type DLGetchuConfig struct {
	Enabled          bool         `yaml:"enabled" json:"enabled"`
	RequestDelay     int          `yaml:"request_delay" json:"request_delay"`                       // Delay between requests in milliseconds (0 = no delay)
	BaseURL          string       `yaml:"base_url" json:"base_url"`                                 // Base URL for DLgetchu
	UseFakeUserAgent bool         `yaml:"use_fake_user_agent" json:"use_fake_user_agent"`           // Use browser-like User-Agent header for this scraper
	FakeUserAgent    string       `yaml:"fake_user_agent" json:"fake_user_agent"`                   // Optional custom fake User-Agent (defaults to built-in browser UA)
	Proxy            *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy    *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
}

// CaribbeancomConfig holds Caribbeancom scraper configuration
type CaribbeancomConfig struct {
	Enabled          bool         `yaml:"enabled" json:"enabled"`
	Language         string       `yaml:"language" json:"language"`                                 // Language code: ja, en (default: ja)
	RequestDelay     int          `yaml:"request_delay" json:"request_delay"`                       // Delay between requests in milliseconds (0 = no delay)
	BaseURL          string       `yaml:"base_url" json:"base_url"`                                 // Base URL for Caribbeancom
	UseFakeUserAgent bool         `yaml:"use_fake_user_agent" json:"use_fake_user_agent"`           // Use browser-like User-Agent header for this scraper
	FakeUserAgent    string       `yaml:"fake_user_agent" json:"fake_user_agent"`                   // Optional custom fake User-Agent (defaults to built-in browser UA)
	Proxy            *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy    *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
}

// FC2Config holds FC2 scraper configuration
type FC2Config struct {
	Enabled          bool         `yaml:"enabled" json:"enabled"`
	RequestDelay     int          `yaml:"request_delay" json:"request_delay"`                       // Delay between requests in milliseconds (0 = no delay)
	BaseURL          string       `yaml:"base_url" json:"base_url"`                                 // Base URL for FC2
	UseFakeUserAgent bool         `yaml:"use_fake_user_agent" json:"use_fake_user_agent"`           // Use browser-like User-Agent header for this scraper
	FakeUserAgent    string       `yaml:"fake_user_agent" json:"fake_user_agent"`                   // Optional custom fake User-Agent (defaults to built-in browser UA)
	Proxy            *ProxyConfig `yaml:"proxy,omitempty" json:"proxy,omitempty"`                   // Optional scraper-specific proxy override
	DownloadProxy    *ProxyConfig `yaml:"download_proxy,omitempty" json:"download_proxy,omitempty"` // Optional scraper-specific download proxy override
}

// FlareSolverrConfig holds FlareSolverr configuration for bypassing Cloudflare
type FlareSolverrConfig struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`         // Enable FlareSolverr for bypassing Cloudflare
	URL        string `yaml:"url" json:"url"`                 // FlareSolverr endpoint (default: http://localhost:8191/v1)
	Timeout    int    `yaml:"timeout" json:"timeout"`         // Request timeout in seconds (default: 30)
	MaxRetries int    `yaml:"max_retries" json:"max_retries"` // Max retry attempts for FlareSolverr calls (default: 3)
	SessionTTL int    `yaml:"session_ttl" json:"session_ttl"` // Session TTL in seconds (default: 300)
}

// ProxyProfile holds reusable proxy connection settings.
type ProxyProfile struct {
	URL          string             `yaml:"url" json:"url"`
	Username     string             `yaml:"username" json:"username"`
	Password     string             `yaml:"password" json:"password"`
	FlareSolverr FlareSolverrConfig `yaml:"flaresolverr" json:"flaresolverr"`
}

// ProxyConfig holds HTTP/SOCKS5 proxy configuration
type ProxyConfig struct {
	Enabled        bool                    `yaml:"enabled" json:"enabled"`                                     // Enable proxy for HTTP requests
	UseMainProxy   bool                    `yaml:"use_main_proxy" json:"use_main_proxy"`                       // Legacy option (rejected by validation)
	Profile        string                  `yaml:"profile,omitempty" json:"profile,omitempty"`                 // Named profile to use (for scraper-specific overrides)
	DefaultProfile string                  `yaml:"default_profile,omitempty" json:"default_profile,omitempty"` // Default profile name (for global scrapers.proxy)
	Profiles       map[string]ProxyProfile `yaml:"profiles,omitempty" json:"profiles,omitempty"`               // Named proxy profiles (global scrapers.proxy)
	URL            string                  `yaml:"url" json:"url"`                                             // Legacy direct field (rejected by validation)
	Username       string                  `yaml:"username" json:"username"`                                   // Legacy direct field (rejected by validation)
	Password       string                  `yaml:"password" json:"password"`                                   // Legacy direct field (rejected by validation)
	FlareSolverr   FlareSolverrConfig      `yaml:"flaresolverr" json:"flaresolverr"`                           // FlareSolverr for Cloudflare bypass
}

// MetadataConfig holds metadata aggregation settings
type MetadataConfig struct {
	Priority         PriorityConfig         `yaml:"priority" json:"priority"`
	ActressDatabase  ActressDatabaseConfig  `yaml:"actress_database" json:"actress_database"`   // Actress image database (SQLite-backed)
	GenreReplacement GenreReplacementConfig `yaml:"genre_replacement" json:"genre_replacement"` // Genre replacement/normalization (SQLite-backed)
	TagDatabase      TagDatabaseConfig      `yaml:"tag_database" json:"tag_database"`           // Per-movie tag database (SQLite-backed)
	Translation      TranslationConfig      `yaml:"translation" json:"translation"`             // Metadata translation pipeline
	IgnoreGenres     []string               `yaml:"ignore_genres" json:"ignore_genres"`
	RequiredFields   []string               `yaml:"required_fields" json:"required_fields"`
	NFO              NFOConfig              `yaml:"nfo" json:"nfo"`
}

// TranslationConfig holds metadata translation settings.
type TranslationConfig struct {
	Enabled                 bool                    `yaml:"enabled" json:"enabled"`                                     // Enable metadata translation after aggregation
	Provider                string                  `yaml:"provider" json:"provider"`                                   // openai, deepl, google
	SourceLanguage          string                  `yaml:"source_language" json:"source_language"`                     // Source language code (e.g., en, ja, auto)
	TargetLanguage          string                  `yaml:"target_language" json:"target_language"`                     // Target language code (e.g., en, ja, zh)
	TimeoutSeconds          int                     `yaml:"timeout_seconds" json:"timeout_seconds"`                     // Request timeout in seconds
	ApplyToPrimary          bool                    `yaml:"apply_to_primary" json:"apply_to_primary"`                   // Replace primary movie metadata with translated text
	OverwriteExistingTarget bool                    `yaml:"overwrite_existing_target" json:"overwrite_existing_target"` // Overwrite target-language translation if already present
	Fields                  TranslationFieldsConfig `yaml:"fields" json:"fields"`                                       // Per-field translation controls
	OpenAI                  OpenAITranslationConfig `yaml:"openai" json:"openai"`                                       // OpenAI/OpenAI-compatible provider settings
	DeepL                   DeepLTranslationConfig  `yaml:"deepl" json:"deepl"`                                         // DeepL provider settings
	Google                  GoogleTranslationConfig `yaml:"google" json:"google"`                                       // Google provider settings
}

// TranslationFieldsConfig controls which metadata fields are translated.
type TranslationFieldsConfig struct {
	Title         bool `yaml:"title" json:"title"`
	OriginalTitle bool `yaml:"original_title" json:"original_title"`
	Description   bool `yaml:"description" json:"description"`
	Director      bool `yaml:"director" json:"director"`
	Maker         bool `yaml:"maker" json:"maker"`
	Label         bool `yaml:"label" json:"label"`
	Series        bool `yaml:"series" json:"series"`
	Genres        bool `yaml:"genres" json:"genres"`
	Actresses     bool `yaml:"actresses" json:"actresses"`
}

// OpenAITranslationConfig holds OpenAI-compatible API settings.
type OpenAITranslationConfig struct {
	BaseURL string `yaml:"base_url" json:"base_url"` // OpenAI-compatible base URL (e.g., https://api.openai.com/v1)
	APIKey  string `yaml:"api_key" json:"api_key"`   // API key for the provider
	Model   string `yaml:"model" json:"model"`       // Model name (e.g., gpt-4o-mini)
}

// DeepLTranslationConfig holds DeepL provider settings.
type DeepLTranslationConfig struct {
	Mode    string `yaml:"mode" json:"mode"`         // free or pro
	BaseURL string `yaml:"base_url" json:"base_url"` // Optional override (defaults to mode-specific endpoint)
	APIKey  string `yaml:"api_key" json:"api_key"`   // DeepL API key
}

// GoogleTranslationConfig holds Google Translate provider settings.
type GoogleTranslationConfig struct {
	Mode    string `yaml:"mode" json:"mode"`         // free or paid
	BaseURL string `yaml:"base_url" json:"base_url"` // Optional override
	APIKey  string `yaml:"api_key" json:"api_key"`   // Required for paid mode
}

// PriorityConfig defines which scraper to prefer for each field
// Note: omitempty is removed so empty arrays are preserved in YAML (signaling "use global")
type PriorityConfig struct {
	Actress       []string `yaml:"actress" json:"actress"`
	OriginalTitle []string `yaml:"original_title" json:"original_title"`
	CoverURL      []string `yaml:"cover_url" json:"cover_url"`
	Description   []string `yaml:"description" json:"description"`
	Director      []string `yaml:"director" json:"director"`
	Genre         []string `yaml:"genre" json:"genre"`
	ID            []string `yaml:"id" json:"id"`
	ContentID     []string `yaml:"content_id" json:"content_id"`
	Label         []string `yaml:"label" json:"label"`
	Maker         []string `yaml:"maker" json:"maker"`
	PosterURL     []string `yaml:"poster_url" json:"poster_url"`
	Rating        []string `yaml:"rating" json:"rating"`
	ReleaseDate   []string `yaml:"release_date" json:"release_date"`
	Runtime       []string `yaml:"runtime" json:"runtime"`
	Series        []string `yaml:"series" json:"series"`
	ScreenshotURL []string `yaml:"screenshot_url" json:"screenshot_url"`
	Title         []string `yaml:"title" json:"title"`
	TrailerURL    []string `yaml:"trailer_url" json:"trailer_url"`
}

// ActressDatabaseConfig holds actress image database configuration
type ActressDatabaseConfig struct {
	Enabled      bool `yaml:"enabled" json:"enabled"`             // Enable actress image lookup from database
	AutoAdd      bool `yaml:"auto_add" json:"auto_add"`           // Automatically add new actresses to database
	ConvertAlias bool `yaml:"convert_alias" json:"convert_alias"` // Convert actress names using alias database
}

// GenreReplacementConfig holds genre replacement/normalization configuration
type GenreReplacementConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`   // Enable genre replacement from database
	AutoAdd bool `yaml:"auto_add" json:"auto_add"` // Automatically add new genres to database (identity mapping)
}

// TagDatabaseConfig holds per-movie tag database configuration
type TagDatabaseConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"` // Enable per-movie tag lookup from database
}

// NFOConfig holds NFO generation settings
type NFOConfig struct {
	Enabled              bool     `yaml:"enabled" json:"enabled"`
	DisplayName          string   `yaml:"display_name" json:"display_name"`
	FilenameTemplate     string   `yaml:"filename_template" json:"filename_template"`
	FirstNameOrder       bool     `yaml:"first_name_order" json:"first_name_order"`
	ActressLanguageJA    bool     `yaml:"actress_language_ja" json:"actress_language_ja"`
	PerFile              bool     `yaml:"per_file" json:"per_file"` // Create separate NFO for each multi-part file
	UnknownActressText   string   `yaml:"unknown_actress_text" json:"unknown_actress_text"`
	ActressAsTag         bool     `yaml:"actress_as_tag" json:"actress_as_tag"`
	AddGenericRole       bool     `yaml:"add_generic_role" json:"add_generic_role"`         // Add generic "Actress" role to all actresses
	AltNameRole          bool     `yaml:"alt_name_role" json:"alt_name_role"`               // Use alternate name (Japanese) in role field
	IncludeOriginalPath  bool     `yaml:"include_originalpath" json:"include_originalpath"` // Include source filename in NFO
	IncludeStreamDetails bool     `yaml:"include_stream_details" json:"include_stream_details"`
	IncludeFanart        bool     `yaml:"include_fanart" json:"include_fanart"`
	IncludeTrailer       bool     `yaml:"include_trailer" json:"include_trailer"`
	RatingSource         string   `yaml:"rating_source" json:"rating_source"`
	Tag                  []string `yaml:"tag" json:"tag"`
	Tagline              string   `yaml:"tagline" json:"tagline"`
	Credits              []string `yaml:"credits" json:"credits"`
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
	FolderFormat        string      `yaml:"folder_format" json:"folder_format"`
	FileFormat          string      `yaml:"file_format" json:"file_format"`
	SubfolderFormat     []string    `yaml:"subfolder_format" json:"subfolder_format"`
	Delimiter           string      `yaml:"delimiter" json:"delimiter"`
	MaxTitleLength      int         `yaml:"max_title_length" json:"max_title_length"`
	MaxPathLength       int         `yaml:"max_path_length" json:"max_path_length"`
	MoveSubtitles       bool        `yaml:"move_subtitles" json:"move_subtitles"`
	SubtitleExtensions  []string    `yaml:"subtitle_extensions" json:"subtitle_extensions"`
	RenameFolderInPlace bool        `yaml:"rename_folder_in_place" json:"rename_folder_in_place"`
	MoveToFolder        bool        `yaml:"move_to_folder" json:"move_to_folder"` // Move/copy files to organized folders (default: true)
	RenameFile          bool        `yaml:"rename_file" json:"rename_file"`       // Rename files using file_format template (default: true)
	GroupActress        bool        `yaml:"group_actress" json:"group_actress"`   // Replace multiple actresses with "@Group" in templates (default: false)
	PosterFormat        string      `yaml:"poster_format" json:"poster_format"`
	FanartFormat        string      `yaml:"fanart_format" json:"fanart_format"`
	TrailerFormat       string      `yaml:"trailer_format" json:"trailer_format"`
	ScreenshotFormat    string      `yaml:"screenshot_format" json:"screenshot_format"`
	ScreenshotFolder    string      `yaml:"screenshot_folder" json:"screenshot_folder"`
	ScreenshotPadding   int         `yaml:"screenshot_padding" json:"screenshot_padding"`
	ActressFolder       string      `yaml:"actress_folder" json:"actress_folder"`
	ActressFormat       string      `yaml:"actress_format" json:"actress_format"`
	DownloadCover       bool        `yaml:"download_cover" json:"download_cover"`
	DownloadPoster      bool        `yaml:"download_poster" json:"download_poster"`
	DownloadExtrafanart bool        `yaml:"download_extrafanart" json:"download_extrafanart"`
	DownloadTrailer     bool        `yaml:"download_trailer" json:"download_trailer"`
	DownloadActress     bool        `yaml:"download_actress" json:"download_actress"`
	DownloadTimeout     int         `yaml:"download_timeout" json:"download_timeout"` // Timeout in seconds for HTTP downloads (default: 60)
	DownloadProxy       ProxyConfig `yaml:"download_proxy" json:"download_proxy"`     // Separate proxy for downloads (optional)
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Type     string `yaml:"type" json:"type"`           // sqlite (currently only supported backend)
	DSN      string `yaml:"dsn" json:"dsn"`             // Data Source Name
	LogLevel string `yaml:"log_level" json:"log_level"` // Database query logging: silent, error, warn, info (default: silent)
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string `yaml:"level" json:"level"`   // debug, info, warn, error
	Format string `yaml:"format" json:"format"` // json, text
	Output string `yaml:"output" json:"output"` // stdout, file path
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

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		ConfigVersion: CurrentConfigVersion,
		Server: ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
		API: APIConfig{
			Security: SecurityConfig{
				AllowedDirectories: []string{}, // Empty = no allowlist restriction
				DeniedDirectories:  []string{}, // Additional denied dirs beyond built-in
				MaxFilesPerScan:    10000,      // Reasonable limit for large directories
				ScanTimeoutSeconds: 30,         // 30 seconds timeout for scans
			},
		},
		Scrapers: ScrapersConfig{
			UserAgent:             DefaultUserAgent,
			TimeoutSeconds:        30,                        // HTTP client timeout
			RequestTimeoutSeconds: 60,                        // Overall request timeout
			Priority:              []string{"r18dev", "dmm"}, // Global scraper execution order
			Proxy: ProxyConfig{
				Enabled:  false,
				URL:      "",
				Profiles: map[string]ProxyProfile{},
				FlareSolverr: FlareSolverrConfig{
					Enabled:    false,
					URL:        "http://localhost:8191/v1",
					Timeout:    30,
					MaxRetries: 3,
					SessionTTL: 300,
				},
			},
			R18Dev: R18DevConfig{
				Enabled:  true,
				Language: "en",
			},
			DMM: DMMConfig{
				Enabled:        false, // DMM site now redirects to JavaScript-rendered site
				ScrapeActress:  false,
				BrowserTimeout: 30, // Timeout for browser operations
			},
			LibreDMM: LibreDMMConfig{
				Enabled:      true,
				RequestDelay: 500,
				BaseURL:      "https://www.libredmm.com",
			},
			MGStage: MGStageConfig{
				Enabled:      false, // Opt-in, requires age verification cookie
				RequestDelay: 500,   // 500ms default delay
			},
			JavLibrary: JavLibraryConfig{
				Enabled:         false, // Opt-in, requires Cloudflare bypass
				Language:        "en",
				RequestDelay:    1000, // 1 second default delay
				BaseURL:         "http://www.javlibrary.com",
				UseFlareSolverr: false,
			},
			JavDB: JavDBConfig{
				Enabled:         false, // Opt-in, often requires Cloudflare bypass
				RequestDelay:    1000,  // 1 second default delay
				BaseURL:         "https://javdb.com",
				UseFlareSolverr: false,
			},
			JavBus: JavBusConfig{
				Enabled:      false,
				Language:     "ja",
				RequestDelay: 1000,
				BaseURL:      "https://www.javbus.com",
			},
			Jav321: Jav321Config{
				Enabled:      false,
				RequestDelay: 1000,
				BaseURL:      "https://jp.jav321.com",
			},
			TokyoHot: TokyoHotConfig{
				Enabled:      false,
				Language:     "ja",
				RequestDelay: 1000,
				BaseURL:      "https://www.tokyo-hot.com",
			},
			AVEntertainment: AVEntertainmentConfig{
				Enabled:            false,
				Language:           "en",
				RequestDelay:       1000,
				BaseURL:            "https://www.aventertainments.com",
				ScrapeBonusScreens: false,
			},
			DLGetchu: DLGetchuConfig{
				Enabled:      false,
				RequestDelay: 1000,
				BaseURL:      "http://dl.getchu.com",
			},
			Caribbeancom: CaribbeancomConfig{
				Enabled:      false,
				Language:     "ja",
				RequestDelay: 1000,
				BaseURL:      "https://www.caribbeancom.com",
			},
			FC2: FC2Config{
				Enabled:      false,
				RequestDelay: 1000,
				BaseURL:      "https://adult.contents.fc2.com",
			},
		},
		Metadata: MetadataConfig{
			Priority: PriorityConfig{
				Actress:       []string{"r18dev", "dmm"},
				Title:         []string{"r18dev", "dmm"},
				Description:   []string{"dmm", "r18dev"},
				Director:      []string{"r18dev", "dmm"},
				Genre:         []string{"r18dev", "dmm"},
				ID:            []string{"r18dev", "dmm"},
				ContentID:     []string{"r18dev", "dmm"},
				Label:         []string{"r18dev", "dmm"},
				Maker:         []string{"r18dev", "dmm"},
				PosterURL:     []string{"r18dev", "dmm"},
				Rating:        []string{"dmm", "r18dev"},
				ReleaseDate:   []string{"r18dev", "dmm"},
				Runtime:       []string{"r18dev", "dmm"},
				Series:        []string{"r18dev", "dmm"},
				CoverURL:      []string{"r18dev", "dmm"},
				ScreenshotURL: []string{"r18dev", "dmm"},
				TrailerURL:    []string{"r18dev", "dmm"},
			},
			ActressDatabase: ActressDatabaseConfig{
				Enabled: true,
				AutoAdd: true,
			},
			GenreReplacement: GenreReplacementConfig{
				Enabled: true,
				AutoAdd: true,
			},
			TagDatabase: TagDatabaseConfig{
				Enabled: false, // Opt-in feature for per-movie custom tags
			},
			Translation: TranslationConfig{
				Enabled:                 false, // Opt-in to avoid API calls unless explicitly configured
				Provider:                "openai",
				SourceLanguage:          "en",
				TargetLanguage:          "ja",
				TimeoutSeconds:          60,
				ApplyToPrimary:          true,
				OverwriteExistingTarget: true,
				Fields: TranslationFieldsConfig{
					Title:         true,
					OriginalTitle: true,
					Description:   true,
					Director:      true,
					Maker:         true,
					Label:         true,
					Series:        true,
					Genres:        true,
					Actresses:     true,
				},
				OpenAI: OpenAITranslationConfig{
					BaseURL: "https://api.openai.com/v1",
					APIKey:  "",
					Model:   "gpt-4o-mini",
				},
				DeepL: DeepLTranslationConfig{
					Mode:    "free",
					BaseURL: "",
					APIKey:  "",
				},
				Google: GoogleTranslationConfig{
					Mode:    "free",
					BaseURL: "",
					APIKey:  "",
				},
			},
			IgnoreGenres: []string{},
			NFO: NFOConfig{
				Enabled:              true,
				DisplayName:          "<TITLE>",
				FilenameTemplate:     "<ID>.nfo",
				FirstNameOrder:       true,
				ActressLanguageJA:    false,
				UnknownActressText:   "Unknown",
				ActressAsTag:         false,
				IncludeStreamDetails: false,
				IncludeFanart:        true,
				IncludeTrailer:       true,
				RatingSource:         "themoviedb",
			},
		},
		Matching: MatchingConfig{
			Extensions:      []string{".mp4", ".mkv", ".avi", ".wmv", ".flv"},
			MinSizeMB:       0,
			ExcludePatterns: []string{"*-trailer*", "*-sample*"},
			RegexEnabled:    false,
			RegexPattern:    `([a-zA-Z|tT28]+-\d+[zZ]?[eE]?)(?:-pt)?(\d{1,2})?`,
		},
		Output: OutputConfig{
			FolderFormat:        "<ID> [<STUDIO>] - <TITLE> (<YEAR>)",
			FileFormat:          "<ID>",
			SubfolderFormat:     []string{},
			Delimiter:           ", ",
			MaxTitleLength:      100,
			MaxPathLength:       240,
			MoveSubtitles:       false,
			SubtitleExtensions:  []string{".srt", ".ass", ".ssa", ".smi", ".vtt"},
			RenameFolderInPlace: false,
			MoveToFolder:        true,  // Move to organized folders by default
			RenameFile:          true,  // Rename files by default
			GroupActress:        false, // Don't group actresses by default
			PosterFormat:        "<ID>-poster.jpg",
			FanartFormat:        "<ID>-fanart.jpg",
			TrailerFormat:       "<ID>-trailer.mp4",
			ScreenshotFormat:    "fanart<INDEX>.jpg",
			ScreenshotFolder:    "extrafanart",
			ScreenshotPadding:   1,
			ActressFolder:       ".actors",
			ActressFormat:       "<ACTORNAME>.jpg",
			DownloadCover:       true,
			DownloadPoster:      true,
			DownloadExtrafanart: false,
			DownloadTrailer:     false,
			DownloadActress:     false,
			DownloadTimeout:     60, // 60 seconds default
			DownloadProxy: ProxyConfig{
				Enabled: false,
				URL:     "",
			},
		},
		Database: DatabaseConfig{
			Type:     "sqlite",
			DSN:      "data/javinizer.db",
			LogLevel: "silent", // Default: no SQL query logging
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
			Output: "stdout",
		},
		Performance: PerformanceConfig{
			MaxWorkers:     5,
			WorkerTimeout:  300,
			BufferSize:     100,
			UpdateInterval: 100,
		},
		MediaInfo: MediaInfoConfig{
			CLIEnabled: false,
			CLIPath:    "mediainfo",
			CLITimeout: 30,
		},
		System: SystemConfig{
			Umask:                    "",
			UpdateEnabled:            true,
			UpdateCheckIntervalHours: 24,
		},
	}
}

// Validate checks configuration values for validity
func (c *Config) Validate() error {
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
	if c.Scrapers.DMM.BrowserTimeout < 1 || c.Scrapers.DMM.BrowserTimeout > 300 {
		return fmt.Errorf("scrapers.dmm.browser_timeout must be between 1 and 300")
	}
	switch strings.ToLower(strings.TrimSpace(c.Scrapers.R18Dev.Language)) {
	case "", "en":
	case "ja":
	default:
		return fmt.Errorf("scrapers.r18dev.language must be either 'en' or 'ja'")
	}

	// Validate JavLibrary language (must be one of: en, ja, cn, tw)
	switch strings.ToLower(strings.TrimSpace(c.Scrapers.JavLibrary.Language)) {
	case "", "en":
	case "ja":
	case "cn":
	case "tw":
	default:
		return fmt.Errorf("scrapers.javlibrary.language must be one of: 'en', 'ja', 'cn', 'tw'")
	}

	if err := validateProxyProfileConfig(c); err != nil {
		return err
	}

	// Validate FlareSolverr config (global + scraper-specific overrides)
	if err := validateFlareSolverrConfig("scrapers.proxy.flaresolverr", c.Scrapers.Proxy.FlareSolverr); err != nil {
		return err
	}
	if c.Scrapers.R18Dev.Proxy != nil {
		if err := validateFlareSolverrConfig("scrapers.r18dev.proxy.flaresolverr", c.Scrapers.R18Dev.Proxy.FlareSolverr); err != nil {
			return err
		}
	}
	if c.Scrapers.DMM.Proxy != nil {
		if err := validateFlareSolverrConfig("scrapers.dmm.proxy.flaresolverr", c.Scrapers.DMM.Proxy.FlareSolverr); err != nil {
			return err
		}
	}
	if c.Scrapers.LibreDMM.Proxy != nil {
		if err := validateFlareSolverrConfig("scrapers.libredmm.proxy.flaresolverr", c.Scrapers.LibreDMM.Proxy.FlareSolverr); err != nil {
			return err
		}
	}
	if c.Scrapers.MGStage.Proxy != nil {
		if err := validateFlareSolverrConfig("scrapers.mgstage.proxy.flaresolverr", c.Scrapers.MGStage.Proxy.FlareSolverr); err != nil {
			return err
		}
	}
	if c.Scrapers.JavLibrary.Proxy != nil {
		if err := validateFlareSolverrConfig("scrapers.javlibrary.proxy.flaresolverr", c.Scrapers.JavLibrary.Proxy.FlareSolverr); err != nil {
			return err
		}
	}
	if c.Scrapers.JavDB.Proxy != nil {
		if err := validateFlareSolverrConfig("scrapers.javdb.proxy.flaresolverr", c.Scrapers.JavDB.Proxy.FlareSolverr); err != nil {
			return err
		}
	}
	if c.Scrapers.JavBus.Proxy != nil {
		if err := validateFlareSolverrConfig("scrapers.javbus.proxy.flaresolverr", c.Scrapers.JavBus.Proxy.FlareSolverr); err != nil {
			return err
		}
	}
	if c.Scrapers.Jav321.Proxy != nil {
		if err := validateFlareSolverrConfig("scrapers.jav321.proxy.flaresolverr", c.Scrapers.Jav321.Proxy.FlareSolverr); err != nil {
			return err
		}
	}
	if c.Scrapers.TokyoHot.Proxy != nil {
		if err := validateFlareSolverrConfig("scrapers.tokyohot.proxy.flaresolverr", c.Scrapers.TokyoHot.Proxy.FlareSolverr); err != nil {
			return err
		}
	}
	if c.Scrapers.AVEntertainment.Proxy != nil {
		if err := validateFlareSolverrConfig("scrapers.aventertainment.proxy.flaresolverr", c.Scrapers.AVEntertainment.Proxy.FlareSolverr); err != nil {
			return err
		}
	}
	if c.Scrapers.DLGetchu.Proxy != nil {
		if err := validateFlareSolverrConfig("scrapers.dlgetchu.proxy.flaresolverr", c.Scrapers.DLGetchu.Proxy.FlareSolverr); err != nil {
			return err
		}
	}
	if c.Scrapers.Caribbeancom.Proxy != nil {
		if err := validateFlareSolverrConfig("scrapers.caribbeancom.proxy.flaresolverr", c.Scrapers.Caribbeancom.Proxy.FlareSolverr); err != nil {
			return err
		}
	}
	if c.Scrapers.FC2.Proxy != nil {
		if err := validateFlareSolverrConfig("scrapers.fc2.proxy.flaresolverr", c.Scrapers.FC2.Proxy.FlareSolverr); err != nil {
			return err
		}
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
	if c.System.UpdateCheckIntervalHours != 0 && (c.System.UpdateCheckIntervalHours < 1 || c.System.UpdateCheckIntervalHours > 168) {
		return fmt.Errorf("system.update_check_interval_hours must be between 1 and 168 (1 week), or 0 for default")
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
	default:
		return fmt.Errorf("metadata.translation.provider must be one of: openai, deepl, google")
	}

	return nil
}

func validateHTTPBaseURL(path, raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("%s must be a valid http(s) URL", path)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%s must be a valid http(s) URL", path)
	}
	return nil
}

func validateProxyProfileConfig(c *Config) error {
	if c == nil {
		return nil
	}

	profiles := c.Scrapers.Proxy.Profiles

	if err := validateNoLegacyProxyDirectFields("scrapers.proxy", &c.Scrapers.Proxy); err != nil {
		return err
	}
	if c.Scrapers.Proxy.Enabled && c.Scrapers.Proxy.DefaultProfile == "" {
		return fmt.Errorf("scrapers.proxy.default_profile is required when scrapers.proxy.enabled is true")
	}

	if c.Scrapers.Proxy.DefaultProfile != "" {
		if _, ok := profiles[c.Scrapers.Proxy.DefaultProfile]; !ok {
			return fmt.Errorf("scrapers.proxy.default_profile references unknown profile %q", c.Scrapers.Proxy.DefaultProfile)
		}
	}

	if err := validateProxyProfileRef("scrapers.r18dev.proxy", c.Scrapers.R18Dev.Proxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.dmm.proxy", c.Scrapers.DMM.Proxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.libredmm.proxy", c.Scrapers.LibreDMM.Proxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.mgstage.proxy", c.Scrapers.MGStage.Proxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.javlibrary.proxy", c.Scrapers.JavLibrary.Proxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.javdb.proxy", c.Scrapers.JavDB.Proxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.javbus.proxy", c.Scrapers.JavBus.Proxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.jav321.proxy", c.Scrapers.Jav321.Proxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.tokyohot.proxy", c.Scrapers.TokyoHot.Proxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.aventertainment.proxy", c.Scrapers.AVEntertainment.Proxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.dlgetchu.proxy", c.Scrapers.DLGetchu.Proxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.caribbeancom.proxy", c.Scrapers.Caribbeancom.Proxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.fc2.proxy", c.Scrapers.FC2.Proxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("output.download_proxy", &c.Output.DownloadProxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.r18dev.download_proxy", c.Scrapers.R18Dev.DownloadProxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.dmm.download_proxy", c.Scrapers.DMM.DownloadProxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.libredmm.download_proxy", c.Scrapers.LibreDMM.DownloadProxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.mgstage.download_proxy", c.Scrapers.MGStage.DownloadProxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.javlibrary.download_proxy", c.Scrapers.JavLibrary.DownloadProxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.javdb.download_proxy", c.Scrapers.JavDB.DownloadProxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.javbus.download_proxy", c.Scrapers.JavBus.DownloadProxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.jav321.download_proxy", c.Scrapers.Jav321.DownloadProxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.tokyohot.download_proxy", c.Scrapers.TokyoHot.DownloadProxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.aventertainment.download_proxy", c.Scrapers.AVEntertainment.DownloadProxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.dlgetchu.download_proxy", c.Scrapers.DLGetchu.DownloadProxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.caribbeancom.download_proxy", c.Scrapers.Caribbeancom.DownloadProxy, profiles); err != nil {
		return err
	}
	if err := validateProxyProfileRef("scrapers.fc2.download_proxy", c.Scrapers.FC2.DownloadProxy, profiles); err != nil {
		return err
	}

	return nil
}

func validateProxyProfileRef(path string, proxyCfg *ProxyConfig, profiles map[string]ProxyProfile) error {
	if proxyCfg == nil {
		return nil
	}

	if err := validateNoLegacyProxyDirectFields(path, proxyCfg); err != nil {
		return err
	}

	if proxyCfg.Enabled && proxyCfg.Profile == "" {
		return fmt.Errorf("%s.profile is required when %s.enabled is true", path, path)
	}
	if proxyCfg.Profile == "" {
		return nil
	}

	if _, ok := profiles[proxyCfg.Profile]; !ok {
		return fmt.Errorf("%s.profile references unknown profile %q", path, proxyCfg.Profile)
	}
	return nil
}

func validateNoLegacyProxyDirectFields(path string, proxyCfg *ProxyConfig) error {
	if proxyCfg == nil {
		return nil
	}
	if proxyCfg.UseMainProxy {
		return fmt.Errorf("%s.use_main_proxy is no longer supported; use profile/default_profile instead", path)
	}
	if proxyCfg.URL != "" || proxyCfg.Username != "" || proxyCfg.Password != "" {
		return fmt.Errorf("%s direct proxy fields (url/username/password) are no longer supported; use profiles + profile/default_profile", path)
	}
	return nil
}

// ResolveScraperUserAgent resolves the effective User-Agent for a scraper.
// When useFakeUserAgent is true, fakeUserAgent takes precedence and falls
// back to DefaultFakeUserAgent when empty.
func ResolveScraperUserAgent(globalUserAgent string, useFakeUserAgent bool, fakeUserAgent string) string {
	if useFakeUserAgent {
		if ua := strings.TrimSpace(fakeUserAgent); ua != "" {
			return ua
		}
		return DefaultFakeUserAgent
	}

	if ua := strings.TrimSpace(globalUserAgent); ua != "" {
		return ua
	}

	return DefaultUserAgent
}

// ResolveScraperProxy returns the effective proxy config for a scraper.
// Scraper proxy usage is opt-in: a scraper override must be present and enabled.
// When enabled, proxy profiles are applied first, then missing URL/credentials
// inherit from the globally resolved proxy.
func ResolveScraperProxy(global ProxyConfig, scraperOverride *ProxyConfig) *ProxyConfig {
	// No scraper override means scraper proxy usage is disabled.
	if scraperOverride == nil || !scraperOverride.Enabled {
		return &ProxyConfig{}
	}

	globalResolved := resolveGlobalProxy(global)
	resolved := *scraperOverride

	if resolved.Profile != "" {
		applyNamedProxyProfile(&resolved, global.Profiles, resolved.Profile)
	}
	// If proxy is enabled but URL is omitted, inherit global proxy
	// credentials so users can toggle per-scraper proxy usage without
	// duplicating global proxy values.
	if resolved.URL == "" {
		resolved.URL = globalResolved.URL
		if resolved.Username == "" {
			resolved.Username = globalResolved.Username
		}
		if resolved.Password == "" {
			resolved.Password = globalResolved.Password
		}
	}
	// If scraper-specific proxy override omits FlareSolverr settings entirely,
	// inherit the global FlareSolverr config so URL/timeout are not lost.
	if isZeroFlareSolverrConfig(scraperOverride.FlareSolverr) {
		resolved.FlareSolverr = globalResolved.FlareSolverr
	}
	return &resolved
}

// ResolveGlobalProxy returns the effective global proxy config, including the
// selected default profile when configured.
func ResolveGlobalProxy(global ProxyConfig) *ProxyConfig {
	resolved := resolveGlobalProxy(global)
	return &resolved
}

func resolveGlobalProxy(global ProxyConfig) ProxyConfig {
	resolved := global
	if resolved.DefaultProfile != "" {
		applyNamedProxyProfile(&resolved, global.Profiles, resolved.DefaultProfile)
	}
	return resolved
}

func applyNamedProxyProfile(target *ProxyConfig, profiles map[string]ProxyProfile, profileName string) {
	if target == nil || profileName == "" || len(profiles) == 0 {
		return
	}
	profile, ok := profiles[profileName]
	if !ok {
		return
	}
	if profile.URL != "" {
		target.URL = profile.URL
	}
	target.Username = profile.Username
	target.Password = profile.Password
	if !isZeroFlareSolverrConfig(profile.FlareSolverr) {
		target.FlareSolverr = profile.FlareSolverr
	}
}

func isZeroFlareSolverrConfig(cfg FlareSolverrConfig) bool {
	return !cfg.Enabled &&
		cfg.URL == "" &&
		cfg.Timeout == 0 &&
		cfg.MaxRetries == 0 &&
		cfg.SessionTTL == 0
}

func validateFlareSolverrConfig(path string, cfg FlareSolverrConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.URL == "" {
		return fmt.Errorf("%s.url is required when flaresolverr is enabled", path)
	}
	if cfg.Timeout < 1 || cfg.Timeout > 300 {
		return fmt.Errorf("%s.timeout must be between 1 and 300", path)
	}
	if cfg.MaxRetries < 0 || cfg.MaxRetries > 10 {
		return fmt.Errorf("%s.max_retries must be between 0 and 10", path)
	}
	if cfg.SessionTTL < 60 || cfg.SessionTTL > 3600 {
		return fmt.Errorf("%s.session_ttl must be between 60 and 3600", path)
	}
	return nil
}
