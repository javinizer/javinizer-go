package nfo

import (
	"encoding/xml"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mediainfo"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
)

// Generator creates NFO files from movie metadata
type Generator struct {
	fs             afero.Fs
	templateEngine *template.Engine
	config         *Config
}

// Config holds NFO generation settings
type Config struct {
	// Actress name formatting
	ActorFirstNameOrder bool   // true = FirstName LastName, false = LastName FirstName
	ActorJapaneseNames  bool   // Use Japanese names if available
	UnknownActress      string // Placeholder for unknown actresses (default: "Unknown")

	// File naming
	NFOFilenameTemplate string // Template for NFO filename (default: "<ID>.nfo")
	PerFile             bool   // Create separate NFO for each multi-part file (default: false)

	// Optional fields
	ActressAsTag         bool // Copy actress names to <tag> elements
	IncludeOriginalPath  bool // Include source filename in NFO
	IncludeStreamDetails bool // Include video/audio stream information
	IncludeFanart        bool // Include fanart section
	IncludeTrailer       bool // Include trailer URLs

	// Actress role options
	AddGenericRole bool // Add generic "Actress" role to all actresses (default: false)
	AltNameRole    bool // Use alternate name (Japanese) in role field instead of name (default: false)

	// Rating source
	DefaultRatingSource string // Which rating to mark as default (default: "themoviedb")

	// Static NFO fields
	StaticTags    []string // Static tags to add to all NFOs
	StaticTagline string   // Static tagline for all NFOs
	StaticCredits []string // Static credits for all NFOs

	// Database integration
	TagDatabase *database.MovieTagRepository // Optional tag database for per-movie tags

	// Output configuration
	GroupActress bool // Replace multiple actresses with "@Group" (default: false)
}

// NewGenerator creates a new NFO generator
func NewGenerator(fs afero.Fs, cfg *Config) *Generator {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Ensure defaults
	if cfg.UnknownActress == "" {
		cfg.UnknownActress = "Unknown"
	}
	if cfg.NFOFilenameTemplate == "" {
		cfg.NFOFilenameTemplate = "<ID>.nfo"
	}

	return &Generator{
		fs:             fs,
		templateEngine: template.NewEngine(),
		config:         cfg,
	}
}

// DefaultConfig returns default NFO generation settings
func DefaultConfig() *Config {
	return &Config{
		ActorFirstNameOrder:  true,
		ActorJapaneseNames:   false,
		UnknownActress:       "Unknown",
		NFOFilenameTemplate:  "<ID>.nfo",
		IncludeStreamDetails: false,
		IncludeFanart:        true,
		IncludeTrailer:       true,
		DefaultRatingSource:  "themoviedb",
	}
}

// Generate creates an NFO file from a Movie model
// partSuffix: optional suffix for multi-part files (e.g., "-pt1", "-A")
// videoFilePath: optional path to video file for extracting stream details (empty string to skip)
func (g *Generator) Generate(movie *models.Movie, outputPath string, partSuffix string, videoFilePath string) error {
	nfo := g.MovieToNFO(movie, videoFilePath)

	// Generate filename using template
	ctx := template.NewContextFromMovie(movie)
	ctx.GroupActress = g.config.GroupActress
	filename, err := g.templateEngine.Execute(g.config.NFOFilenameTemplate, ctx)
	if err != nil {
		return fmt.Errorf("failed to generate NFO filename: %w", err)
	}

	// Sanitize filename
	filename = template.SanitizeFilename(filename)

	// Remove .nfo extension if present (we'll add it back at the end)
	filename = strings.TrimSuffix(filename, ".nfo")

	// Append part suffix before extension (if provided and per_file is enabled)
	if partSuffix != "" && g.config.PerFile {
		filename = filename + partSuffix
	}

	// Ensure .nfo extension
	filename += ".nfo"

	fullPath := filepath.Join(outputPath, filename)

	return g.WriteNFO(nfo, fullPath)
}

// MovieToNFO converts a Movie model to NFO format
// videoFilePath: optional path to video file for extracting stream details (empty string to skip)
func (g *Generator) MovieToNFO(movie *models.Movie, videoFilePath string) *Movie {
	// Use DisplayName if available (contains templated title), otherwise fall back to Title
	title := movie.Title
	if movie.DisplayName != "" {
		title = movie.DisplayName
	}

	nfo := &Movie{
		ID:            movie.ID,
		Title:         title,
		OriginalTitle: movie.OriginalTitle,
		SortTitle:     movie.ID, // Use ID for sorting
		Plot:          movie.Description,
		Director:      movie.Director,
		Studio:        movie.Maker,
		Maker:         movie.Maker, // Custom JAV field
		Label:         movie.Label, // Custom JAV field
		Set:           movie.Series,
	}

	// Add unique IDs
	if movie.ContentID != "" {
		nfo.UniqueID = append(nfo.UniqueID, UniqueID{
			Type:    "contentid",
			Default: true,
			Value:   movie.ContentID,
		})
	}

	// Add release date
	if movie.ReleaseDate != nil {
		nfo.ReleaseDate = movie.ReleaseDate.Format("2006-01-02")
		nfo.Premiered = movie.ReleaseDate.Format("2006-01-02")
		nfo.Year = movie.ReleaseDate.Year()
	}

	// Add runtime
	if movie.Runtime > 0 {
		nfo.Runtime = movie.Runtime
	}

	// Add rating
	if movie.RatingScore > 0 {
		nfo.Ratings = Ratings{
			Rating: []Rating{
				{
					Name:    g.config.DefaultRatingSource,
					Max:     10,
					Default: true,
					Value:   movie.RatingScore,
					Votes:   movie.RatingVotes,
				},
			},
		}
	}

	// Add actresses
	if len(movie.Actresses) > 0 {
		nfo.Actors = make([]Actor, 0, len(movie.Actresses))
		for i, actress := range movie.Actresses {
			actorName := g.formatActressName(actress)
			actor := Actor{
				Name:  actorName,
				Order: i,
			}

			// Add generic role if configured
			if g.config.AddGenericRole {
				actor.Role = "Actress"
			}

			// Use alternate name (Japanese) in role field if configured
			if g.config.AltNameRole && actress.JapaneseName != "" {
				actor.Role = actress.JapaneseName
			}

			// Add actress image if available
			if actress.ThumbURL != "" {
				actor.Thumb = actress.ThumbURL
			}

			nfo.Actors = append(nfo.Actors, actor)
		}
	}

	// Add genres
	if len(movie.Genres) > 0 {
		nfo.Genres = make([]string, 0, len(movie.Genres))
		for _, genre := range movie.Genres {
			nfo.Genres = append(nfo.Genres, genre.Name)
		}
	}

	// Add cover image
	if movie.CoverURL != "" {
		nfo.Thumb = []Thumb{
			{
				Aspect: "poster",
				Value:  movie.CoverURL,
			},
		}
	}

	// Add fanart (background images)
	if g.config.IncludeFanart && len(movie.Screenshots) > 0 {
		fanart := &Fanart{
			Thumbs: make([]Thumb, 0, len(movie.Screenshots)),
		}
		for _, url := range movie.Screenshots {
			fanart.Thumbs = append(fanart.Thumbs, Thumb{
				Value: url,
			})
		}
		nfo.Fanart = fanart
	}

	// Add trailer
	if g.config.IncludeTrailer && movie.TrailerURL != "" {
		nfo.Trailer = movie.TrailerURL
	}

	// Add stream details if enabled and video file path provided
	if g.config.IncludeStreamDetails && videoFilePath != "" {
		if streamDetails := g.extractStreamDetails(videoFilePath); streamDetails != nil {
			nfo.FileInfo = &FileInfo{
				StreamDetails: streamDetails,
			}
		}
	}

	// Add original filename if enabled
	if g.config.IncludeOriginalPath && movie.OriginalFileName != "" {
		nfo.OriginalPath = movie.OriginalFileName
	}

	// Add actress names as tags if enabled
	if g.config.ActressAsTag && len(movie.Actresses) > 0 {
		// Create set of existing tags for deduplication
		tagSet := make(map[string]bool)
		for _, tag := range nfo.Tags {
			tagSet[tag] = true
		}

		// Add actress names as tags
		for _, actress := range movie.Actresses {
			actressName := g.formatActressName(actress)
			if actressName != "" && actressName != g.config.UnknownActress && !tagSet[actressName] {
				nfo.Tags = append(nfo.Tags, actressName)
				tagSet[actressName] = true
			}
		}
	}

	// Add movie-specific tags from database
	if g.config.TagDatabase != nil {
		movieTags, err := g.config.TagDatabase.GetTagsForMovie(movie.ID)
		if err == nil && len(movieTags) > 0 {
			// Create or reuse tagSet for deduplication
			tagSet := make(map[string]bool)
			for _, tag := range nfo.Tags {
				tagSet[tag] = true
			}

			// Add database tags
			for _, tag := range movieTags {
				if tag != "" && !tagSet[tag] {
					nfo.Tags = append(nfo.Tags, tag)
					tagSet[tag] = true
				}
			}
		}
	}

	// Add static tags from config
	if len(g.config.StaticTags) > 0 {
		// Create set for deduplication if not already created
		tagSet := make(map[string]bool)
		for _, tag := range nfo.Tags {
			tagSet[tag] = true
		}

		// Add static tags
		for _, tag := range g.config.StaticTags {
			if tag != "" && !tagSet[tag] {
				nfo.Tags = append(nfo.Tags, tag)
				tagSet[tag] = true
			}
		}
	}

	// Add static tagline from config
	if g.config.StaticTagline != "" {
		nfo.Tagline = g.config.StaticTagline
	}

	// Add static credits from config
	if len(g.config.StaticCredits) > 0 {
		// Join credits with comma separator for NFO XML
		nfo.Credits = strings.Join(g.config.StaticCredits, ", ")
	}

	return nfo
}

// formatActressName formats an actress name according to config
func (g *Generator) formatActressName(actress models.Actress) string {
	return g.formatActressNameFromInfo(actress.FirstName, actress.LastName, actress.JapaneseName)
}

// formatActressNameFromActressInfo formats an actress name from ActressInfo
func (g *Generator) formatActressNameFromActressInfo(actress models.ActressInfo) string {
	return g.formatActressNameFromInfo(actress.FirstName, actress.LastName, actress.JapaneseName)
}

// formatActressNameFromInfo is the common implementation for formatting actress names
func (g *Generator) formatActressNameFromInfo(firstName, lastName, japaneseName string) string {
	// Use Japanese name if configured and available
	if g.config.ActorJapaneseNames && japaneseName != "" {
		return japaneseName
	}

	// Handle unknown actress - fall back to Japanese name if available
	if firstName == "" && lastName == "" {
		if japaneseName != "" {
			return japaneseName // Better to show Japanese name than "Unknown"
		}
		return g.config.UnknownActress
	}

	// Format based on name order preference
	if g.config.ActorFirstNameOrder {
		// FirstName LastName
		if firstName != "" && lastName != "" {
			return firstName + " " + lastName
		}
		if firstName != "" {
			return firstName
		}
		return lastName
	}

	// LastName FirstName
	if firstName != "" && lastName != "" {
		return lastName + " " + firstName
	}
	if lastName != "" {
		return lastName
	}
	return firstName
}

// WriteNFO writes an NFO structure to a file
func (g *Generator) WriteNFO(nfo *Movie, path string) error {
	// Ensure output directory exists
	dir := filepath.Dir(path)
	if err := g.fs.MkdirAll(dir, 0777); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create file
	file, err := g.fs.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create NFO file: %w", err)
	}
	defer func() { _ = file.Close() }()

	// Write XML header
	if _, err := file.WriteString(xml.Header); err != nil {
		return fmt.Errorf("failed to write XML header: %w", err)
	}

	// Create encoder with indentation
	encoder := xml.NewEncoder(file)
	encoder.Indent("", "  ")

	// Encode NFO
	if err := encoder.Encode(nfo); err != nil {
		return fmt.Errorf("failed to encode NFO: %w", err)
	}

	// Write final newline
	if _, err := file.WriteString("\n"); err != nil {
		return fmt.Errorf("failed to write final newline: %w", err)
	}

	return nil
}

// GenerateFromScraperResult creates an NFO from a ScraperResult
func (g *Generator) GenerateFromScraperResult(result *models.ScraperResult, outputPath string) error {
	// Convert ScraperResult to NFO
	nfo := g.ScraperResultToNFO(result)

	// Generate filename using template
	ctx := template.NewContextFromScraperResult(result)
	filename, err := g.templateEngine.Execute(g.config.NFOFilenameTemplate, ctx)
	if err != nil {
		return fmt.Errorf("failed to generate NFO filename: %w", err)
	}

	// Sanitize filename
	filename = template.SanitizeFilename(filename)

	// Ensure .nfo extension
	if !strings.HasSuffix(filename, ".nfo") {
		filename += ".nfo"
	}

	fullPath := filepath.Join(outputPath, filename)

	return g.WriteNFO(nfo, fullPath)
}

// ScraperResultToNFO converts a ScraperResult to NFO format
func (g *Generator) ScraperResultToNFO(result *models.ScraperResult) *Movie {
	nfo := &Movie{
		ID:            result.ID,
		Title:         result.Title,
		OriginalTitle: result.OriginalTitle,
		SortTitle:     result.ID,
		Plot:          result.Description,
		Director:      result.Director,
		Studio:        result.Maker,
		Maker:         result.Maker,
		Label:         result.Label,
		Set:           result.Series,
	}

	// Add unique IDs
	if result.ContentID != "" {
		nfo.UniqueID = append(nfo.UniqueID, UniqueID{
			Type:    "contentid",
			Default: true,
			Value:   result.ContentID,
		})
	}

	// Add release date
	if result.ReleaseDate != nil {
		nfo.ReleaseDate = result.ReleaseDate.Format("2006-01-02")
		nfo.Premiered = result.ReleaseDate.Format("2006-01-02")
		nfo.Year = result.ReleaseDate.Year()
	}

	// Add runtime
	if result.Runtime > 0 {
		nfo.Runtime = result.Runtime
	}

	// Add rating
	if result.Rating != nil {
		nfo.Ratings = Ratings{
			Rating: []Rating{
				{
					Name:    g.config.DefaultRatingSource,
					Max:     10,
					Default: true,
					Value:   result.Rating.Score,
					Votes:   result.Rating.Votes,
				},
			},
		}
	}

	// Add actresses
	if len(result.Actresses) > 0 {
		nfo.Actors = make([]Actor, 0, len(result.Actresses))
		for i, actress := range result.Actresses {
			actorName := g.formatActressNameFromActressInfo(actress)
			actor := Actor{
				Name:  actorName,
				Order: i,
			}

			// Add generic role if configured
			if g.config.AddGenericRole {
				actor.Role = "Actress"
			}

			// Use alternate name (Japanese) in role field if configured
			if g.config.AltNameRole && actress.JapaneseName != "" {
				actor.Role = actress.JapaneseName
			}

			if actress.ThumbURL != "" {
				actor.Thumb = actress.ThumbURL
			}

			nfo.Actors = append(nfo.Actors, actor)
		}
	}

	// Add genres
	if len(result.Genres) > 0 {
		nfo.Genres = result.Genres
	}

	// Add cover image
	if result.CoverURL != "" {
		nfo.Thumb = []Thumb{
			{
				Aspect: "poster",
				Value:  result.CoverURL,
			},
		}
	}

	// Add fanart
	if g.config.IncludeFanart && len(result.ScreenshotURL) > 0 {
		fanart := &Fanart{
			Thumbs: make([]Thumb, 0, len(result.ScreenshotURL)),
		}
		for _, url := range result.ScreenshotURL {
			fanart.Thumbs = append(fanart.Thumbs, Thumb{
				Value: url,
			})
		}
		nfo.Fanart = fanart
	}

	// Add trailer
	if g.config.IncludeTrailer && result.TrailerURL != "" {
		nfo.Trailer = result.TrailerURL
	}

	return nfo
}

// extractStreamDetails extracts video/audio stream information from a video file
func (g *Generator) extractStreamDetails(videoFilePath string) *StreamDetails {
	// Extract media information
	info, err := mediainfo.Analyze(videoFilePath)
	if err != nil {
		// Silently fail - stream details are optional
		return nil
	}

	streamDetails := &StreamDetails{}

	// Add video stream
	if info.Width > 0 && info.Height > 0 {
		videoStream := VideoStream{
			Codec:  info.VideoCodec,
			Aspect: info.AspectRatio,
			Width:  info.Width,
			Height: info.Height,
		}

		if info.Duration > 0 {
			videoStream.DurationInSeconds = int(info.Duration)
		}

		streamDetails.Video = []VideoStream{videoStream}
	}

	// Add audio stream
	if info.AudioCodec != "" {
		audioStream := AudioStream{
			Codec:    info.AudioCodec,
			Channels: info.AudioChannels,
		}

		streamDetails.Audio = []AudioStream{audioStream}
	}

	// Return nil if no streams were added
	if len(streamDetails.Video) == 0 && len(streamDetails.Audio) == 0 {
		return nil
	}

	return streamDetails
}
