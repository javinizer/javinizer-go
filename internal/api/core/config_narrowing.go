package core

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// APIConfig holds the subset of application configuration consumed by the API layer.
// Instead of reaching 3-4 levels deep into *config.Config, handlers read from this
// narrow struct. A change to config structure changes one ConfigFromAppConfig() function,
// not 428 scattered access sites.
type APIConfig struct {
	// Security — path validation, scan limits, CORS, auth
	AllowedDirectories []string // cfg.API.Security.AllowedDirectories
	DeniedDirectories  []string // cfg.API.Security.DeniedDirectories
	MaxFilesPerScan    int      // cfg.API.Security.MaxFilesPerScan
	ScanTimeoutSeconds int      // cfg.API.Security.ScanTimeoutSeconds
	AllowedOrigins     []string // cfg.API.Security.AllowedOrigins
	AllowUNC           bool     // cfg.API.Security.AllowUNC
	AllowedUNCServers  []string // cfg.API.Security.AllowedUNCServers
	RateLimitRPM       int      // cfg.API.Security.RateLimit.RequestsPerMinute
	TrustedProxies     []string // cfg.API.Security.TrustedProxies
	ForceSecureCookies bool     // cfg.API.Security.ForceSecureCookies

	// Server
	Host string // cfg.Server.Host
	Port int    // cfg.Server.Port

	// Scrapers
	ScraperPriority     []string                  // cfg.Scrapers.Priority
	ScraperUserAgent    string                    // cfg.Scrapers.UserAgent
	ScraperReferer      string                    // cfg.Scrapers.Referer
	ProxyConfig         models.ProxyConfig        // cfg.Scrapers.Proxy — whole struct for proxy test/validation
	FlareSolverrConfig  models.FlareSolverrConfig // cfg.Scrapers.FlareSolverr — whole struct for proxy test/validation
	DownloadProxyConfig models.ProxyConfig        // cfg.Output.Download.DownloadProxy — download-proxy profiles surfaced as download_proxy.profile choices

	// Metadata
	NFOEnabled          bool                     // cfg.Metadata.NFO.Feature.Enabled
	NFOFilenameTemplate string                   // cfg.Metadata.NFO.Format.FilenameTemplate
	NFOPerFile          bool                     // cfg.Metadata.NFO.Feature.PerFile
	NFODisplayTitle     string                   // cfg.Metadata.NFO.Format.DisplayTitle
	MaxPosterHeight     int                      // cfg.Output.MediaFormat.MaxPosterHeight (0 = no cap)
	TranslationConfig   config.TranslationConfig // cfg.Metadata.Translation — whole struct for validation in config update handler

	// Output
	OperationMode string // cfg.Output.GetOperationMode() — resolved method call, stored as string
	AllowRevert   bool   // cfg.Output.Operation.AllowRevert

	// Performance
	MaxWorkers    int           // cfg.Performance.MaxWorkers
	WorkerTimeout time.Duration // cfg.Performance.WorkerTimeout converted to time.Duration

	// Matching
	RegexEnabled bool   // cfg.Matching.RegexEnabled
	RegexPattern string // cfg.Matching.RegexPattern

	// System
	TempDir             string // cfg.System.TempDir
	VersionCheckEnabled bool   // cfg.System.VersionCheckEnabled

	// Logging
	LogLevel string // cfg.Logging.Level

	// Database (for test helpers that construct DB from config)
	DatabaseDSN      string // cfg.Database.DSN
	DatabaseLogLevel string // cfg.Database.LogLevel
}

// SecurityNarrowConfig holds only the security fields consumed by API-layer handlers
// (path validation, auth cookie helpers, autocomplete).
type SecurityNarrowConfig struct {
	AllowedDirectories []string
	DeniedDirectories  []string
	AllowUNC           bool
	AllowedUNCServers  []string
	ForceSecureCookies bool
	TrustedProxies     []string
}

// BatchNarrowConfig holds the fields consumed by batch processing handlers.
// Batch handlers read these fields (plus SecurityNarrowConfig for path validation)
// instead of the full APIConfig, reducing coupling to ~10 fields from 42.
type BatchNarrowConfig struct {
	OperationMode      string        // resolved operation mode for batch execution
	MaxWorkers         int           // worker pool concurrency
	WorkerTimeout      time.Duration // worker execution timeout
	ScraperPriority    []string      // scraper source ordering
	NFOEnabled         bool          // whether NFO generation is active
	ScraperUserAgent   string        // user-agent for poster downloads
	ScraperReferer     string        // referer for poster downloads
	ScanTimeoutSeconds int           // timeout for file discovery
	MaxFilesPerScan    int           // max files per scan operation
	TempDir            string        // temp directory for batch artifacts
	MaxPosterHeight    int           // cfg.Output.MediaFormat.MaxPosterHeight (0 = no cap)
}

// ScannerNarrowConfig holds the fields consumed by scanner/file handlers.
// These handlers also use SecurityNarrowConfig for path validation.
type ScannerNarrowConfig struct {
	ScanTimeoutSeconds int      // timeout for scan operations
	MaxFilesPerScan    int      // max files per scan
	AllowedDirectories []string // allowed scan directories (first entry = default path)
}

// TempNarrowConfig holds the fields consumed by temp handlers that serve
// ephemeral poster images and proxy remote images for the preview UI.
type TempNarrowConfig struct {
	TempDir          string // temp directory for batch artifacts
	ScraperUserAgent string // user-agent for proxied image requests
	ScraperReferer   string // referer for proxied image requests
}

// MatcherNarrowConfig holds the fields consumed by the runtime manager
// for constructing a matcher.MatcherInterface from the APIConfig snapshot.
type MatcherNarrowConfig struct {
	RegexEnabled bool
	RegexPattern string
}

// SecurityConfig returns the narrow security config consumed by API-layer handlers.
func (c APIConfig) SecurityConfig() *SecurityNarrowConfig {
	return &SecurityNarrowConfig{
		AllowedDirectories: c.AllowedDirectories,
		DeniedDirectories:  c.DeniedDirectories,
		AllowUNC:           c.AllowUNC,
		AllowedUNCServers:  c.AllowedUNCServers,
		ForceSecureCookies: c.ForceSecureCookies,
		TrustedProxies:     c.TrustedProxies,
	}
}

// BatchConfig returns the narrow batch-processing config consumed by batch handlers.
// Batch handlers should also call SecurityConfig() for path validation.
func (c APIConfig) BatchConfig() *BatchNarrowConfig {
	return &BatchNarrowConfig{
		OperationMode:      c.OperationMode,
		MaxWorkers:         c.MaxWorkers,
		WorkerTimeout:      c.WorkerTimeout,
		ScraperPriority:    c.ScraperPriority,
		NFOEnabled:         c.NFOEnabled,
		ScraperUserAgent:   c.ScraperUserAgent,
		ScraperReferer:     c.ScraperReferer,
		ScanTimeoutSeconds: c.ScanTimeoutSeconds,
		MaxFilesPerScan:    c.MaxFilesPerScan,
		TempDir:            c.TempDir,
		MaxPosterHeight:    c.MaxPosterHeight,
	}
}

// ScannerConfig returns the narrow scanner/file config consumed by file handlers.
// File handlers should also call SecurityConfig() for path validation.
func (c APIConfig) ScannerConfig() *ScannerNarrowConfig {
	return &ScannerNarrowConfig{
		ScanTimeoutSeconds: c.ScanTimeoutSeconds,
		MaxFilesPerScan:    c.MaxFilesPerScan,
		AllowedDirectories: c.AllowedDirectories,
	}
}

// TempConfig returns the narrow temp config consumed by temp handlers
// that serve ephemeral poster images and proxy remote images.
func (c APIConfig) TempConfig() *TempNarrowConfig {
	return &TempNarrowConfig{
		TempDir:          c.TempDir,
		ScraperUserAgent: c.ScraperUserAgent,
		ScraperReferer:   c.ScraperReferer,
	}
}

// MatcherConfig returns the narrow matcher config consumed by the runtime
// manager for constructing a matcher from the APIConfig snapshot.
func (c APIConfig) MatcherConfig() *MatcherNarrowConfig {
	return &MatcherNarrowConfig{
		RegexEnabled: c.RegexEnabled,
		RegexPattern: c.RegexPattern,
	}
}

// ConfigFromAppConfig extracts API-relevant fields from the application config.
// Returns a zero-value APIConfig when cfg is nil.
//
// Config-bridge reads: cfg.API.Security.AllowedDirectories, cfg.API.Security.DeniedDirectories,
// cfg.API.Security.MaxFilesPerScan, cfg.API.Security.ScanTimeoutSeconds,
// cfg.API.Security.AllowedOrigins, cfg.API.Security.AllowUNC,
// cfg.API.Security.AllowedUNCServers, cfg.API.Security.RateLimit.RequestsPerMinute,
// cfg.API.Security.TrustedProxies, cfg.API.Security.ForceSecureCookies,
// cfg.Server.Host, cfg.Server.Port,
// cfg.Scrapers.Priority, cfg.Scrapers.UserAgent, cfg.Scrapers.Referer,
// cfg.Scrapers.Proxy, cfg.Scrapers.FlareSolverr,
// cfg.Metadata.NFO.Feature.Enabled, cfg.Metadata.NFO.Format.FilenameTemplate,
// cfg.Metadata.NFO.Feature.PerFile, cfg.Metadata.NFO.Format.DisplayTitle,
// cfg.Metadata.Translation,
// cfg.Output.GetOperationMode(), cfg.Output.Operation.AllowRevert,
// cfg.Output.MediaFormat.MaxPosterHeight,
// cfg.Output.Download, cfg.Output.Download.DownloadProxy,
// cfg.Performance.MaxWorkers, cfg.Performance.WorkerTimeout,
// cfg.Matching.RegexEnabled, cfg.Matching.RegexPattern,
// cfg.System.TempDir, cfg.System.VersionCheckEnabled,
// cfg.Logging.Level, cfg.Database.DSN, cfg.Database.LogLevel
func ConfigFromAppConfig(cfg *config.Config) APIConfig {
	if cfg == nil {
		return APIConfig{}
	}
	return APIConfig{
		AllowedDirectories:  cfg.API.Security.AllowedDirectories,
		DeniedDirectories:   cfg.API.Security.DeniedDirectories,
		MaxFilesPerScan:     cfg.API.Security.MaxFilesPerScan,
		ScanTimeoutSeconds:  cfg.API.Security.ScanTimeoutSeconds,
		AllowedOrigins:      cfg.API.Security.AllowedOrigins,
		AllowUNC:            cfg.API.Security.AllowUNC,
		AllowedUNCServers:   cfg.API.Security.AllowedUNCServers,
		RateLimitRPM:        cfg.API.Security.RateLimit.RequestsPerMinute,
		TrustedProxies:      cfg.API.Security.TrustedProxies,
		ForceSecureCookies:  cfg.API.Security.ForceSecureCookies,
		Host:                cfg.Server.Host,
		Port:                cfg.Server.Port,
		ScraperPriority:     cfg.Scrapers.Priority,
		ScraperUserAgent:    cfg.Scrapers.UserAgent,
		ScraperReferer:      cfg.Scrapers.Referer,
		ProxyConfig:         cfg.Scrapers.Proxy,
		FlareSolverrConfig:  cfg.Scrapers.FlareSolverr,
		DownloadProxyConfig: cfg.Output.Download.DownloadProxy,
		NFOEnabled:          cfg.Metadata.NFO.Feature.Enabled,
		NFOFilenameTemplate: cfg.Metadata.NFO.Format.FilenameTemplate,
		NFOPerFile:          cfg.Metadata.NFO.Feature.PerFile,
		NFODisplayTitle:     cfg.Metadata.NFO.Format.DisplayTitle,
		MaxPosterHeight:     cfg.Output.MediaFormat.MaxPosterHeight,
		TranslationConfig:   cfg.Metadata.Translation,
		OperationMode:       string(cfg.Output.GetOperationMode()),
		AllowRevert:         cfg.Output.Operation.AllowRevert,
		MaxWorkers:          cfg.Performance.MaxWorkers,
		WorkerTimeout:       time.Duration(cfg.Performance.WorkerTimeout) * time.Second,
		RegexEnabled:        cfg.Matching.RegexEnabled,
		RegexPattern:        cfg.Matching.RegexPattern,
		TempDir:             cfg.System.TempDir,
		VersionCheckEnabled: cfg.System.VersionCheckEnabled,
		LogLevel:            cfg.Logging.Level,
		DatabaseDSN:         cfg.Database.DSN,
		DatabaseLogLevel:    cfg.Database.LogLevel,
	}
}
