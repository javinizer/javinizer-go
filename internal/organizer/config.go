package organizer

import (
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// Config holds the subset of application configuration needed by the Organizer.
// Per ADR-0033: fields are grouped into named sub-categories for locality —
// adding a template field doesn't touch download or subtitle code.
// OperationMode is pre-resolved from the config type by the bridge function
// so the organizer never imports config types.
type Config struct {
	// TemplateConfig controls filename and folder formatting.
	FolderFormat     string
	FileFormat       string
	SubfolderFormat  []string
	ActressDelimiter string // Delimiter between actress names when no DELIM= modifier is present (default: ", ")
	MaxTitleLength   int
	MaxPathLength    int

	// OperationConfig controls file operations and revert behavior.
	OperationMode     operationmode.OperationMode
	RenameFile        bool
	AllowRevert       bool
	FirstNameOrder    bool // true = FirstName LastName, false = LastName FirstName
	ActressLanguageJA bool // true = prefer JapaneseName over First/Last for <ACTORS>/<ACTRESS> (mirrors nfo.actress_language_ja)

	// MediaFormatConfig controls media filename templates and actress grouping.
	// Embedded so downstream code can access fields directly (e.g. cfg.PosterFormat)
	// while also passing cfg.MediaFormatConfig as a unit to helpers.
	MediaFormatConfig

	// SubtitleConfig controls subtitle file handling.
	MoveSubtitles      bool
	SubtitleExtensions []string
}

// ConfigFromAppConfig extracts Organizer-relevant fields from the application config.
// nameCfg is the pre-constructed NFONameConfig shared across nfo, organizer, and
// downloader bridges — constructed once in extractDomainConfigs so that overlapping
// fields (FirstNameOrder, GroupActress, GroupActressName) are read from the monolith
// config exactly once.
//
// Config-bridge reads: cfg.Output.Template.FolderFormat, cfg.Output.Template.FileFormat, cfg.Output.Template.SubfolderFormat,
// cfg.Output.Template.ActressDelimiter, cfg.Output.Template.MaxTitleLength, cfg.Output.Template.MaxPathLength,
// cfg.Output.GetOperationMode(), cfg.Output.Operation.RenameFile, cfg.Output.Operation.AllowRevert,
// cfg.Output.MediaFormat.PosterFormat, cfg.Output.MediaFormat.FanartFormat, cfg.Output.MediaFormat.TrailerFormat,
// cfg.Output.MediaFormat.ScreenshotFormat, cfg.Output.MediaFormat.ScreenshotFolder, cfg.Output.MediaFormat.ScreenshotPadding,
// cfg.Output.MediaFormat.ActressFolder, cfg.Output.MediaFormat.ActressFormat,
// cfg.Output.Operation.MoveSubtitles, cfg.Output.Operation.SubtitleExtensions
// (Fields FirstNameOrder, GroupActress, GroupActressName are read via nameCfg — see NFONameConfigFromAppConfig)
func ConfigFromAppConfig(cfg *config.Config, nameCfg nfo.NFONameConfig) *Config {
	if cfg == nil {
		return nil
	}
	return &Config{
		FolderFormat:      cfg.Output.Template.FolderFormat,
		FileFormat:        cfg.Output.Template.FileFormat,
		SubfolderFormat:   cfg.Output.Template.SubfolderFormat,
		ActressDelimiter:  cfg.Output.Template.ActressDelimiter,
		MaxTitleLength:    cfg.Output.Template.MaxTitleLength,
		MaxPathLength:     cfg.Output.Template.MaxPathLength,
		OperationMode:     cfg.Output.GetOperationMode(),
		RenameFile:        cfg.Output.Operation.RenameFile,
		AllowRevert:       cfg.Output.Operation.AllowRevert,
		FirstNameOrder:    nameCfg.FirstNameOrder,
		ActressLanguageJA: nameCfg.ActressLanguageJA,
		MediaFormatConfig: MediaFormatConfig{
			PosterFormat:            cfg.Output.MediaFormat.PosterFormat,
			FanartFormat:            cfg.Output.MediaFormat.FanartFormat,
			TrailerFormat:           cfg.Output.MediaFormat.TrailerFormat,
			ScreenshotFormat:        cfg.Output.MediaFormat.ScreenshotFormat,
			ScreenshotFolder:        cfg.Output.MediaFormat.ScreenshotFolder,
			ScreenshotPadding:       cfg.Output.MediaFormat.ScreenshotPadding,
			GroupActress:            nameCfg.GroupActress,
			GroupActressName:        nameCfg.GroupActressName,
			GroupUnknownActressName: nameCfg.GroupUnknownActressName,
			ActressFolder:           cfg.Output.MediaFormat.ActressFolder,
			ActressFormat:           cfg.Output.MediaFormat.ActressFormat,
		},
		MoveSubtitles:      cfg.Output.Operation.MoveSubtitles,
		SubtitleExtensions: cfg.Output.Operation.SubtitleExtensions,
	}
}
