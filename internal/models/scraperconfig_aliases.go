package models

// Package-internal type aliases for scraperconfig types.
// These aliases allow a gradual migration: existing code referencing
// models.ScraperSettings etc. continues to compile while callers are
// updated to import scraperconfig directly.
// Once all callers have been migrated, these aliases will be removed.

import "github.com/javinizer/javinizer-go/internal/scraperconfig"

type ScraperSettings = scraperconfig.ScraperSettings
type ProxyConfig = scraperconfig.ProxyConfig
type ProxyProfile = scraperconfig.ProxyProfile
type ScraperProxyMode = scraperconfig.ScraperProxyMode
type BrowserConfig = scraperconfig.BrowserConfig
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
)
