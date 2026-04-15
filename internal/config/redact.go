package config

const RedactedValue = "•••••"

func (c *Config) Redact() *Config {
	if c == nil {
		return nil
	}

	copy := *c

	copy.Database = c.Database
	copy.Database.DSN = redactString(c.Database.DSN)

	copy.Metadata = c.Metadata
	copy.Metadata.Translation = c.Metadata.Translation
	copy.Metadata.Translation.OpenAI = c.Metadata.Translation.OpenAI
	copy.Metadata.Translation.OpenAI.APIKey = redactString(c.Metadata.Translation.OpenAI.APIKey)
	copy.Metadata.Translation.DeepL = c.Metadata.Translation.DeepL
	copy.Metadata.Translation.DeepL.APIKey = redactString(c.Metadata.Translation.DeepL.APIKey)
	copy.Metadata.Translation.Google = c.Metadata.Translation.Google
	copy.Metadata.Translation.Google.APIKey = redactString(c.Metadata.Translation.Google.APIKey)
	copy.Metadata.Translation.OpenAICompatible = c.Metadata.Translation.OpenAICompatible
	copy.Metadata.Translation.OpenAICompatible.APIKey = redactString(c.Metadata.Translation.OpenAICompatible.APIKey)
	copy.Metadata.Translation.Anthropic = c.Metadata.Translation.Anthropic
	copy.Metadata.Translation.Anthropic.APIKey = redactString(c.Metadata.Translation.Anthropic.APIKey)

	copy.Scrapers = c.Scrapers
	copy.Scrapers.Proxy = redactProxyConfig(c.Scrapers.Proxy)
	copy.Output.DownloadProxy = redactProxyConfig(c.Output.DownloadProxy)

	if c.Scrapers.Overrides != nil {
		copy.Scrapers.Overrides = make(map[string]*ScraperSettings, len(c.Scrapers.Overrides))
		for k, v := range c.Scrapers.Overrides {
			if v == nil {
				continue
			}
			s := *v
			if v.Proxy != nil {
				p := *v.Proxy
				s.Proxy = &p
				s.Proxy.Profiles = redactProxyProfiles(v.Proxy.Profiles)
			}
			if v.DownloadProxy != nil {
				p := *v.DownloadProxy
				s.DownloadProxy = &p
				s.DownloadProxy.Profiles = redactProxyProfiles(v.DownloadProxy.Profiles)
			}
			copy.Scrapers.Overrides[k] = &s
		}
	}

	return &copy
}

func redactString(s string) string {
	if s == "" {
		return ""
	}
	return RedactedValue
}

func redactProxyConfig(pc ProxyConfig) ProxyConfig {
	pc.Profiles = redactProxyProfiles(pc.Profiles)
	return pc
}

func redactProxyProfiles(profiles map[string]ProxyProfile) map[string]ProxyProfile {
	if profiles == nil {
		return nil
	}
	result := make(map[string]ProxyProfile, len(profiles))
	for k, v := range profiles {
		p := v
		p.Username = redactString(v.Username)
		p.Password = redactString(v.Password)
		result[k] = p
	}
	return result
}
