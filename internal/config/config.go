package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
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
)

// Config represents the application configuration
type Config struct {
	Server      ServerConfig      `yaml:"server" json:"server"`
	API         APIConfig         `yaml:"api" json:"api"`
	System      SystemConfig      `yaml:"system" json:"system"`
	Scrapers    ScrapersConfig    `yaml:"scrapers" json:"scrapers"`
	Metadata    MetadataConfig    `yaml:"metadata" json:"metadata"`
	Matching    MatchingConfig    `yaml:"file_matching" json:"file_matching"`
	Output      OutputConfig      `yaml:"output" json:"output"`
	Database    DatabaseConfig    `yaml:"database" json:"database"`
	Logging     LoggingConfig     `yaml:"logging" json:"logging"`
	Performance PerformanceConfig `yaml:"performance" json:"performance"`
	MediaInfo   MediaInfoConfig   `yaml:"mediainfo" json:"mediainfo"`
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
}

// ScrapersConfig holds scraper-specific settings
type ScrapersConfig struct {
	UserAgent             string        `yaml:"user_agent" json:"user_agent"`
	Referer               string        `yaml:"referer" json:"referer"`                                 // Referer header for CDN compatibility (default: https://www.dmm.co.jp/)
	TimeoutSeconds        int           `yaml:"timeout_seconds" json:"timeout_seconds"`                 // HTTP client timeout in seconds (default: 30)
	RequestTimeoutSeconds int           `yaml:"request_timeout_seconds" json:"request_timeout_seconds"` // Overall request timeout in seconds (default: 60)
	Priority              []string      `yaml:"priority" json:"priority"`                               // Global scraper priority order
	Proxy                 ProxyConfig   `yaml:"proxy" json:"proxy"`                                     // HTTP/SOCKS5 proxy for scraper requests
	R18Dev                R18DevConfig  `yaml:"r18dev" json:"r18dev"`
	DMM                   DMMConfig     `yaml:"dmm" json:"dmm"`
	MGStage               MGStageConfig `yaml:"mgstage" json:"mgstage"`
}

// R18DevConfig holds R18.dev scraper configuration
type R18DevConfig struct {
	Enabled           bool `yaml:"enabled" json:"enabled"`
	RequestDelay      int  `yaml:"request_delay" json:"request_delay"`             // Delay between requests in milliseconds (0 = no delay)
	MaxRetries        int  `yaml:"max_retries" json:"max_retries"`                 // Maximum number of retry attempts for rate-limited requests
	RespectRetryAfter bool `yaml:"respect_retry_after" json:"respect_retry_after"` // Whether to respect Retry-After header from server
}

// DMMConfig holds DMM/Fanza scraper configuration
type DMMConfig struct {
	Enabled        bool `yaml:"enabled" json:"enabled"`
	ScrapeActress  bool `yaml:"scrape_actress" json:"scrape_actress"`
	EnableBrowser  bool `yaml:"enable_browser" json:"enable_browser"`   // Enable browser mode for video.dmm.co.jp (JavaScript rendering)
	BrowserTimeout int  `yaml:"browser_timeout" json:"browser_timeout"` // Timeout in seconds for browser operations (default: 30)
}

// MGStageConfig holds MGStage scraper configuration
type MGStageConfig struct {
	Enabled      bool `yaml:"enabled" json:"enabled"`
	RequestDelay int  `yaml:"request_delay" json:"request_delay"` // Delay between requests in milliseconds (0 = no delay)
}

// ProxyConfig holds HTTP/SOCKS5 proxy configuration
type ProxyConfig struct {
	Enabled  bool   `yaml:"enabled" json:"enabled"`   // Enable proxy for HTTP requests
	URL      string `yaml:"url" json:"url"`           // Proxy URL (e.g., "http://proxy:8080" or "socks5://proxy:1080")
	Username string `yaml:"username" json:"username"` // Optional proxy authentication username
	Password string `yaml:"password" json:"password"` // Optional proxy authentication password
}

// MetadataConfig holds metadata aggregation settings
type MetadataConfig struct {
	Priority         PriorityConfig         `yaml:"priority" json:"priority"`
	ActressDatabase  ActressDatabaseConfig  `yaml:"actress_database" json:"actress_database"`   // Actress image database (SQLite-backed)
	GenreReplacement GenreReplacementConfig `yaml:"genre_replacement" json:"genre_replacement"` // Genre replacement/normalization (SQLite-backed)
	TagDatabase      TagDatabaseConfig      `yaml:"tag_database" json:"tag_database"`           // Per-movie tag database (SQLite-backed)
	IgnoreGenres     []string               `yaml:"ignore_genres" json:"ignore_genres"`
	RequiredFields   []string               `yaml:"required_fields" json:"required_fields"`
	NFO              NFOConfig              `yaml:"nfo" json:"nfo"`
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
	Type     string `yaml:"type" json:"type"`           // sqlite, postgres, mysql
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
			UserAgent:             "Javinizer (+https://github.com/javinizer/Javinizer)",
			TimeoutSeconds:        30,                        // HTTP client timeout
			RequestTimeoutSeconds: 60,                        // Overall request timeout
			Priority:              []string{"r18dev", "dmm"}, // Global scraper execution order
			Proxy: ProxyConfig{
				Enabled: false,
				URL:     "",
			},
			R18Dev: R18DevConfig{
				Enabled: true,
			},
			DMM: DMMConfig{
				Enabled:        false, // DMM site now redirects to JavaScript-rendered site
				ScrapeActress:  false,
				BrowserTimeout: 30, // Timeout for browser operations
			},
			MGStage: MGStageConfig{
				Enabled:      false, // Opt-in, requires age verification cookie
				RequestDelay: 500,   // 500ms default delay
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
	}
}

// Validate checks configuration values for validity
func (c *Config) Validate() error {
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

	// Set default referer if not specified (for backward compatibility with old configs)
	// DMM CDN requires a referer header to avoid 403 errors
	if c.Scrapers.Referer == "" {
		c.Scrapers.Referer = "https://www.dmm.co.jp/"
	}

	// Validate referer URL format
	u, err := url.Parse(c.Scrapers.Referer)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("scrapers.referer must be a valid http(s) URL with a host")
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

	return nil
}

// Load reads configuration from a YAML file
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// If file doesn't exist, return default config
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return cfg, nil
}

// Save writes the configuration to a YAML file
func Save(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DirPermConfig); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(path, data, FilePermConfig); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// LoadOrCreate loads config from file or creates it with defaults
func LoadOrCreate(path string) (*Config, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// If file didn't exist, save the default config
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := Save(cfg, path); err != nil {
			return nil, fmt.Errorf("failed to save default config: %w", err)
		}
	}

	return cfg, nil
}
