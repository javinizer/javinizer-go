package nfo

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
)

type NFONameConfig struct {
	FilenameTemplate string
	GroupActress     bool
	GroupActressName string // Folder name when GroupActress is enabled and multiple actresses
	PerFile          bool
	IsMultiPart      bool
	PartSuffix       string
	FirstNameOrder   bool

	// Actress rendering options mirrored from the app config so that
	// <ACTORS>/<ACTRESSES> tag resolution picks up main's actress features
	// (group unknown substitution, JA preference, custom delimiter).
	GroupUnknownActressName string
	ActressDelimiter        string
	ActressLanguageJA       bool
}

// Config holds NFO generation settings.
// Fields are pre-resolved by the workflow bridge so that
// the nfo package does not import internal/config.
type Config struct {
	// NFO generation toggles
	PerFile              bool // Create separate NFO for each multi-part file
	IncludeFanart        bool
	IncludeTrailer       bool
	IncludeStreamDetails bool
	IncludeOriginalPath  bool
	ActressAsTag         bool
	AddGenericRole       bool // Add generic "Actress" role to all actresses
	AltNameRole          bool // Use alternate name (Japanese) in role field

	// Display settings
	FilenameTemplate   string
	RatingSource       string
	Tagline            string
	FirstNameOrder     bool
	ActressLanguageJA  bool
	UnknownActressMode models.UnknownActressMode // "skip" (default) or "fallback"
	UnknownActressText string                    // Text for fallback mode

	// Extra metadata
	Tag     []string
	Credits []string

	// Dependencies
	TemplateEngine   template.EngineInterface
	GroupActress     bool
	GroupActressName string // Folder name when GroupActress is enabled and multiple actresses (default: "@Group")

	// Actress rendering options for <ACTORS>/<ACTRESSES> template tags.
	GroupUnknownActressName string // Replacement when group_actress is enabled and the actress list is empty or unknown (default: "@Unknown")
	ActressDelimiter        string // Delimiter between actress names when no DELIM= modifier is present (default: ", ")
}

// ConfigFromAppConfig converts application config to NFO generator config.
// nameCfg is the pre-constructed NFONameConfig shared across nfo, organizer, and
// downloader bridges — constructed once in extractDomainConfigs so that overlapping
// fields (FilenameTemplate, FirstNameOrder, PerFile, GroupActress, GroupActressName)
// are read from the monolith config exactly once.
//
// Config-bridge reads: cfg.Metadata.NFO.Format.ActressLanguageJA,
// cfg.Metadata.NFO.Format.UnknownActressMode, cfg.Metadata.NFO.Format.UnknownActressText,
// cfg.Metadata.NFO.Feature.ActressAsTag, cfg.Metadata.NFO.Feature.AddGenericRole,
// cfg.Metadata.NFO.Feature.AltNameRole, cfg.Metadata.NFO.Feature.IncludeOriginalPath,
// cfg.Metadata.NFO.Feature.IncludeStreamDetails, cfg.Metadata.NFO.Feature.IncludeFanart,
// cfg.Metadata.NFO.Feature.IncludeTrailer, cfg.Metadata.NFO.Format.RatingSource,
// cfg.Metadata.NFO.Extra.Tag, cfg.Metadata.NFO.Format.Tagline, cfg.Metadata.NFO.Extra.Credits
// (Fields FilenameTemplate, FirstNameOrder, PerFile, GroupActress, GroupActressName are read via nameCfg — see NFONameConfigFromAppConfig)
func ConfigFromAppConfig(cfg *config.Config, nameCfg NFONameConfig) *Config {
	if cfg == nil {
		return nil
	}

	return &Config{
		FilenameTemplate:        nameCfg.FilenameTemplate,
		FirstNameOrder:          nameCfg.FirstNameOrder,
		ActressLanguageJA:       cfg.Metadata.NFO.Format.ActressLanguageJA,
		PerFile:                 nameCfg.PerFile,
		UnknownActressMode:      cfg.Metadata.NFO.Format.UnknownActressMode,
		UnknownActressText:      cfg.Metadata.NFO.Format.UnknownActressText,
		ActressAsTag:            cfg.Metadata.NFO.Feature.ActressAsTag,
		AddGenericRole:          cfg.Metadata.NFO.Feature.AddGenericRole,
		AltNameRole:             cfg.Metadata.NFO.Feature.AltNameRole,
		IncludeOriginalPath:     cfg.Metadata.NFO.Feature.IncludeOriginalPath,
		IncludeStreamDetails:    cfg.Metadata.NFO.Feature.IncludeStreamDetails,
		IncludeFanart:           cfg.Metadata.NFO.Feature.IncludeFanart,
		IncludeTrailer:          cfg.Metadata.NFO.Feature.IncludeTrailer,
		RatingSource:            cfg.Metadata.NFO.Format.RatingSource,
		Tag:                     cfg.Metadata.NFO.Extra.Tag,
		Tagline:                 cfg.Metadata.NFO.Format.Tagline,
		Credits:                 cfg.Metadata.NFO.Extra.Credits,
		GroupActress:            nameCfg.GroupActress,
		GroupActressName:        nameCfg.GroupActressName,
		GroupUnknownActressName: nameCfg.GroupUnknownActressName,
		ActressDelimiter:        nameCfg.ActressDelimiter,
	}
}

// ToNFONameConfig converts Config to NFONameConfig, filling in the caller-provided
// multipart fields. This eliminates manual field-by-field mapping at every call site
// that needs NFONameConfig from a Config.
func (c *Config) ToNFONameConfig(isMultiPart bool, partSuffix string) NFONameConfig {
	return NFONameConfig{
		FilenameTemplate:        c.FilenameTemplate,
		GroupActress:            c.GroupActress,
		GroupActressName:        c.GroupActressName,
		PerFile:                 c.PerFile,
		IsMultiPart:             isMultiPart,
		PartSuffix:              partSuffix,
		FirstNameOrder:          c.FirstNameOrder,
		GroupUnknownActressName: c.GroupUnknownActressName,
		ActressDelimiter:        c.ActressDelimiter,
		ActressLanguageJA:       c.ActressLanguageJA,
	}
}

// NFONameConfigFromAppConfig constructs an NFONameConfig directly from the
// application config. This is the shared construction used by extractDomainConfigs
// and available for tests that need an NFONameConfig without going through the
// full bridge chain.
//
// Config-bridge reads: cfg.Metadata.NFO.Format.FilenameTemplate, cfg.Metadata.NFO.Format.FirstNameOrder,
// cfg.Metadata.NFO.Feature.PerFile, cfg.Output.Operation.GroupActress, cfg.Output.Operation.GroupActressName,
// cfg.Output.Operation.GroupUnknownActressName, cfg.Output.Template, cfg.Output.Template.ActressDelimiter,
// cfg.Metadata.NFO.Format.ActressLanguageJA
func NFONameConfigFromAppConfig(cfg *config.Config) NFONameConfig {
	if cfg == nil {
		return NFONameConfig{}
	}
	return NFONameConfig{
		FilenameTemplate:        cfg.Metadata.NFO.Format.FilenameTemplate,
		GroupActress:            cfg.Output.Operation.GroupActress,
		GroupActressName:        cfg.Output.Operation.GroupActressName,
		PerFile:                 cfg.Metadata.NFO.Feature.PerFile,
		FirstNameOrder:          cfg.Metadata.NFO.Format.FirstNameOrder,
		GroupUnknownActressName: cfg.Output.Operation.GroupUnknownActressName,
		ActressDelimiter:        cfg.Output.Template.ActressDelimiter,
		ActressLanguageJA:       cfg.Metadata.NFO.Format.ActressLanguageJA,
	}
}
