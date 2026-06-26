// Package config — bridge index
//
// This file is the single reference for all ConfigFromAppConfig bridge functions
// that extract narrow views from *config.Config. When a config field is added,
// renamed, or moved, search this file to find every bridge that reads it.
//
// Bridge functions live in their respective packages (not in internal/config) to
// avoid import cycles — the config package cannot import downstream types like
// scanner.Config or organizer.Config. Instead, each downstream package defines its
// own bridge function and annotates it with a "Config-bridge reads:" comment
// listing the config.Config field paths it reads.
//
// Convention: every bridge function must have a doc comment line starting with
//
//	// Config-bridge reads: cfg.X.Y, cfg.A.B, ...
//
// listing every *config.Config field path it accesses (excluding nil checks).
// The function signature must accept *config.Config (or a sub-config struct
// like *config.TranslationConfig) and return the package's own narrow config type.
//
// ┌─────────────────────────────────────────────────────────────────────────────┐
// │ Bridge index — alphabetical by package                                     │
// ├──────────────────────┬──────────────────────────────────────────────────────┤
// │ Package              │ Function                                           │
// ├──────────────────────┼──────────────────────────────────────────────────────┤
// │ internal/aggregator  │ ConfigFromAppConfig                                │
// │ internal/aggregator  │ MetadataConfigFromApp                              │
// │ internal/api/core    │ ConfigFromAppConfig                                │
// │ internal/database    │ ConfigFromAppConfig (type alias)                  │
// │ internal/downloader  │ ConfigFromAppConfig                                │
// │ internal/matcher     │ ConfigFromAppConfig                                │
// │ internal/nfo         │ ConfigFromAppConfig                                │
// │ internal/nfo         │ NFONameConfigFromAppConfig                         │
// │ internal/organizer   │ ConfigFromAppConfig                                │
// │ internal/scanner     │ ConfigFromAppConfig                                │
// │ internal/scrape      │ ConfigFromAppConfig                                │
// │ internal/scrape      │ NewTranslatorFromApp                               │
// │ internal/scraper     │ ScraperRegistryConfigFromApp                       │
// │ internal/translation │ ConfigFromApp                                      │
// │ internal/workflow    │ workflowConfigFromAppConfig                        │
// └──────────────────────┴──────────────────────────────────────────────────────┘
//
// Reverse index — config field paths → bridges that read them:
//
//	cfg.API.Security.AllowedDirectories     → api/core
//	cfg.API.Security.AllowedOrigins         → api/core
//	cfg.API.Security.AllowUNC               → api/core
//	cfg.API.Security.AllowedUNCServers      → api/core
//	cfg.API.Security.DeniedDirectories      → api/core
//	cfg.API.Security.ForceSecureCookies     → api/core
//	cfg.API.Security.MaxFilesPerScan        → api/core, workflow
//	cfg.API.Security.RateLimit.RequestsPerMinute → api/core
//	cfg.API.Security.ScanTimeoutSeconds     → api/core
//	cfg.API.Security.TrustedProxies         → api/core
//	cfg.Database.DSN                        → api/core, database
//	cfg.Database.LogLevel                   → api/core, database
//	cfg.Database.Type                       → database
//	cfg.Logging.Level                       → api/core
//	cfg.Matching.ExcludePatterns            → scanner
//	cfg.Matching.Extensions                 → scanner
//	cfg.Matching.MinSizeMB                  → scanner
//	cfg.Matching.RegexEnabled               → api/core, matcher
//	cfg.Matching.RegexPattern               → api/core, matcher
//	cfg.Metadata                             → aggregator
//	cfg.Metadata.ActressDatabase.ConvertAlias → aggregator
//	cfg.Metadata.ActressDatabase.Enabled    → aggregator, scrape
//	cfg.Metadata.GenreReplacement.AutoAdd   → aggregator
//	cfg.Metadata.GenreReplacement.Enabled   → aggregator
//	cfg.Metadata.IgnoreGenres                → aggregator
//	cfg.Metadata.NFO.Feature.ActressAsTag           → nfo
//	cfg.Metadata.NFO.Format.ActressLanguageJA      → downloader, nfo
//	cfg.Metadata.NFO.Feature.AddGenericRole         → nfo
//	cfg.Metadata.NFO.Feature.AltNameRole            → nfo
//	cfg.Metadata.NFO.Extra.Credits                → nfo
//	cfg.Metadata.NFO.Format.DisplayTitle            → api/core, workflow
//	cfg.Metadata.NFO.Feature.Enabled                → nfo, workflow
//	cfg.Metadata.NFO.Format.FirstNameOrder         → downloader, nfo, organizer, workflow
//	cfg.Metadata.NFO.Format.FilenameTemplate       → api/core, nfo, workflow
//	cfg.Metadata.NFO.Feature.IncludeFanart          → nfo
//	cfg.Metadata.NFO.Feature.IncludeOriginalPath    → nfo
//	cfg.Metadata.NFO.Feature.IncludeStreamDetails   → nfo
//	cfg.Metadata.NFO.Feature.IncludeTrailer         → nfo
//	cfg.Metadata.NFO.Feature.PerFile                → api/core, nfo, workflow
//	cfg.Metadata.Priority                    → aggregator
//	cfg.Metadata.NFO.Format.RatingSource           → nfo
//	cfg.Metadata.RequiredFields              → aggregator
//	cfg.Metadata.NFO.Extra.Tag                    → nfo
//	cfg.Metadata.NFO.Format.Tagline                → nfo
//	cfg.Metadata.NFO.Format.UnknownActressMode     → aggregator, downloader, nfo
//	cfg.Metadata.NFO.Format.UnknownActressText     → aggregator, downloader, nfo
//	cfg.Metadata.WordReplacement.Enabled      → aggregator
//	cfg.Metadata.Translation                → api/core, scrape, translation
//	cfg.Metadata.Translation.Anthropic        → translation
//	cfg.Metadata.Translation.ApplyToPrimary   → translation
//	cfg.Metadata.Translation.DeepL            → translation
//	cfg.Metadata.Translation.Enabled          → scrape, translation
//	cfg.Metadata.Translation.Fields           → translation
//	cfg.Metadata.Translation.Google           → translation
//	cfg.Metadata.Translation.OpenAI           → translation
//	cfg.Metadata.Translation.OpenAICompatible → translation
//	cfg.Metadata.Translation.OverwriteExistingTarget → translation
//	cfg.Metadata.Translation.Provider          → scrape, translation
//	cfg.Metadata.Translation.SettingsHash()    → scrape
//	cfg.Metadata.Translation.SourceLanguage    → scrape, translation
//	cfg.Metadata.Translation.TargetLanguage    → scrape, translation
//	cfg.Metadata.Translation.TimeoutSeconds    → translation
//	cfg.Output.Operation.AllowRevert                  → api/core, organizer, workflow
//	cfg.Output.MediaFormat.ActressFolder                → downloader, organizer
//	cfg.Output.MediaFormat.ActressFormat                → downloader, organizer
//	cfg.Output.Template.ActressDelimiter                    → organizer
//	cfg.Output.Download.DownloadActress              → downloader, organizer
//	cfg.Output.Download.DownloadCover                → downloader, organizer
//	cfg.Output.Download.DownloadExtrafanart          → downloader, organizer
//	cfg.Output.Download.DownloadPoster               → downloader, organizer
//	cfg.Output.Download.DownloadTimeout              → downloader, workflow
//	cfg.Output.Download.DownloadTrailer              → downloader, organizer
//	cfg.Output.Download                            → api/core
//	cfg.Output.Download.DownloadProxy              → api/core
//	cfg.Output.MediaFormat.FanartFormat                 → downloader, organizer
//	cfg.Output.Template.FileFormat                   → organizer
//	cfg.Output.Template.FolderFormat                 → organizer
//	cfg.Output.GetOperationMode()           → api/core, organizer
//	cfg.Output.Operation.GroupActress                 → downloader, nfo, organizer, workflow
//	cfg.Output.Operation.GroupActressName             → downloader, nfo, organizer, workflow
//	cfg.Output.Operation.GroupUnknownActressName     → downloader, nfo, organizer, workflow
//	cfg.Output.MediaFormat.MaxPosterHeight            → api/core, downloader
//	cfg.Output.Template.MaxPathLength                → organizer
//	cfg.Output.Template                             → nfo
//	cfg.Output.Template.MaxTitleLength               → organizer
//	cfg.Output.Operation.MoveSubtitles                → organizer
//	cfg.Output.MediaFormat.PosterFormat                 → downloader, organizer
//	cfg.Output.Operation.RenameFile                   → organizer
//	cfg.Output.MediaFormat.ScreenshotFolder             → downloader, organizer
//	cfg.Output.MediaFormat.ScreenshotFormat             → downloader, organizer
//	cfg.Output.MediaFormat.ScreenshotPadding            → downloader, organizer
//	cfg.Output.Template.SubfolderFormat              → organizer
//	cfg.Output.Operation.SubtitleExtensions           → organizer
//	cfg.Output.MediaFormat.TrailerFormat                → downloader, organizer
//	cfg.Performance.MaxWorkers              → api/core
//	cfg.Performance.WorkerTimeout           → api/core
//	cfg.Scrapers.Browser                    → scraper
//	cfg.Scrapers.FlareSolverr               → api/core, scraper
//	cfg.Scrapers.Overrides                  → scraper
//	cfg.Scrapers.Priority                   → aggregator, api/core, scrape, workflow
//	cfg.Scrapers.Proxy                      → api/core, scraper, workflow
//	cfg.Scrapers.ScrapeActress              → scraper, scrape
//	cfg.Scrapers.TimeoutSeconds             → scraper
//	cfg.Scrapers.UserAgent                  → api/core, downloader, scrape
//	cfg.Scrapers.Referer                    → api/core, scrape
//	cfg.Server.Host                         → api/core
//	cfg.Server.Port                         → api/core
//	cfg.System.TempDir                      → api/core, scrape
//	cfg.System.VersionCheckEnabled          → api/core
//
// To update this index: grep for "Config-bridge reads:" across internal/ and
// rebuild the tables above.
package config
