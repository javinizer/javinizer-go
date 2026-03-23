package api

import (
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
)

// getAvailableScrapers godoc
// @Summary Get available scrapers
// @Description Get list of all available scrapers with their display names, enabled status, and configuration options. Scrapers are ordered by priority from config.
// @Tags system
// @Produce json
// @Success 200 {object} AvailableScrapersResponse
// @Failure 500 {object} ErrorResponse
// @Router /api/v1/scrapers [get]
func getAvailableScrapers(deps *ServerDependencies) gin.HandlerFunc {
	return func(c *gin.Context) {
		scrapers := []ScraperInfo{}
		cfg := deps.GetConfig()
		profileChoices := proxyProfileChoices(cfg)

		// Use getter to get current registry (respects config reloads)
		registry := deps.GetRegistry()
		registered := registry.GetAll()
		scraperByName := make(map[string]models.Scraper, len(registered))
		for _, scraper := range registered {
			scraperByName[scraper.Name()] = scraper
		}

		// Build deterministic order:
		// 1) config scrapers.priority order
		// 2) any remaining registered scrapers (sorted by name)
		orderedNames := make([]string, 0, len(scraperByName))
		seen := make(map[string]bool, len(scraperByName))
		if cfg != nil {
			for _, name := range cfg.Scrapers.Priority {
				if _, ok := scraperByName[name]; !ok || seen[name] {
					continue
				}
				orderedNames = append(orderedNames, name)
				seen[name] = true
			}
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
			displayName, options := scraperDisplayNameAndOptions(name, profileChoices)

			scrapers = append(scrapers, ScraperInfo{
				Name:        name,
				DisplayName: displayName,
				Enabled:     scraper.IsEnabled(),
				Options:     options,
			})
		}

		c.JSON(200, AvailableScrapersResponse{
			Scrapers: scrapers,
		})
	}
}

func scraperProxyOptions(profileChoices []ScraperChoice) []ScraperOption {
	return []ScraperOption{
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

func scraperFakeUserAgentOptions() []ScraperOption {
	return []ScraperOption{
		{
			Key:         "use_fake_user_agent",
			Label:       "Use fake User-Agent",
			Description: "Use a browser-like User-Agent string for this scraper",
			Type:        "boolean",
		},
		{
			Key:         "fake_user_agent",
			Label:       "Fake User-Agent",
			Description: "Optional custom fake User-Agent (leave empty to use default browser User-Agent)",
			Type:        "string",
		},
	}
}

func scraperDownloadProxyOptions(profileChoices []ScraperChoice) []ScraperOption {
	return []ScraperOption{
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

func proxyProfileChoices(cfg *config.Config) []ScraperChoice {
	choices := []ScraperChoice{
		{Value: "", Label: "Inherit Default"},
	}
	if cfg == nil || len(cfg.Scrapers.Proxy.Profiles) == 0 {
		return choices
	}

	names := make([]string, 0, len(cfg.Scrapers.Proxy.Profiles))
	for name := range cfg.Scrapers.Proxy.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		choices = append(choices, ScraperChoice{
			Value: name,
			Label: name,
		})
	}

	return choices
}

// ptrInt returns a pointer to an int value
func ptrInt(v int) *int {
	return &v
}
