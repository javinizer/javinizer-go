package config

import (
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

var defaultScraperPriority = []string{
	"dmm", "r18dev", "libredmm", "mgstage", "javlibrary",
	"javdb", "javbus", "jav321", "tokyohot", "aventertainment",
	"dlgetchu", "caribbeancom", "fc2", "javstash",
}

func getScraperPriorities() []string {
	priorities := scraperutil.GetPriorities()
	if len(priorities) > 0 {
		return priorities
	}
	return defaultScraperPriority
}

func getFirstScraperPriority() string {
	priorities := scraperutil.GetPriorities()
	if len(priorities) > 0 {
		return priorities[0]
	}
	if len(defaultScraperPriority) > 0 {
		return defaultScraperPriority[0]
	}
	return ""
}

// buildDefaultsFromRegistry constructs scraper defaults from registered scrapers.
// This eliminates hardcoded scraper names from config.go.
// Uses scraperutil to avoid import cycle (config -> scraperutil, scraper -> scraperutil).
func buildDefaultsFromRegistry() map[string]*ScraperSettings {
	defaults := scraperutil.GetDefaultScraperSettings()
	result := make(map[string]*ScraperSettings, len(defaults))
	for name, settings := range defaults {
		// Type-assert from any to ScraperSettings and deep copy to isolate
		if s, ok := settings.(ScraperSettings); ok {
			result[name] = s.DeepCopy()
			continue
		}
		if s, ok := settings.(*ScraperSettings); ok && s != nil {
			result[name] = s.DeepCopy()
		}
	}
	return result
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	thinkingDisabled := false

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
				AllowedOrigins: []string{
					"http://localhost:8080",
					"http://localhost:5173",
					"http://127.0.0.1:8080",
					"http://127.0.0.1:5173",
				},
			},
		},
		Scrapers: ScrapersConfig{
			UserAgent:             DefaultUserAgent,
			Referer:               "https://www.dmm.co.jp/", // Referer header for CDN compatibility (required by DMM/R18 CDN)
			TimeoutSeconds:        30,                       // HTTP client timeout
			RequestTimeoutSeconds: 60,                       // Overall request timeout
			Priority:              getScraperPriorities(),   // Global scraper execution order
			FlareSolverr: FlareSolverrConfig{
				Enabled:    false,
				URL:        "http://localhost:8191/v1",
				Timeout:    30,
				MaxRetries: 3,
				SessionTTL: 300,
			},
			// NEW: Global scrape_actress default (opt-out behavior)
			ScrapeActress: true,
			// NEW: Global Browser configuration
			Browser: BrowserConfig{
				Enabled:      false, // Opt-in
				BinaryPath:   "",    // Auto-discovered if empty
				Timeout:      30,
				MaxRetries:   3,
				Headless:     true,
				StealthMode:  true,
				WindowWidth:  1920,
				WindowHeight: 1080,
				SlowMo:       0,
				BlockImages:  true,
				BlockCSS:     false,
				UserAgent:    "",
				DebugVisible: false,
			},
			Proxy: ProxyConfig{
				Enabled:        false,
				DefaultProfile: "main",
				Profiles: map[string]ProxyProfile{
					"main":   {URL: "", Username: "", Password: ""},
					"backup": {URL: "", Username: "", Password: ""},
				},
			},
			Overrides: buildDefaultsFromRegistry(),
		},
		Metadata: MetadataConfig{
			Priority: PriorityConfig{
				Priority: nil, // Derived from registered scraper priorities at runtime
			},
			ActressDatabase: ActressDatabaseConfig{
				Enabled:      true,
				AutoAdd:      true,
				ConvertAlias: false,
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
				SourceLanguage:          "ja", // Japanese content translated to English
				TargetLanguage:          "en",
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
				OpenAICompatible: OpenAICompatibleTranslationConfig{
					BaseURL:        "http://localhost:11434/v1",
					APIKey:         "",
					Model:          "",
					EnableThinking: &thinkingDisabled,
				},
				Anthropic: AnthropicTranslationConfig{
					BaseURL: "https://api.anthropic.com",
					APIKey:  "",
					Model:   "claude-sonnet-4-20250514",
				},
			},
			IgnoreGenres: []string{},
			NFO: NFOConfig{
				Enabled:              true,
				DisplayTitle:         "<TITLE>",
				FilenameTemplate:     "<ID>.nfo",
				FirstNameOrder:       true,
				ActressLanguageJA:    false,
				PerFile:              false,
				UnknownActressText:   "Unknown",
				ActressAsTag:         false,
				AddGenericRole:       false,
				AltNameRole:          false,
				IncludeOriginalPath:  false,
				IncludeStreamDetails: false,
				IncludeFanart:        true,
				IncludeTrailer:       true,
				RatingSource:         getFirstScraperPriority(),
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
			FileFormat:          "<ID><IF:MULTIPART>-pt<PART></IF>",
			SubfolderFormat:     []string{"<ID>"},
			Delimiter:           ", ",
			MaxTitleLength:      100,
			MaxPathLength:       240,
			MoveSubtitles:       false,
			SubtitleExtensions:  []string{".srt", ".ass", ".ssa", ".smi", ".vtt"},
			OperationMode:       "",
			RenameFolderInPlace: false,
			MoveToFolder:        true,  // Move to organized folders by default
			RenameFile:          true,  // Rename files by default
			AllowRevert:         false, // Opt-in: revert is disabled by default for safety
			GroupActress:        false, // Don't group actresses by default
			PosterFormat:        "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg",
			FanartFormat:        "<ID><IF:MULTIPART>-pt<PART></IF>-fanart.jpg",
			TrailerFormat:       "<ID>-trailer.mp4",
			ScreenshotFormat:    "fanart<INDEX>.jpg",
			ScreenshotFolder:    "extrafanart",
			ScreenshotPadding:   1,
			ActressFolder:       ".actors",
			ActressFormat:       "<ACTORNAME>.jpg",
			DownloadCover:       true,
			DownloadPoster:      true,
			DownloadExtrafanart: true,
			DownloadTrailer:     true,
			DownloadActress:     true,
			DownloadTimeout:     60, // 60 seconds default
			DownloadProxy: ProxyConfig{
				Enabled: false,
			},
		},
		Database: DatabaseConfig{
			Type:     "sqlite",
			DSN:      "data/javinizer.db",
			LogLevel: "silent", // Default: no SQL query logging
		},
		Logging: LoggingConfig{
			Level:      "info",
			Format:     "text",
			Output:     "stdout,data/logs/javinizer.log",
			MaxSizeMB:  10,
			MaxBackups: 5,
			MaxAgeDays: 0,
			Compress:   true,
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
			Umask:                     "002",
			VersionCheckEnabled:       true,
			VersionCheckIntervalHours: 24,
			TempDir:                   DefaultTempDir,
		},
	}
}
