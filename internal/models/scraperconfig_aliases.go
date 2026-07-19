package models

// Package-internal type aliases for scraperconfig types.
// These aliases allow a gradual migration: existing code referencing
// models.ScraperSettings etc. continues to compile while callers are
// updated to import scraperconfig directly.
// Once all callers have been migrated, these aliases will be removed.

import "github.com/javinizer/javinizer-go/internal/scraperconfig"

// ScraperSettings aliases the scraperconfig.ScraperSettings type for backward compatibility.
type ScraperSettings = scraperconfig.ScraperSettings

// ProxyConfig aliases the scraperconfig.ProxyConfig type for backward compatibility.
type ProxyConfig = scraperconfig.ProxyConfig

// ProxyProfile aliases the scraperconfig.ProxyProfile type for backward compatibility.
type ProxyProfile = scraperconfig.ProxyProfile

// ScraperProxyMode aliases the scraperconfig.ScraperProxyMode type for backward compatibility.
type ScraperProxyMode = scraperconfig.ScraperProxyMode

// BrowserConfig aliases the scraperconfig.BrowserConfig type for backward compatibility.
type BrowserConfig = scraperconfig.BrowserConfig

// FlareSolverrConfig aliases the scraperconfig.FlareSolverrConfig type for backward compatibility.
type FlareSolverrConfig = scraperconfig.FlareSolverrConfig

// Constant aliases — these let existing code use models.ScraperProxyModeDirect etc.
const (
	ScraperProxyModeDirect   = scraperconfig.ScraperProxyModeDirect
	ScraperProxyModeInherit  = scraperconfig.ScraperProxyModeInherit
	ScraperProxyModeSpecific = scraperconfig.ScraperProxyModeSpecific

	RedactedValue = scraperconfig.RedactedValue
)

// Function aliases — these let existing code use models.ResolveScraperProxy etc.
var (
	ResolveScraperProxy     = scraperconfig.ResolveScraperProxy
	ResolveGlobalProxy      = scraperconfig.ResolveGlobalProxy
	ResolveScraperProxyMode = scraperconfig.ResolveScraperProxyMode

	WithScrapeActress = scraperconfig.WithScrapeActress
	WithBrowser       = scraperconfig.WithBrowser
)

// ScraperOverride aliases the scraperconfig.ScraperOverride type for backward compatibility.
type ScraperOverride = scraperconfig.ScraperOverride
