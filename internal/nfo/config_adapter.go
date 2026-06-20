package nfo

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
)

// ConfigFromAppConfig converts application config to NFO generator config
// Optional db parameter enables tag database lookups if metadata config has tag_database.enabled
func ConfigFromAppConfig(appCfg *config.NFOConfig, outputCfg *config.OutputConfig, metadataCfg *config.MetadataConfig, db *database.DB) *Config {
	if appCfg == nil {
		return DefaultConfig()
	}

	groupActress := false
	var groupActressName string
	var groupUnknownActressName string
	if outputCfg != nil {
		groupActress = outputCfg.GroupActress
		groupActressName = outputCfg.GroupActressName
		groupUnknownActressName = outputCfg.GroupUnknownActressName
	}

	nfoConfig := &Config{
		ActorFirstNameOrder:     appCfg.FirstNameOrder,
		ActorJapaneseNames:      appCfg.ActressLanguageJA,
		UnknownActress:          appCfg.UnknownActressText,
		UnknownActressMode:      appCfg.UnknownActressMode,
		NFOFilenameTemplate:     appCfg.FilenameTemplate,
		PerFile:                 appCfg.PerFile,
		ActressAsTag:            appCfg.ActressAsTag,
		AddGenericRole:          appCfg.AddGenericRole,
		AltNameRole:             appCfg.AltNameRole,
		IncludeOriginalPath:     appCfg.IncludeOriginalPath,
		IncludeStreamDetails:    appCfg.IncludeStreamDetails,
		IncludeFanart:           appCfg.IncludeFanart,
		IncludeTrailer:          appCfg.IncludeTrailer,
		DefaultRatingSource:     appCfg.RatingSource,
		StaticTags:              appCfg.Tag,
		StaticTagline:           appCfg.Tagline,
		StaticCredits:           appCfg.Credits,
		GroupActress:            groupActress,
		GroupActressName:        groupActressName,
		GroupUnknownActressName: groupUnknownActressName,
	}

	// Add tag database if enabled and db provided
	if db != nil && metadataCfg != nil && metadataCfg.TagDatabase.Enabled {
		nfoConfig.TagDatabase = database.NewMovieTagRepository(db)
	}

	return nfoConfig
}
