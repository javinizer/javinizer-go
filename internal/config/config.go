package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration
type Config struct {
	Server      ServerConfig      `yaml:"server"`
	Scrapers    ScrapersConfig    `yaml:"scrapers"`
	Metadata    MetadataConfig    `yaml:"metadata"`
	Matching    MatchingConfig    `yaml:"file_matching"`
	Output      OutputConfig      `yaml:"output"`
	Database    DatabaseConfig    `yaml:"database"`
	Logging     LoggingConfig     `yaml:"logging"`
	Performance PerformanceConfig `yaml:"performance"`
}

// ServerConfig holds API server configuration
type ServerConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// ScrapersConfig holds scraper-specific settings
type ScrapersConfig struct {
	UserAgent string       `yaml:"user_agent"`
	Priority  []string     `yaml:"priority"` // Global scraper priority order
	R18Dev    R18DevConfig `yaml:"r18dev"`
	DMM       DMMConfig    `yaml:"dmm"`
}

// R18DevConfig holds R18.dev scraper configuration
type R18DevConfig struct {
	Enabled bool `yaml:"enabled"`
}

// DMMConfig holds DMM/Fanza scraper configuration
type DMMConfig struct {
	Enabled         bool `yaml:"enabled"`
	ScrapeActress   bool `yaml:"scrape_actress"`
	EnableHeadless  bool `yaml:"enable_headless"`  // Enable headless browser for video.dmm.co.jp
	HeadlessTimeout int  `yaml:"headless_timeout"` // Timeout in seconds for headless browser (default: 30)
}

// MetadataConfig holds metadata aggregation settings
type MetadataConfig struct {
	Priority       PriorityConfig `yaml:"priority"`
	ThumbCSV       CSVConfig      `yaml:"thumb_csv"`
	GenreCSV       CSVConfig      `yaml:"genre_csv"`
	IgnoreGenres   []string       `yaml:"ignore_genres"`
	RequiredFields []string       `yaml:"required_fields"`
	NFO            NFOConfig      `yaml:"nfo"`
}

// PriorityConfig defines which scraper to prefer for each field
type PriorityConfig struct {
	Actress       []string `yaml:"actress"`
	OriginalTitle []string `yaml:"original_title"`
	CoverURL      []string `yaml:"cover_url"`
	Description   []string `yaml:"description"`
	Director      []string `yaml:"director"`
	Genre         []string `yaml:"genre"`
	ID            []string `yaml:"id"`
	ContentID     []string `yaml:"content_id"`
	Label         []string `yaml:"label"`
	Maker         []string `yaml:"maker"`
	PosterURL     []string `yaml:"poster_url"`
	Rating        []string `yaml:"rating"`
	ReleaseDate   []string `yaml:"release_date"`
	Runtime       []string `yaml:"runtime"`
	Series        []string `yaml:"series"`
	ScreenshotURL []string `yaml:"screenshot_url"`
	Title         []string `yaml:"title"`
	TrailerURL    []string `yaml:"trailer_url"`
}

// CSVConfig holds CSV file configuration
type CSVConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"`
	AutoAdd bool   `yaml:"auto_add"`
}

// NFOConfig holds NFO generation settings
type NFOConfig struct {
	Enabled              bool     `yaml:"enabled"`
	DisplayName          string   `yaml:"display_name"`
	FilenameTemplate     string   `yaml:"filename_template"`
	FirstNameOrder       bool     `yaml:"first_name_order"`
	ActressLanguageJA    bool     `yaml:"actress_language_ja"`
	UnknownActressText   string   `yaml:"unknown_actress_text"`
	ActressAsTag         bool     `yaml:"actress_as_tag"`
	IncludeStreamDetails bool     `yaml:"include_stream_details"`
	IncludeFanart        bool     `yaml:"include_fanart"`
	IncludeTrailer       bool     `yaml:"include_trailer"`
	RatingSource         string   `yaml:"rating_source"`
	Tag                  []string `yaml:"tag"`
	Tagline              string   `yaml:"tagline"`
	Credits              []string `yaml:"credits"`
}

// MatchingConfig holds file matching configuration
type MatchingConfig struct {
	Extensions      []string `yaml:"extensions"`
	MinSizeMB       int      `yaml:"min_size_mb"`
	ExcludePatterns []string `yaml:"exclude_patterns"`
	RegexEnabled    bool     `yaml:"regex_enabled"`
	RegexPattern    string   `yaml:"regex_pattern"`
}

// OutputConfig holds output/organization settings
type OutputConfig struct {
	FolderFormat        string   `yaml:"folder_format"`
	FileFormat          string   `yaml:"file_format"`
	SubfolderFormat     []string `yaml:"subfolder_format"`
	Delimiter           string   `yaml:"delimiter"`
	DownloadCover       bool     `yaml:"download_cover"`
	DownloadPoster      bool     `yaml:"download_poster"`
	DownloadExtrafanart bool     `yaml:"download_extrafanart"`
	DownloadTrailer     bool     `yaml:"download_trailer"`
	DownloadActress     bool     `yaml:"download_actress"`
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Type string `yaml:"type"` // sqlite, postgres, mysql
	DSN  string `yaml:"dsn"`  // Data Source Name
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string `yaml:"level"`  // debug, info, warn, error
	Format string `yaml:"format"` // json, text
	Output string `yaml:"output"` // stdout, file path
}

// PerformanceConfig holds performance and concurrency settings
type PerformanceConfig struct {
	MaxWorkers     int `yaml:"max_workers"`     // Maximum concurrent workers (default: 5)
	WorkerTimeout  int `yaml:"worker_timeout"`  // Timeout per task in seconds (default: 300)
	BufferSize     int `yaml:"buffer_size"`     // Channel buffer size (default: 100)
	UpdateInterval int `yaml:"update_interval"` // UI update interval in milliseconds (default: 100)
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Host: "localhost",
			Port: 8080,
		},
		Scrapers: ScrapersConfig{
			UserAgent: "Javinizer (+https://github.com/javinizer/Javinizer)",
			Priority:  []string{"r18dev", "dmm"}, // Global scraper execution order
			R18Dev: R18DevConfig{
				Enabled: true,
			},
			DMM: DMMConfig{
				Enabled:       false, // DMM site now redirects to JavaScript-rendered site
				ScrapeActress: false,
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
			ThumbCSV: CSVConfig{
				Enabled: true,
				Path:    "data/actress.csv",
				AutoAdd: true,
			},
			GenreCSV: CSVConfig{
				Enabled: true,
				Path:    "data/genres.csv",
				AutoAdd: true,
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
			DownloadCover:       true,
			DownloadPoster:      true,
			DownloadExtrafanart: false,
			DownloadTrailer:     false,
			DownloadActress:     false,
		},
		Database: DatabaseConfig{
			Type: "sqlite",
			DSN:  "data/javinizer.db",
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
	}
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
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
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

	// If file didn't exist, save the default config
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := Save(cfg, path); err != nil {
			return nil, fmt.Errorf("failed to save default config: %w", err)
		}
	}

	return cfg, nil
}
