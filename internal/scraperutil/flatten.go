package scraperutil

type FlattenOverrides struct {
	BaseURL       string
	Language      string
	UseBrowser    bool
	ScrapeActress *bool
	Cookies       map[string]string
	Extra         map[string]any
}

type FlattenedConfig struct {
	Enabled       bool
	RateLimit     int
	Proxy         any
	DownloadProxy any
}

func ExtractFlattenedConfig(cfg any) (*FlattenedConfig, bool) {
	c, ok := cfg.(ScraperConfigInterface)
	if !ok {
		return nil, false
	}
	return &FlattenedConfig{
		Enabled:       c.IsEnabled(),
		RateLimit:     c.GetRequestDelay(),
		Proxy:         c.GetProxy(),
		DownloadProxy: c.GetDownloadProxy(),
	}, true
}

type SettingsBuilder func(*FlattenedConfig, FlattenOverrides) any

type SettingsBuilderWithRaw func(*FlattenedConfig, FlattenOverrides, any) any

func DefaultFlattenConfig(overrides FlattenOverrides, builder SettingsBuilder) FlattenFunc {
	return FlattenFunc(func(cfg any) any {
		fc, ok := ExtractFlattenedConfig(cfg)
		if !ok {
			return nil
		}
		return builder(fc, overrides)
	})
}

func DefaultFlattenConfigWithRaw(overrides FlattenOverrides, builder SettingsBuilderWithRaw) FlattenFunc {
	return FlattenFunc(func(cfg any) any {
		fc, ok := ExtractFlattenedConfig(cfg)
		if !ok {
			return nil
		}
		return builder(fc, overrides, cfg)
	})
}

func DefaultFlattenConfigFromConfig(c ScraperConfigInterface, overrides FlattenOverrides, builder SettingsBuilder) any {
	fc := &FlattenedConfig{
		Enabled:       c.IsEnabled(),
		RateLimit:     c.GetRequestDelay(),
		Proxy:         c.GetProxy(),
		DownloadProxy: c.GetDownloadProxy(),
	}
	return builder(fc, overrides)
}
