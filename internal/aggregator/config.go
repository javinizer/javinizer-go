package aggregator

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

// MetadataConfig is the narrow view of *config.MetadataConfig that the
// Aggregator and its sub-processors actually read. Fields that the
// aggregator never touches (Translation, TagDatabase, Completeness, etc.)
// are omitted — callers cannot accidentally depend on them.
type MetadataConfig struct {
	Priority         config.PriorityConfig
	RequiredFields   []string
	IgnoreGenres     []string
	NFO              nfoConfigView
	GenreReplacement genreReplacementConfigView
	WordReplacement  wordReplacementConfigView
	ActressDatabase  actressDatabaseConfigView
}

// nfoConfigView exposes only the NFO fields the aggregator reads.
type nfoConfigView struct {
	UnknownActressMode models.UnknownActressMode
	UnknownActressText string
}

// genreReplacementConfigView exposes only the genre replacement fields the aggregator reads.
type genreReplacementConfigView struct {
	Enabled bool
	AutoAdd bool
}

// wordReplacementConfigView exposes only the word replacement fields the aggregator reads.
type wordReplacementConfigView struct {
	Enabled bool
}

// actressDatabaseConfigView exposes only the actress database fields the aggregator reads.
type actressDatabaseConfigView struct {
	Enabled      bool
	ConvertAlias bool
}

// MetadataConfigFromApp extracts the narrow aggregator view from *config.MetadataConfig.
//
// Config-bridge reads: cfg.Metadata.Priority, cfg.Metadata.RequiredFields, cfg.Metadata.IgnoreGenres,
// cfg.Metadata.NFO.Format.UnknownActressMode, cfg.Metadata.NFO.Format.UnknownActressText,
// cfg.Metadata.GenreReplacement.Enabled, cfg.Metadata.GenreReplacement.AutoAdd,
// cfg.Metadata.WordReplacement.Enabled, cfg.Metadata.ActressDatabase.Enabled,
// cfg.Metadata.ActressDatabase.ConvertAlias
func MetadataConfigFromApp(mc *config.MetadataConfig) *MetadataConfig {
	if mc == nil {
		return nil
	}
	return &MetadataConfig{
		Priority:       mc.Priority,
		RequiredFields: append([]string(nil), mc.RequiredFields...),
		IgnoreGenres:   append([]string(nil), mc.IgnoreGenres...),
		NFO: nfoConfigView{
			UnknownActressMode: mc.NFO.Format.UnknownActressMode,
			UnknownActressText: mc.NFO.Format.UnknownActressText,
		},
		GenreReplacement: genreReplacementConfigView{
			Enabled: mc.GenreReplacement.Enabled,
			AutoAdd: mc.GenreReplacement.AutoAdd,
		},
		WordReplacement: wordReplacementConfigView{
			Enabled: mc.WordReplacement.Enabled,
		},
		ActressDatabase: actressDatabaseConfigView{
			Enabled:      mc.ActressDatabase.Enabled,
			ConvertAlias: mc.ActressDatabase.ConvertAlias,
		},
	}
}

// IsUnknownActressFallback returns true when the NFO unknown actress mode is "fallback".
func (n nfoConfigView) IsUnknownActressFallback() bool {
	return n.UnknownActressMode == models.UnknownActressModeFallback
}

// Config holds the subset of application configuration needed by the Aggregator.
type Config struct {
	Metadata         *MetadataConfig
	ScrapersPriority []string
	// Scrapers was removed — priority resolution uses ScrapersPriority exclusively.
	// See ADR-0042 for rationale.
	TemplateEngine template.EngineInterface // Optional: override template engine
}

// ConfigFromAppConfig extracts Aggregator-relevant fields from the application config.
//
// Config-bridge reads: cfg.Metadata (via MetadataConfigFromApp), cfg.Scrapers.Priority
func ConfigFromAppConfig(cfg *config.Config) *Config {
	if cfg == nil {
		return nil
	}
	return &Config{
		Metadata:         MetadataConfigFromApp(&cfg.Metadata),
		ScrapersPriority: append([]string(nil), cfg.Scrapers.Priority...),
	}
}
