package config

import (
	"maps"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Clone returns a deep copy of the Config.
// Note: maps.Clone is used for ProxyProfile maps — this is sufficient because
// models.ProxyProfile only contains value-type fields (no slices, maps, or pointers).
// Pointer, map, and slice fields are cloned so mutations to the copy do not affect the original.
func (c *Config) Clone() *Config {
	if c == nil {
		return nil
	}

	cp := *c

	// Deep-copy API.Security slice fields
	cp.API.Security.AllowedDirectories = cloneStringSlice(c.API.Security.AllowedDirectories)
	cp.API.Security.DeniedDirectories = cloneStringSlice(c.API.Security.DeniedDirectories)
	cp.API.Security.AllowedOrigins = cloneStringSlice(c.API.Security.AllowedOrigins)
	cp.API.Security.AllowedUNCServers = cloneStringSlice(c.API.Security.AllowedUNCServers)
	cp.API.Security.TrustedProxies = cloneStringSlice(c.API.Security.TrustedProxies)

	// Deep-copy Metadata slice and map fields
	cp.Metadata.IgnoreGenres = cloneStringSlice(c.Metadata.IgnoreGenres)
	cp.Metadata.RequiredFields = cloneStringSlice(c.Metadata.RequiredFields)
	cp.Metadata.Priority.Priority = cloneStringSlice(c.Metadata.Priority.Priority)
	cp.Metadata.Priority.Fields = deepCopyFieldsMap(c.Metadata.Priority.Fields)

	// Deep-copy OpenAICompatible.EnableThinking *bool pointer
	if c.Metadata.Translation.OpenAICompatible.EnableThinking != nil {
		v := *c.Metadata.Translation.OpenAICompatible.EnableThinking
		cp.Metadata.Translation.OpenAICompatible.EnableThinking = &v
	}

	// Deep-copy Completeness slice fields
	cp.Metadata.Completeness.Tiers.Essential.Fields = cloneStringSlice(c.Metadata.Completeness.Tiers.Essential.Fields)
	cp.Metadata.Completeness.Tiers.Important.Fields = cloneStringSlice(c.Metadata.Completeness.Tiers.Important.Fields)
	cp.Metadata.Completeness.Tiers.NiceToHave.Fields = cloneStringSlice(c.Metadata.Completeness.Tiers.NiceToHave.Fields)

	// Deep-copy NFO slice fields
	cp.Metadata.NFO.Extra.Tag = cloneStringSlice(c.Metadata.NFO.Extra.Tag)
	cp.Metadata.NFO.Extra.Credits = cloneStringSlice(c.Metadata.NFO.Extra.Credits)

	// Deep-copy Matching slice fields
	cp.Matching.Extensions = cloneStringSlice(c.Matching.Extensions)
	cp.Matching.ExcludePatterns = cloneStringSlice(c.Matching.ExcludePatterns)

	// Deep-copy Output slice fields
	cp.Output.Template.SubfolderFormat = cloneStringSlice(c.Output.Template.SubfolderFormat)
	cp.Output.Operation.SubtitleExtensions = cloneStringSlice(c.Output.Operation.SubtitleExtensions)

	// Deep-copy Scrapers reference-type fields
	cp.Scrapers.Priority = cloneStringSlice(c.Scrapers.Priority)
	cp.Scrapers.Proxy.Profiles = maps.Clone(c.Scrapers.Proxy.Profiles)
	cp.Output.Download.DownloadProxy.Profiles = maps.Clone(c.Output.Download.DownloadProxy.Profiles)

	if c.Scrapers.Overrides != nil {
		cp.Scrapers.Overrides = make(map[string]*models.ScraperSettings, len(c.Scrapers.Overrides))
		for k, v := range c.Scrapers.Overrides {
			if v == nil {
				continue
			}
			cloned := v.Clone()
			cp.Scrapers.Overrides[k] = &cloned
		}
	}

	return &cp
}

// cloneStringSlice returns a deep copy of a string slice.
// Returns nil if the input is nil.
func cloneStringSlice(s []string) []string {
	if s == nil {
		return nil
	}
	cp := make([]string, len(s))
	copy(cp, s)
	return cp
}
