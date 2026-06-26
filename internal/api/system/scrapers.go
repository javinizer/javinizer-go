package system

import (
	"net/http"
	"sort"

	"github.com/javinizer/javinizer-go/internal/api/core"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/models"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

// getAvailableScrapers godoc
// @Summary Get available scrapers
// @Description Get list of all available scrapers with their display names, enabled status, and configuration options. Scrapers are ordered by priority from config.
// @Tags system
// @Produce json
// @Success 200 {object} contracts.AvailableScrapersResponse
// @Failure 500 {object} contracts.ErrorResponse
// @Router /api/v1/scrapers [get]
func getAvailableScrapers(rt *core.APIRuntime) gin.HandlerFunc {
	return func(c *gin.Context) {
		deps := rt.Deps()
		scrapers := []contracts.ScraperInfo{}
		apiCfg := rt.GetAPIConfig()
		batchCfg := apiCfg.BatchConfig()
		profileChoices := proxyProfileChoices(apiCfg)
		downloadProfileChoices := downloadProxyProfileChoices(apiCfg)

		// Use getter to get current registry (respects config reloads)
		registry := deps.GetScraperLister()
		registered := registry.GetAllInstances()
		scraperByName := make(map[string]models.Scraper, len(registered))
		for _, scraper := range registered {
			if scraper != nil {
				scraperByName[scraper.Name()] = scraper
			}
		}

		// Build deterministic order:
		// 1) config scrapers.priority order
		// 2) any remaining registered scrapers (sorted by name)
		orderedNames := make([]string, 0, len(scraperByName))
		seen := make(map[string]bool, len(scraperByName))
		for _, name := range batchCfg.ScraperPriority {
			if _, ok := scraperByName[name]; !ok || seen[name] {
				continue
			}
			orderedNames = append(orderedNames, name)
			seen[name] = true
		}
		remainingNames := make([]string, 0, len(scraperByName))
		for name := range scraperByName {
			if !seen[name] {
				remainingNames = append(remainingNames, name)
			}
		}
		sort.Strings(remainingNames)
		orderedNames = append(orderedNames, remainingNames...)

		for _, name := range orderedNames {
			scraper := scraperByName[name]
			displayName, options := scraperDisplayTitleAndOptions(deps, name, profileChoices, downloadProfileChoices)

			scrapers = append(scrapers, contracts.ScraperInfo{
				Name:         name,
				DisplayTitle: displayName,
				Enabled:      scraper.IsEnabled(),
				Options:      options,
			})
		}

		c.JSON(http.StatusOK, contracts.AvailableScrapersResponse{
			Scrapers: scrapers,
		})
	}
}

func scraperProxyOptions(profileChoices []contracts.ScraperChoice) []contracts.ScraperOption {
	return []contracts.ScraperOption{
		{
			Key:         "proxy.enabled",
			Label:       "Enable proxy for this scraper",
			Description: "Use proxy for this scraper (inherits global proxy profile when no scraper profile is selected)",
			Type:        "boolean",
		},
		{
			Key:         "proxy.profile",
			Label:       "Proxy profile",
			Description: "Optional scraper-specific proxy profile (leave empty to inherit global default profile)",
			Type:        "select",
			Choices:     profileChoices,
		},
	}
}

func scraperUserAgentOptions() []contracts.ScraperOption {
	return []contracts.ScraperOption{
		{
			Key:         "user_agent",
			Label:       "User-Agent",
			Description: "Custom User-Agent (uses default browser UA if empty)",
			Type:        "string",
		},
	}
}

func scraperDownloadProxyOptions(profileChoices []contracts.ScraperChoice) []contracts.ScraperOption {
	return []contracts.ScraperOption{
		{
			Key:         "download_proxy.enabled",
			Label:       "Download proxy enabled",
			Description: "Enable scraper-specific download proxy override",
			Type:        "boolean",
		},
		{
			Key:         "download_proxy.profile",
			Label:       "Download proxy profile",
			Description: "Optional scraper-specific download proxy profile (leave empty to inherit scraper/global proxy profile)",
			Type:        "select",
			Choices:     profileChoices,
		},
	}
}

func proxyProfileChoices(apiCfg core.APIConfig) []contracts.ScraperChoice {
	return proxyProfileChoicesFrom(apiCfg.ProxyConfig)
}

// downloadProxyProfileChoices returns the selectable profile names for the
// download proxy (cfg.Output.Download.DownloadProxy). Surfacing these lets the
// scrapers response assemble download_proxy.profile choices from the
// download-proxy profiles rather than reusing the scrape-proxy profiles.
func downloadProxyProfileChoices(apiCfg core.APIConfig) []contracts.ScraperChoice {
	return proxyProfileChoicesFrom(apiCfg.DownloadProxyConfig)
}

// proxyProfileChoicesFrom builds the choice list (Inherit Default + sorted
// profile names) for a single ProxyConfig. Shared by the scrape- and
// download-proxy choice builders to avoid duplicating the assembly logic.
func proxyProfileChoicesFrom(sysCfg models.ProxyConfig) []contracts.ScraperChoice {
	choices := []contracts.ScraperChoice{
		{Value: "", Label: "Inherit Default"},
	}
	if len(sysCfg.Profiles) == 0 {
		return choices
	}

	names := make([]string, 0, len(sysCfg.Profiles))
	for name := range sysCfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		choices = append(choices, contracts.ScraperChoice{
			Value: name,
			Label: name,
		})
	}

	return choices
}
