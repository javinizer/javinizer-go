package organizer

import "github.com/javinizer/javinizer-go/internal/models"

// MediaFormatConfig holds shared media file naming/format configuration.
// Both organizer.Config and downloader.Config embed this to avoid duplicating
// the 8+ identical media-format fields that both derive from *config.Config.
type MediaFormatConfig struct {
	PosterFormat            string
	FanartFormat            string
	TrailerFormat           string
	ScreenshotFormat        string
	ScreenshotFolder        string
	ScreenshotPadding       int
	GroupActress            bool
	GroupActressMin         int                       // Minimum actress count to trigger grouping (0 => 2)
	GroupActressName        string                    // Folder name when GroupActress is enabled and multiple actresses (default: "@Group")
	GroupUnknownActressName string                    // Replacement when GroupActress is enabled and the actress list is empty or unknown (default: "@Unknown")
	UnknownActressMode      models.UnknownActressMode // Governs @Unknown substitution under GroupActress: "skip" (default) suppresses it, "fallback" inserts it
	ActressFolder           string
	ActressFormat           string
	ActressDelimiter        string // Delimiter between actress names when no DELIM= modifier is present (default: ", ")
	FirstNameOrder          bool   // true = FirstName LastName, false = LastName FirstName (default: false)
	ActressLanguageJA       bool   // true = prefer JapaneseName over First/Last (mirrors nfo.actress_language_ja)
}
