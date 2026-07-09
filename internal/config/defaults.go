package config

import "github.com/javinizer/javinizer-go/internal/models"

// defaultScraperPriority mirrors the priority order documented in
// configs/config.yaml.example so DefaultConfig(nil, nil) and the embedded
// example agree on scraper ordering (and thus on rating_source). Previously
// this started with "dmm" while the example started with "r18dev", causing
// getFirstScraperPriorityStatic() and DefaultConfig to set rating_source to
// "dmm" despite the example documenting "r18dev".
var defaultScraperPriority = []string{
	"r18dev", "libredmm", "dmm", "javlibrary",
	"javdb", "javbus", "jav321", "mgstage", "tokyohot", "aventertainment",
	"caribbeancom", "dlgetchu", "fc2", "javstash",
}

// getFirstScraperPriorityStatic returns the first element of the hardcoded
// default scraper priority list. Callers that need dynamic (registry-based)
// priorities should inject them via DefaultConfig() instead.
func getFirstScraperPriorityStatic() string {
	if len(defaultScraperPriority) > 0 {
		return defaultScraperPriority[0]
	}
	return ""
}

// defaultServerConfig returns the default server configuration.
func defaultServerConfig() ServerConfig {
	return ServerConfig{
		Host: "localhost",
		Port: 8765,
	}
}

// defaultAPIConfig returns the default API configuration.
func defaultAPIConfig() APIConfig {
	return APIConfig{
		Security: SecurityConfig{
			AllowedDirectories: []string{},
			DeniedDirectories:  []string{},
			MaxFilesPerScan:    10000,
			ScanTimeoutSeconds: 30,
			AllowedOrigins: []string{
				"http://localhost:8765",
				"http://localhost:5173",
				"http://localhost:5174",
				"http://127.0.0.1:8765",
				"http://127.0.0.1:5173",
				"http://127.0.0.1:5174",
			},
			RateLimit: RateLimitConfig{
				RequestsPerMinute: 60,
			},
		},
	}
}

// defaultScraperConfig returns the default scraper configuration.
func defaultScraperConfig(priorities []string, defaults map[string]*models.ScraperSettings) ScrapersConfig {
	return ScrapersConfig{
		UserAgent:             "",
		Referer:               "https://www.dmm.co.jp/", // Referer header for CDN compatibility (required by DMM/R18 CDN)
		TimeoutSeconds:        30,                       // HTTP client timeout
		RequestTimeoutSeconds: 60,                       // Overall request timeout
		Priority:              priorities,               // Caller-injected scraper execution order
		FlareSolverr: models.FlareSolverrConfig{
			Enabled:    false,
			URL:        "http://localhost:8191/v1",
			Timeout:    30,
			MaxRetries: 3,
			SessionTTL: 300,
		},
		ScrapeActress: true, // Global scrape_actress default (opt-out behavior)
		Browser: models.BrowserConfig{
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
		Proxy: models.ProxyConfig{
			Enabled:        false,
			DefaultProfile: "main",
			Profiles: map[string]models.ProxyProfile{
				"main":   {URL: "", Username: "", Password: ""},
				"backup": {URL: "", Username: "", Password: ""},
			},
		},
		Overrides: defaults,
	}
}

// defaultTranslationConfig returns the default translation configuration.
func defaultTranslationConfig() TranslationConfig {
	thinkingDisabled := false
	return TranslationConfig{
		Enabled:                 false, // Opt-in to avoid API calls unless explicitly configured
		Provider:                translationProviderOpenAI,
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
			Mode:    models.DeepLModeFree,
			BaseURL: "",
			APIKey:  "",
		},
		Google: GoogleTranslationConfig{
			Mode:    models.GoogleModeFree,
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
	}
}

// defaultMetadataConfig returns the default metadata configuration.
func defaultMetadataConfig() MetadataConfig {
	return MetadataConfig{
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
		WordReplacement: WordReplacementConfig{
			Enabled: false, // Opt-in: rewrites all text fields
		},
		TagDatabase: tagDatabaseConfig{
			Enabled: false, // Opt-in feature for per-movie custom tags
		},
		R18DevDump: R18DevDumpConfig{
			Enabled: true,                         // Harmless without the dump file; activates on `javinizer dump download`
			Path:    "data/r18dev/r18dev_dump.db", // Relative to working dir, like the main DB
		},
		Translation:  defaultTranslationConfig(),
		IgnoreGenres: []string{},
		NFO:          defaultNFOConfig(),
		Completeness: defaultCompletenessConfig(),
	}
}

// defaultNFOConfig returns the default NFO configuration.
func defaultNFOConfig() NFOConfig {
	return NFOConfig{
		Feature: NFOFeatureConfig{
			Enabled:              true,
			PerFile:              false,
			IncludeFanart:        true,
			IncludeTrailer:       true,
			IncludeStreamDetails: false,
			IncludeOriginalPath:  false,
			ActressAsTag:         false,
			AddGenericRole:       false,
			AltNameRole:          false,
		},
		Format: NFOFormatConfig{
			DisplayTitle:       "<TITLE>",
			FilenameTemplate:   "<ID>.nfo",
			FirstNameOrder:     true, // NFO defaults to FirstName LastName (Kodi/Plex convention); output.first_name_order defaults to false (Japanese convention)
			ActressLanguageJA:  false,
			RatingSource:       getFirstScraperPriorityStatic(),
			Tagline:            "",
			UnknownActressMode: models.UnknownActressModeSkip,
			UnknownActressText: "Unknown",
		},
		Extra: NFOExtraConfig{},
	}
}

// defaultCompletenessConfig returns the default completeness configuration.
func defaultCompletenessConfig() completenessConfig {
	return completenessConfig{
		Enabled: false,
		Tiers: completenessTierConfig{
			Essential: completenessTierDefinition{
				Weight: 50,
				Fields: []string{"title", "poster_url", "cover_url", "actresses", "genres"},
			},
			Important: completenessTierDefinition{
				Weight: 35,
				Fields: []string{"description", "maker", "release_date", "director", "runtime", "trailer_url", "screenshot_urls"},
			},
			NiceToHave: completenessTierDefinition{
				Weight: 15,
				Fields: []string{"label", "series", "rating_score", "original_title", "translations"},
			},
		},
	}
}

// defaultOutputConfig returns the default output configuration.
func defaultOutputConfig() OutputConfig {
	return OutputConfig{
		Template: OutputTemplateConfig{
			FolderFormat:     "<ID> [<STUDIO>] - <TITLE> (<YEAR>)",
			FileFormat:       "<ID><IF:MULTIPART>-pt<PART></IF>",
			SubfolderFormat:  []string{"<ID>"},
			ActressDelimiter: ", ",
			MaxTitleLength:   100,
			MaxPathLength:    240,
			FirstNameOrder:   false, // Default to LastName FirstName (Japanese naming convention)
		},
		Operation: OutputOperationConfig{
			OperationMode:           "",
			RenameFile:              true,       // Rename files by default
			AllowRevert:             false,      // Opt-in: revert is disabled by default for safety
			GroupActress:            false,      // Don't group actresses by default
			GroupActressName:        "@Group",   // Default group name when group_actress is enabled
			GroupUnknownActressName: "@Unknown", // Default unknown-actress name when group_actress is enabled and the actress list is empty or unknown
			MoveSubtitles:           false,
			MoveFiles:               false,
			SubtitleExtensions:      []string{".srt", ".ass", ".ssa", ".smi", ".vtt"},
		},
		MediaFormat: OutputMediaFormatConfig{
			PosterFormat:      "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg",
			MaxPosterHeight:   0, // No cap — preserve source resolution
			FanartFormat:      "<ID><IF:MULTIPART>-pt<PART></IF>-fanart.jpg",
			TrailerFormat:     "<ID>-trailer.mp4",
			ScreenshotFormat:  "fanart<INDEX>.jpg",
			ScreenshotFolder:  "extrafanart",
			ScreenshotPadding: 1,
			ActressFolder:     ".actors",
			ActressFormat:     "<ACTORNAME>.jpg",
		},
		Download: OutputDownloadConfig{
			DownloadCover:       true,
			DownloadPoster:      true,
			DownloadExtrafanart: true,
			DownloadTrailer:     true,
			DownloadActress:     true,
			DownloadTimeout:     60, // 60 seconds default
			DownloadProxy: models.ProxyConfig{
				Enabled: false,
			},
		},
	}
}

// defaultLoggingConfig returns the default logging configuration.
func defaultLoggingConfig() LoggingConfig {
	return LoggingConfig{
		Level:      "info",
		Format:     "text",
		Output:     "stdout,data/logs/javinizer.log",
		MaxSizeMB:  10,
		MaxBackups: 5,
		MaxAgeDays: 0,
		Compress:   true,
	}
}

// DefaultConfig returns the default configuration. Priorities and defaults are
// injected by the caller so the config package has no dependency on scraperutil.
// Pass nil for both to use the hardcoded fallbacks (suitable for structural
// defaults that YAML unmarshaling immediately overrides).
func DefaultConfig(priorities []string, defaults map[string]*models.ScraperSettings) *Config {
	// Use injected priorities or fall back to hardcoded defaults.
	if len(priorities) == 0 {
		priorities = defaultScraperPriority
	}
	// Use injected defaults or fall back to empty map.
	if defaults == nil {
		defaults = make(map[string]*models.ScraperSettings)
	}

	return &Config{
		ConfigVersion: CurrentConfigVersion,
		Server:        defaultServerConfig(),
		API:           defaultAPIConfig(),
		Scrapers:      defaultScraperConfig(priorities, defaults),
		Metadata:      defaultMetadataConfig(),
		Matching: MatchingConfig{
			Extensions:      []string{".mp4", ".mkv", ".avi", ".wmv", ".flv"},
			MinSizeMB:       0,
			ExcludePatterns: []string{"*-trailer*", "*-sample*"},
			RegexEnabled:    false,
			RegexPattern:    `([a-zA-Z|tT28]+-\d+[zZ]?[eE]?)(?:-pt)?(\d{1,2})?`,
		},
		Output: defaultOutputConfig(),
		Database: DatabaseConfig{
			Type:     "sqlite",
			DSN:      "data/javinizer.db",
			LogLevel: "silent", // Default: no SQL query logging
		},
		Logging: defaultLoggingConfig(),
		Performance: PerformanceConfig{
			MaxWorkers:     5,
			WorkerTimeout:  300,
			BufferSize:     100,
			UpdateInterval: 100,
		},
		MediaInfo: mediaInfoConfig{
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
		// VersionCheckStableOnly is intentionally omitted: its zero value (false)
		// is the correct default (prereleases allowed). Existing configs that
		// lack the field inherit false via decodeConfig's load-into-DefaultConfig,
		// so no migration is required.
		WebUI: webUIConfig{
			DefaultReviewView: "grid-poster",
		},
	}
}
