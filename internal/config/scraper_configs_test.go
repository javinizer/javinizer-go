package config

import (
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// RegisterTestScraperConfigs registers all 13 scraper configs with scraperutil
// for tests that need to validate scraper configurations.
// This replaces the backward-compatibility fallback that was removed from normalizeScraperConfigs.
//
// Usage: Call this at the start of test functions that need scraper validation,
// then call cfg.Validate() normally.
//
// Note: This registers validators that use stub validation (return nil) since
// we don't import actual scraper packages. For full validation, tests should
// import the specific scraper packages they need.
func RegisterTestScraperConfigs() {
	scraperutil.ResetScraperConfigs()
	scraperutil.ResetValidators()
	scraperutil.ResetConfigFactories()
	scraperutil.ResetFlattenFuncs()
	scraperutil.ResetDefaults()

	// Register default scraper settings for all 13 scrapers with correct priorities
	// Priorities: higher number = higher priority (run first)
	// These match the priorities registered by each scraper package's init()
	scraperPriorities := []struct {
		name     string
		priority int
	}{
		{"r18dev", 100},
		{"libredmm", 95},
		{"dmm", 90},
		{"javlibrary", 80},
		{"javdb", 75},
		{"javbus", 70},
		{"jav321", 65},
		{"mgstage", 55},
		{"tokyohot", 50},
		{"aventertainment", 45},
		{"caribbeancom", 40},
		{"dlgetchu", 40},
		{"fc2", 35},
		{"javstash", 10},
	}
	for _, sp := range scraperPriorities {
		scraperutil.RegisterDefaultScraperSettings(sp.name, &ScraperSettings{
			Enabled: true,
		}, sp.priority)
	}

	// Register FlattenFunc for each scraper using config package types.
	// This ensures FlatToScraperConfig uses the registry path, not the fallback.
	for _, name := range []string{
		"r18dev", "dmm", "libredmm", "mgstage", "javlibrary", "javdb",
		"javbus", "jav321", "tokyohot", "aventertainment", "dlgetchu",
		"caribbeancom", "fc2", "javstash",
	} {
		scraperutil.RegisterFlattenFunc(name, func(a any) any {
			cfg, ok := a.(scraperutil.ScraperConfigInterface)
			if !ok {
				return nil
			}
			sc := &ScraperSettings{}
			sc.Enabled = cfg.IsEnabled()
			sc.RateLimit = cfg.GetRequestDelay()
			sc.RetryCount = cfg.GetMaxRetries()
			sc.UserAgent = cfg.GetUserAgent()
			if p := cfg.GetProxy(); p != nil {
				sc.Proxy = p.(*ProxyConfig)
			}
			if dp := cfg.GetDownloadProxy(); dp != nil {
				sc.DownloadProxy = dp.(*ProxyConfig)
			}
			return sc
		})
	}

	// Register stub validators (actual validation happens in scraper packages)
	// These return nil to pass validation without doing full scraper-specific checks
	for _, name := range []string{
		"r18dev", "dmm", "libredmm", "mgstage", "javlibrary", "javdb",
		"javbus", "jav321", "tokyohot", "aventertainment", "dlgetchu",
		"caribbeancom", "fc2", "javstash",
	} {
		scraperutil.RegisterValidator(name, func(a any) error {
			return nil // Stub - scraper packages do actual validation
		})
	}

	// ConfigFactory registration removed: all 13 scraper packages register their own
	// ConfigFactory via init() functions in their respective config.go files.
	// Tests that import scraper packages will have their ConfigFactory registered automatically.
	// This approach eliminates duplicate registrations and circular dependency issues.
}
