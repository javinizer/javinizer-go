package system

import (
	"github.com/javinizer/javinizer-go/internal/api/core"

	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
)

func scraperDisplayTitleAndOptions(deps *core.APIDeps, name string, profileChoices, downloadProfileChoices []contracts.ScraperChoice) (string, []contracts.ScraperOption) {
	if deps != nil {
		if result, exists := deps.GetScraperOptions(name); exists {
			options := make([]contracts.ScraperOption, 0, len(result.Options)+10)
			options = append(options, result.Options...)

			options = append(options, scraperUserAgentOptions()...)
			options = append(options, scraperProxyOptions(profileChoices)...)
			options = append(options, scraperDownloadProxyOptions(downloadProfileChoices)...)

			return result.DisplayTitle, options
		}
	}

	options := scraperUserAgentOptions()
	options = append(options, scraperProxyOptions(profileChoices)...)
	options = append(options, scraperDownloadProxyOptions(downloadProfileChoices)...)

	return name, options
}
