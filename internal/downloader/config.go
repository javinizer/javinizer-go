package downloader

import (
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// Config holds the subset of application configuration needed by the Downloader.
// Proxy profiles are pre-resolved by the bridge function so the downloader
// never imports internal/config for proxy resolution.
type Config struct {
	// MediaFormatConfig controls media filename templates and actress grouping.
	// Embedded so downstream code can access fields directly while also passing
	// the unit to helpers that share the same fields as organizer.Config.
	organizer.MediaFormatConfig

	// Download toggles
	DownloadCover       bool
	DownloadPoster      bool
	DownloadExtrafanart bool
	DownloadTrailer     bool
	DownloadActress     bool
	DownloadTimeout     int

	// Poster cropping cap (px); 0 = no cap, preserve source resolution.
	MaxPosterHeight int

	// HTTP client settings
	UserAgent string // From Scrapers.UserAgent, resolved by bridge

	// Actor name formatting (from NFO config, resolved by bridge)
	ActorJapaneseNames  bool
	ActorFirstNameOrder bool
	ActressDelimiter    string                    // Delimiter between actress names when no DELIM= modifier is present (default: ", ")
	UnknownActressMode  models.UnknownActressMode // "skip" or "fallback" — controls how unknown actresses are handled
	UnknownActressText  string                    // Display text for unknown actresses when mode is "fallback"
}

// ConfigFromAppConfig extracts Downloader-relevant fields from the application config.
// nameCfg is the pre-constructed NFONameConfig shared across nfo, organizer, and
// downloader bridges — constructed once in extractDomainConfigs so that overlapping
// fields (FirstNameOrder, GroupActress, GroupActressName) are read from the monolith
// config exactly once.
//
// Config-bridge reads: cfg.Output.MediaFormat.PosterFormat, cfg.Output.MediaFormat.FanartFormat, cfg.Output.MediaFormat.TrailerFormat,
// cfg.Output.MediaFormat.ScreenshotFormat, cfg.Output.MediaFormat.ScreenshotFolder, cfg.Output.MediaFormat.ScreenshotPadding,
// cfg.Output.MediaFormat.ActressFolder, cfg.Output.MediaFormat.ActressFormat,
// cfg.Output.Download.DownloadCover, cfg.Output.Download.DownloadPoster,
// cfg.Output.Download.DownloadExtrafanart, cfg.Output.Download.DownloadTrailer, cfg.Output.Download.DownloadActress,
// cfg.Output.Download.DownloadTimeout, cfg.Scrapers.UserAgent,
// cfg.Metadata.NFO.Format.ActressLanguageJA,
// cfg.Metadata.NFO.Format.UnknownActressMode, cfg.Metadata.NFO.Format.UnknownActressText,
// cfg.Output.MediaFormat.MaxPosterHeight
// (Fields FirstNameOrder, GroupActress, GroupActressName are read via nameCfg — see NFONameConfigFromAppConfig)
func ConfigFromAppConfig(cfg *config.Config, nameCfg nfo.NFONameConfig) *Config {
	if cfg == nil {
		return nil
	}
	return &Config{
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat:            cfg.Output.MediaFormat.PosterFormat,
			FanartFormat:            cfg.Output.MediaFormat.FanartFormat,
			TrailerFormat:           cfg.Output.MediaFormat.TrailerFormat,
			ScreenshotFormat:        cfg.Output.MediaFormat.ScreenshotFormat,
			ScreenshotFolder:        cfg.Output.MediaFormat.ScreenshotFolder,
			ScreenshotPadding:       cfg.Output.MediaFormat.ScreenshotPadding,
			GroupActress:            nameCfg.GroupActress,
			GroupActressName:        nameCfg.GroupActressName,
			GroupUnknownActressName: nameCfg.GroupUnknownActressName,
			ActressFolder:           cfg.Output.MediaFormat.ActressFolder,
			ActressFormat:           cfg.Output.MediaFormat.ActressFormat,
		},
		DownloadCover:       cfg.Output.Download.DownloadCover,
		DownloadPoster:      cfg.Output.Download.DownloadPoster,
		DownloadExtrafanart: cfg.Output.Download.DownloadExtrafanart,
		DownloadTrailer:     cfg.Output.Download.DownloadTrailer,
		DownloadActress:     cfg.Output.Download.DownloadActress,
		DownloadTimeout:     cfg.Output.Download.DownloadTimeout,
		MaxPosterHeight:     cfg.Output.MediaFormat.MaxPosterHeight,
		UserAgent:           cfg.Scrapers.UserAgent,
		ActorJapaneseNames:  cfg.Metadata.NFO.Format.ActressLanguageJA,
		ActorFirstNameOrder: nameCfg.FirstNameOrder,
		ActressDelimiter:    nameCfg.ActressDelimiter,
		UnknownActressMode:  cfg.Metadata.NFO.Format.UnknownActressMode,
		UnknownActressText:  cfg.Metadata.NFO.Format.UnknownActressText,
	}
}

// HTTPClientConfig holds the pre-resolved HTTP client configuration.
// The bridge function resolves all proxy profiles so the downloader
// never imports internal/config for proxy resolution.
type HTTPClientConfig struct {
	Timeout           time.Duration
	DownloadProxy     *models.ProxyProfile           // Pre-resolved explicit download proxy (nil = not configured)
	ProxyResolvers    []models.DownloadProxyResolver // Pre-collected from registry
	GlobalProxy       *models.ProxyProfile           // Pre-resolved global scraper proxy
	GlobalProxyConfig *models.ProxyConfig            // For per-request scraper proxy resolution (profile lookup)
}
