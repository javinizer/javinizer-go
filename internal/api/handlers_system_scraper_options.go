package api

func scraperDisplayNameAndOptions(name string, profileChoices []ScraperChoice) (string, []ScraperOption) {
	displayName := name
	var options []ScraperOption

	switch name {
	case "r18dev":
		displayName = "R18.dev"
		options = []ScraperOption{
			{
				Key:         "language",
				Label:       "Language",
				Description: "Language for metadata fields from R18.dev",
				Type:        "select",
				Choices: []ScraperChoice{
					{Value: "en", Label: "English"},
					{Value: "ja", Label: "Japanese"},
				},
			},
		}
		options = append(options, scraperFakeUserAgentOptions()...)
		options = append(options, scraperProxyOptions(profileChoices)...)
		options = append(options, scraperDownloadProxyOptions(profileChoices)...)
	case "dmm":
		displayName = "DMM/Fanza"
		// DMM scraper options
		minTimeout := 5
		maxTimeout := 120
		options = []ScraperOption{
			{
				Key:         "scrape_actress",
				Label:       "Scrape Actress Information",
				Description: "Extract actress names and IDs from DMM. Disable for faster scraping if you only need actress data from other sources.",
				Type:        "boolean",
			},
			{
				Key:         "enable_browser",
				Label:       "Enable browser mode",
				Description: "Use browser automation for video.dmm.co.jp (required for JavaScript-rendered content)",
				Type:        "boolean",
			},
			{
				Key:         "browser_timeout",
				Label:       "Browser timeout",
				Description: "Maximum time to wait for browser operations",
				Type:        "number",
				Min:         &minTimeout,
				Max:         &maxTimeout,
				Unit:        "seconds",
			},
		}
		options = append(options, scraperFakeUserAgentOptions()...)
		options = append(options, scraperProxyOptions(profileChoices)...)
		options = append(options, scraperDownloadProxyOptions(profileChoices)...)
	case "libredmm":
		displayName = "LibreDMM"
		options = []ScraperOption{
			{
				Key:         "request_delay",
				Label:       "Request delay",
				Description: "Delay between requests to avoid rate limiting",
				Type:        "number",
				Min:         ptrInt(0),
				Max:         ptrInt(5000),
				Unit:        "ms",
			},
			{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "LibreDMM base URL",
				Type:        "string",
			},
		}
		options = append(options, scraperFakeUserAgentOptions()...)
		options = append(options, scraperProxyOptions(profileChoices)...)
		options = append(options, scraperDownloadProxyOptions(profileChoices)...)
	case "mgstage":
		displayName = "MGStage"
		// MGStage scraper options
		options = []ScraperOption{
			{
				Key:         "request_delay",
				Label:       "Request delay",
				Description: "Delay between requests to avoid rate limiting (0 = no delay)",
				Type:        "number",
				Min:         ptrInt(0),
				Max:         ptrInt(5000),
				Unit:        "ms",
			},
		}
		options = append(options, scraperFakeUserAgentOptions()...)
		options = append(options, scraperProxyOptions(profileChoices)...)
		options = append(options, scraperDownloadProxyOptions(profileChoices)...)
	case "javlibrary":
		displayName = "JavLibrary"
		options = []ScraperOption{
			{
				Key:         "language",
				Label:       "Language",
				Description: "Language for metadata (affects title, genres, and actress names)",
				Type:        "select",
				Choices: []ScraperChoice{
					{Value: "en", Label: "English"},
					{Value: "ja", Label: "Japanese"},
					{Value: "cn", Label: "Chinese (Simplified)"},
					{Value: "tw", Label: "Chinese (Traditional)"},
				},
			},
			{
				Key:         "request_delay",
				Label:       "Request delay",
				Description: "Delay between requests to avoid rate limiting",
				Type:        "number",
				Min:         ptrInt(0),
				Max:         ptrInt(5000),
				Unit:        "ms",
			},
			{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "JavLibrary base URL (leave default unless you need a mirror/domain override)",
				Type:        "string",
			},
			{
				Key:         "use_flaresolverr",
				Label:       "Use FlareSolverr",
				Description: "Route requests through FlareSolverr to bypass Cloudflare protection (requires FlareSolverr to be configured in Proxy settings)",
				Type:        "boolean",
			},
		}
		options = append(options, scraperFakeUserAgentOptions()...)
		options = append(options, scraperProxyOptions(profileChoices)...)
		options = append(options, scraperDownloadProxyOptions(profileChoices)...)
	case "javdb":
		displayName = "JavDB"
		options = []ScraperOption{
			{
				Key:         "request_delay",
				Label:       "Request delay",
				Description: "Delay between requests to avoid rate limiting",
				Type:        "number",
				Min:         ptrInt(0),
				Max:         ptrInt(5000),
				Unit:        "ms",
			},
			{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "JavDB base URL (leave default unless you need a mirror/domain override)",
				Type:        "string",
			},
			{
				Key:         "use_flaresolverr",
				Label:       "Use FlareSolverr",
				Description: "Route requests through FlareSolverr to bypass Cloudflare protection (often needed for JavDB)",
				Type:        "boolean",
			},
		}
		options = append(options, scraperFakeUserAgentOptions()...)
		options = append(options, scraperProxyOptions(profileChoices)...)
		options = append(options, scraperDownloadProxyOptions(profileChoices)...)
	case "javbus":
		displayName = "JavBus"
		options = []ScraperOption{
			{
				Key:         "language",
				Label:       "Language",
				Description: "Language for metadata output",
				Type:        "select",
				Choices: []ScraperChoice{
					{Value: "ja", Label: "Japanese"},
					{Value: "en", Label: "English"},
					{Value: "zh", Label: "Chinese"},
				},
			},
			{
				Key:         "request_delay",
				Label:       "Request delay",
				Description: "Delay between requests to avoid rate limiting",
				Type:        "number",
				Min:         ptrInt(0),
				Max:         ptrInt(5000),
				Unit:        "ms",
			},
			{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "JavBus base URL (leave default unless you need a mirror/domain override)",
				Type:        "string",
			},
		}
		options = append(options, scraperFakeUserAgentOptions()...)
		options = append(options, scraperProxyOptions(profileChoices)...)
		options = append(options, scraperDownloadProxyOptions(profileChoices)...)
	case "jav321":
		displayName = "Jav321"
		options = []ScraperOption{
			{
				Key:         "request_delay",
				Label:       "Request delay",
				Description: "Delay between requests to avoid rate limiting",
				Type:        "number",
				Min:         ptrInt(0),
				Max:         ptrInt(5000),
				Unit:        "ms",
			},
			{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "Jav321 base URL",
				Type:        "string",
			},
		}
		options = append(options, scraperFakeUserAgentOptions()...)
		options = append(options, scraperProxyOptions(profileChoices)...)
		options = append(options, scraperDownloadProxyOptions(profileChoices)...)
	case "tokyohot":
		displayName = "Tokyo-Hot"
		options = []ScraperOption{
			{
				Key:         "language",
				Label:       "Language",
				Description: "Language for metadata output",
				Type:        "select",
				Choices: []ScraperChoice{
					{Value: "ja", Label: "Japanese"},
					{Value: "en", Label: "English"},
					{Value: "zh", Label: "Chinese"},
				},
			},
			{
				Key:         "request_delay",
				Label:       "Request delay",
				Description: "Delay between requests to avoid rate limiting",
				Type:        "number",
				Min:         ptrInt(0),
				Max:         ptrInt(5000),
				Unit:        "ms",
			},
			{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "Tokyo-Hot base URL",
				Type:        "string",
			},
		}
		options = append(options, scraperFakeUserAgentOptions()...)
		options = append(options, scraperProxyOptions(profileChoices)...)
		options = append(options, scraperDownloadProxyOptions(profileChoices)...)
	case "aventertainment":
		displayName = "AV Entertainment"
		options = []ScraperOption{
			{
				Key:         "language",
				Label:       "Language",
				Description: "Language for metadata output",
				Type:        "select",
				Choices: []ScraperChoice{
					{Value: "en", Label: "English"},
					{Value: "ja", Label: "Japanese"},
				},
			},
			{
				Key:         "request_delay",
				Label:       "Request delay",
				Description: "Delay between requests to avoid rate limiting",
				Type:        "number",
				Min:         ptrInt(0),
				Max:         ptrInt(5000),
				Unit:        "ms",
			},
			{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "AV Entertainment base URL",
				Type:        "string",
			},
			{
				Key:         "scrape_bonus_screens",
				Label:       "Scrape bonus screenshots",
				Description: "Append bonus image files (e.g., 特典ファイル) to screenshots",
				Type:        "boolean",
			},
		}
		options = append(options, scraperFakeUserAgentOptions()...)
		options = append(options, scraperProxyOptions(profileChoices)...)
		options = append(options, scraperDownloadProxyOptions(profileChoices)...)
	case "dlgetchu":
		displayName = "DLGetchu"
		options = []ScraperOption{
			{
				Key:         "request_delay",
				Label:       "Request delay",
				Description: "Delay between requests to avoid rate limiting",
				Type:        "number",
				Min:         ptrInt(0),
				Max:         ptrInt(5000),
				Unit:        "ms",
			},
			{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "DLGetchu base URL",
				Type:        "string",
			},
		}
		options = append(options, scraperFakeUserAgentOptions()...)
		options = append(options, scraperProxyOptions(profileChoices)...)
		options = append(options, scraperDownloadProxyOptions(profileChoices)...)
	case "caribbeancom":
		displayName = "Caribbeancom"
		options = []ScraperOption{
			{
				Key:         "language",
				Label:       "Language",
				Description: "Language for metadata output",
				Type:        "select",
				Choices: []ScraperChoice{
					{Value: "ja", Label: "Japanese"},
					{Value: "en", Label: "English"},
				},
			},
			{
				Key:         "request_delay",
				Label:       "Request delay",
				Description: "Delay between requests to avoid rate limiting",
				Type:        "number",
				Min:         ptrInt(0),
				Max:         ptrInt(5000),
				Unit:        "ms",
			},
			{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "Caribbeancom base URL",
				Type:        "string",
			},
		}
		options = append(options, scraperFakeUserAgentOptions()...)
		options = append(options, scraperProxyOptions(profileChoices)...)
		options = append(options, scraperDownloadProxyOptions(profileChoices)...)
	case "fc2":
		displayName = "FC2"
		options = []ScraperOption{
			{
				Key:         "request_delay",
				Label:       "Request delay",
				Description: "Delay between requests to avoid rate limiting",
				Type:        "number",
				Min:         ptrInt(0),
				Max:         ptrInt(5000),
				Unit:        "ms",
			},
			{
				Key:         "base_url",
				Label:       "Base URL",
				Description: "FC2 base URL",
				Type:        "string",
			},
		}
		options = append(options, scraperFakeUserAgentOptions()...)
		options = append(options, scraperProxyOptions(profileChoices)...)
		options = append(options, scraperDownloadProxyOptions(profileChoices)...)
	}

	return displayName, options
}
