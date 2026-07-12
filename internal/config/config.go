package config

import (
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"gopkg.in/yaml.v3"
)

// Package configuration constants: config schema version, file/directory permissions, default HTTP user agents, and temp directory.
const (
	CurrentConfigVersion = 3

	// DirPerm is the default permission for directory creation.
	DirPerm = 0777
	// DirPermTemp is the permission for temporary directory creation.
	DirPermTemp = 0700
	// FilePerm is the default permission for file creation.
	FilePerm = 0666

	// DefaultUserAgent is the true/identifying UA for Javinizer.
	DefaultUserAgent = "Javinizer (+https://github.com/javinizer/javinizer-go)"

	// DefaultScraperUserAgent is a browser-like UA used as the default for scrapers
	// when no scraper-specific or global user_agent is configured.
	DefaultScraperUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"

	DefaultTempDir = "data/temp"
)

var cachedUmask atomic.Int32

func init() {
	cachedUmask.Store(0)
}

// StoreUmask caches the provided umask value for later use.
func StoreUmask(mask int) {
	cachedUmask.Store(int32(mask))
}

// ValidateHTTPBaseURL checks that raw is a valid HTTP or HTTPS URL.
// An empty string is accepted (the field is optional).
func ValidateHTTPBaseURL(path, raw string) error {
	if raw == "" {
		return nil
	}
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return fmt.Errorf("%s must be a valid HTTP or HTTPS URL", path)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s must be a valid URL: %w", path, err)
	}
	// url.Parse accepts "http://" (no host) and "http:///path". Require a host
	// so a base URL with no authority is rejected rather than silently accepted.
	if u.Host == "" {
		return fmt.Errorf("%s must be a valid HTTP or HTTPS URL with a host", path)
	}
	return nil
}

// ValidateScraperBaseURL validates a configurable scraper base URL and enforces
// that its host is on the source's allow-list. A generic HTTP base-URL check is
// not enough for scraper egress: this setting steers outbound requests, so a
// user-set base_url pointing at an arbitrary host (or a loopback/private host)
// must be rejected before use. allowedHosts is the set of hosts the scraper is
// permitted to talk to (e.g. dmm.co.jp, www.dmm.co.jp, video.dmm.co.jp). Host
// comparison is case-insensitive. An empty raw value is allowed (the scraper
// falls back to its compiled-in default).
func ValidateScraperBaseURL(path, raw string, allowedHosts []string) error {
	if err := ValidateHTTPBaseURL(path, raw); err != nil {
		return err
	}
	if raw == "" {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("%s must be a valid URL: %w", path, err)
	}
	host := strings.ToLower(u.Hostname())
	for _, allowed := range allowedHosts {
		if host == strings.ToLower(strings.TrimSpace(allowed)) {
			return nil
		}
	}
	return fmt.Errorf("%s host %q is not in the allowed list for this scraper: %s", path, u.Hostname(), strings.Join(allowedHosts, ", "))
}

type webUIConfig struct {
	DefaultReviewView string          `yaml:"default_review_view" json:"default_review_view"`
	Favorites         FavoritesConfig `yaml:"favorites" json:"favorites"`
}

// FavoritesConfig holds user-curated quick-apply lists surfaced in the web UI.
// Genre favorites back the "quick apply" workflow on the Genres page.
type FavoritesConfig struct {
	Genre []string `yaml:"genre" json:"genre"`
}

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
	MediaInfo     mediaInfoConfig   `yaml:"mediainfo" json:"mediainfo"`
	WebUI         webUIConfig       `yaml:"webui" json:"webui"`
	Warnings      []ConfigWarning   `yaml:"-" json:"warnings,omitempty"`
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

// RateLimitConfig holds API rate limiting settings.
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
	// VersionCheckStableOnly, when true, restricts update notifications to
	// stable releases only (prereleases are still fetched and cached for
	// transparency but never reported as available). Defaults to false: with
	// v1.0.0 stable as the latest release, suppressing prereleases by default
	// still surfaces stable updates, and users who want release candidates can
	// opt in via --prerelease. Set to true to be notified only about stable
	// releases.
	// Modeled as an opt-in restriction (not an opt-out) so the zero value is
	// the correct default for existing configs — no migration needed.
	VersionCheckStableOnly bool `yaml:"version_check_stable_only" json:"version_check_stable_only"`
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
		"webui":          c.WebUI,
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

// OutputTemplateConfig controls filename and folder formatting.
type OutputTemplateConfig struct {
	FolderFormat     string   `yaml:"folder_format" json:"folder_format"`
	FileFormat       string   `yaml:"file_format" json:"file_format"`
	SubfolderFormat  []string `yaml:"subfolder_format" json:"subfolder_format"`
	ActressDelimiter string   `yaml:"actress_delimiter" json:"actress_delimiter"` // Delimiter between actress names when joining <ACTORS>/<ACTRESSES> with no in-tag DELIM= modifier (default: ", ")
	// LegacyDelimiter is a legacy alias for actress_delimiter retained for
	// backward compatibility with configs written before the rename. It is
	// omitted from JSON output and copied into ActressDelimiter during
	// Normalize when the new key is unset and the legacy one is not.
	// backward compatibility with configs written before the rename. It is
	// omitted from JSON output and copied into ActressDelimiter during
	// Normalize when the new key is unset and the legacy one is not.
	// Exported because yaml.v3 cannot decode into unexported fields when
	// UnmarshalYAML uses a type-alias decode path.
	LegacyDelimiter   string `yaml:"delimiter,omitempty" json:"-"`
	MaxTitleLength    int    `yaml:"max_title_length" json:"max_title_length"`
	MaxPathLength     int    `yaml:"max_path_length" json:"max_path_length"`
	FirstNameOrder    bool   `yaml:"first_name_order" json:"first_name_order"`       // true = FirstName LastName, false = LastName FirstName (default: false)
	ActressLanguageJA bool   `yaml:"actress_language_ja" json:"actress_language_ja"` // true = prefer JapaneseName over First/Last for <ACTORS>/<ACTRESS> in folder/file naming (default: false), mirrors nfo.actress_language_ja; tag-level <ACTORS:JA> still takes precedence
}

// OutputOperationConfig controls file operations and revert behavior.
type OutputOperationConfig struct {
	OperationMode           operationmode.OperationMode `yaml:"operation_mode" json:"operation_mode"`
	RenameFile              bool                        `yaml:"rename_file" json:"rename_file"`                               // Rename files using file_format template (default: true)
	AllowRevert             bool                        `yaml:"allow_revert" json:"allow_revert"`                             // Enable revert operations (default: false — opt-in for safety)
	GroupActress            bool                        `yaml:"group_actress" json:"group_actress"`                           // Replace multiple actresses with group name in templates (default: false)
	GroupActressName        string                      `yaml:"group_actress_name" json:"group_actress_name"`                 // Folder name when group_actress is enabled and multiple actresses (default: "@Group")
	GroupUnknownActressName string                      `yaml:"group_unknown_actress_name" json:"group_unknown_actress_name"` // Folder name when group_actress is enabled and the actress list is empty or unknown (default: "@Unknown")
	MoveSubtitles           bool                        `yaml:"move_subtitles" json:"move_subtitles"`
	MoveFiles               bool                        `yaml:"move_files" json:"move_files"` // Move files instead of copying (default: false / copy)
	SubtitleExtensions      []string                    `yaml:"subtitle_extensions" json:"subtitle_extensions"`
}

// OutputMediaFormatConfig controls media filename templates.
type OutputMediaFormatConfig struct {
	PosterFormat      string `yaml:"poster_format" json:"poster_format"`
	MaxPosterHeight   int    `yaml:"max_poster_height" json:"max_poster_height"` // Max height in px for cropped posters; 0 = no cap (preserve source resolution). When the cropped poster exceeds this height it is downscaled preserving aspect ratio.
	FanartFormat      string `yaml:"fanart_format" json:"fanart_format"`
	TrailerFormat     string `yaml:"trailer_format" json:"trailer_format"`
	ScreenshotFormat  string `yaml:"screenshot_format" json:"screenshot_format"`
	ScreenshotFolder  string `yaml:"screenshot_folder" json:"screenshot_folder"`
	ScreenshotPadding int    `yaml:"screenshot_padding" json:"screenshot_padding"`
	ActressFolder     string `yaml:"actress_folder" json:"actress_folder"`
	ActressFormat     string `yaml:"actress_format" json:"actress_format"`
}

// OutputDownloadConfig controls which media types to download.
type OutputDownloadConfig struct {
	DownloadCover       bool               `yaml:"download_cover" json:"download_cover"`
	DownloadPoster      bool               `yaml:"download_poster" json:"download_poster"`
	DownloadExtrafanart bool               `yaml:"download_extrafanart" json:"download_extrafanart"`
	DownloadTrailer     bool               `yaml:"download_trailer" json:"download_trailer"`
	DownloadActress     bool               `yaml:"download_actress" json:"download_actress"`
	DownloadTimeout     int                `yaml:"download_timeout" json:"download_timeout"` // Timeout in seconds for HTTP downloads (default: 60)
	DownloadProxy       models.ProxyConfig `yaml:"download_proxy" json:"download_proxy"`     // Separate proxy for downloads (optional)
}

// OutputConfig holds output/organization settings.
// Fields are grouped into named sub-structs using yaml:",inline" so the
// YAML format stays flat (output.folder_format) while the Go type system
// provides named access groups (cfg.Output.Template.FolderFormat).
type OutputConfig struct {
	Template    OutputTemplateConfig    `yaml:",inline"`
	Operation   OutputOperationConfig   `yaml:",inline"`
	MediaFormat OutputMediaFormatConfig `yaml:",inline"`
	Download    OutputDownloadConfig    `yaml:",inline"`
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

// mediaInfoConfig holds MediaInfo functionality configuration
type mediaInfoConfig struct {
	CLIEnabled bool   `yaml:"cli_enabled" json:"cli_enabled"` // Enable MediaInfo CLI fallback (default: false)
	CLIPath    string `yaml:"cli_path" json:"cli_path"`       // Path to mediainfo binary (default: "mediainfo")
	CLITimeout int    `yaml:"cli_timeout" json:"cli_timeout"` // Timeout in seconds for CLI execution (default: 30)
}

// Validate checks configuration values for validity.
// Structural/field-level checks run first, then cross-field validators in sequence.
func (c *Config) Validate() error {
	c.Scrapers.Normalize()
	if err := ValidateConfig(c); err != nil {
		return err
	}
	c.RecomputeWarnings()
	return nil
}

// RecomputeWarnings updates Config.Warnings based on the current scraper
// override state. Call after Validate (initial pass) and after Finalize
// (which populates scraper defaults). Safe to call multiple times.
func (c *Config) RecomputeWarnings() {
	c.Warnings = ValidatePriorityOverrides(c)
}

// ValidateConfig validates a Config without mutating it.
// Extracted from Config.Validate so that pure validation can be tested
// independently of the Normalize side-effect.
func ValidateConfig(cfg *Config) error {
	// Validate a clone so the delegated validators (which call
	// Scrapers.Normalize to populate/repair state) cannot mutate the caller's
	// Config. ValidateConfig is documented as pure/non-mutating; without this
	// clone, direct callers would observe normalization side-effects.
	cfg = cfg.Clone()

	// --- Structural / field-level checks ---

	// Validate database settings
	dbType := strings.ToLower(strings.TrimSpace(cfg.Database.Type))
	if dbType == "" {
		// Backward compatibility: treat empty type as sqlite default.
		dbType = "sqlite"
	}
	if dbType != "sqlite" {
		return fmt.Errorf("database.type must be 'sqlite' (currently only sqlite is supported)")
	}

	if strings.TrimSpace(cfg.Database.DSN) == "" {
		return fmt.Errorf("database.dsn is required")
	}

	// Validate scraper timeouts
	if cfg.Scrapers.TimeoutSeconds < 1 || cfg.Scrapers.TimeoutSeconds > 300 {
		return fmt.Errorf("scrapers.timeout_seconds must be between 1 and 300")
	}
	if cfg.Scrapers.RequestTimeoutSeconds < 1 || cfg.Scrapers.RequestTimeoutSeconds > 600 {
		return fmt.Errorf("scrapers.request_timeout_seconds must be between 1 and 600")
	}

	// Validate FlareSolverr config (global)
	if err := cfg.Scrapers.FlareSolverr.Validate("scrapers.flaresolverr"); err != nil {
		return err
	}

	// Validate Browser config (global)
	if err := cfg.Scrapers.Browser.Validate("scrapers.browser"); err != nil {
		return err
	}

	// Validate referer URL format
	referer := strings.TrimSpace(cfg.Scrapers.Referer)
	if referer == "" {
		// Backward compatibility with old configs that omitted referer.
		referer = "https://www.dmm.co.jp/"
	}
	u, err := url.Parse(referer)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("scrapers.referer must be a valid http(s) URL with a host")
	}

	// Validate performance settings
	if cfg.Performance.MaxWorkers < 1 || cfg.Performance.MaxWorkers > 100 {
		return fmt.Errorf("performance.max_workers must be between 1 and 100")
	}
	if cfg.Performance.WorkerTimeout < 10 || cfg.Performance.WorkerTimeout > 3600 {
		return fmt.Errorf("performance.worker_timeout must be between 10 and 3600")
	}
	if cfg.Performance.UpdateInterval < 10 || cfg.Performance.UpdateInterval > 5000 {
		return fmt.Errorf("performance.update_interval must be between 10 and 5000")
	}

	// Validate update settings
	// Allow 0 to mean "use default" (handled by DefaultConfig and migrations)
	if cfg.System.VersionCheckIntervalHours != 0 && (cfg.System.VersionCheckIntervalHours < 1 || cfg.System.VersionCheckIntervalHours > 168) {
		return fmt.Errorf("system.version_check_interval_hours must be between 1 and 168 (1 week), or 0 for default")
	}

	// Validate logging rotation settings
	if cfg.Logging.MaxSizeMB < 0 {
		return fmt.Errorf("logging.max_size_mb must be >= 0")
	}
	if cfg.Logging.MaxBackups < 0 {
		return fmt.Errorf("logging.max_backups must be >= 0")
	}
	if cfg.Logging.MaxAgeDays < 0 {
		return fmt.Errorf("logging.max_age_days must be >= 0")
	}

	if v := cfg.WebUI.DefaultReviewView; v != "" {
		switch v {
		case "detail", "grid-poster", "grid-cover":
		default:
			return fmt.Errorf("webui.default_review_view must be one of: detail, grid-poster, grid-cover")
		}
	}

	// Operation mode feeds workflow construction; a config typo must fail closed
	// instead of silently defaulting to organize (which would run the wrong
	// file-operation mode). Empty is allowed (defaults to organize elsewhere).
	if rawMode := strings.TrimSpace(string(cfg.Output.Operation.OperationMode)); rawMode != "" {
		if _, err := operationmode.ParseOperationMode(rawMode); err != nil {
			return fmt.Errorf("output.operation_mode is invalid: %w", err)
		}
	}

	// --- Cross-field validators ---

	if err := ValidateScraperOverrides(cfg); err != nil {
		return err
	}
	if err := ValidateProxyProfiles(cfg); err != nil {
		return err
	}
	if err := ValidateTranslationProvider(cfg); err != nil {
		return err
	}

	return nil
}

// validateTranslationConfig is the internal implementation for ValidateTranslationProvider.
//
//nolint:unused
func (c *Config) validateTranslationConfig() error {
	return validateTranslationProviderInternal(c)
}
