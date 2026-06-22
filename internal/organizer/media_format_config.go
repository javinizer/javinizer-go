package organizer

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
	GroupActressName        string // Folder name when GroupActress is enabled and multiple actresses (default: "@Group")
	GroupUnknownActressName string // Replacement when GroupActress is enabled and the actress list is empty or unknown (default: "@Unknown")
	ActressFolder           string
	ActressFormat           string
}
