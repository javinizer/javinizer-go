package nfo

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/spf13/afero"
)

// ParseResult contains the parsed NFO data and any warnings
type ParseResult struct {
	Movie    *models.Movie
	Warnings []string // Non-fatal parsing issues
	Source   string   // File path for debugging
	NFOTitle string   // Raw <title> from NFO, used for display title preservation
}

// Maximum NFO file size (1 MB) - prevents memory exhaustion attacks
const maxNFOSize = 1 << 20 // 1 MiB

// ParseNFO parses a Kodi-compatible NFO file into a models.Movie struct
// Uses streaming XML parsing with a size limit to prevent memory exhaustion.
func ParseNFO(fs afero.Fs, filePath string) (*ParseResult, error) {
	// Open file
	f, err := fs.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read NFO file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Limit reader to prevent memory exhaustion on large files
	limited := io.LimitReader(f, maxNFOSize)

	// Parse XML using streaming decoder
	decoder := xml.NewDecoder(limited)
	var nfoMovie Movie
	if err := decoder.Decode(&nfoMovie); err != nil {
		// Check if error is due to size limit
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil, fmt.Errorf("NFO file exceeds maximum size of %d bytes", maxNFOSize)
		}
		return nil, fmt.Errorf("failed to parse NFO XML: %w", err)
	}

	// Convert to models.Movie
	movie, warnings := nfoToMovie(&nfoMovie)

	return &ParseResult{
		Movie:    movie,
		Warnings: warnings,
		Source:   filePath,
		NFOTitle: nfoMovie.Title,
	}, nil
}

// resolveNFOIDs extracts the movie ID and ContentID from NFO uniqueid elements.
// If ContentID is not found via uniqueid, ID is used as fallback.
func resolveNFOIDs(nfo *Movie) (id, contentID string) {
	id = nfo.ID

	for _, uid := range nfo.UniqueID {
		if uid.Type == "contentid" && uid.Value != "" {
			contentID = uid.Value
			break
		}
	}

	// If ContentID is still empty, use ID as fallback
	if contentID == "" && id != "" {
		contentID = id
	}

	return id, contentID
}

// resolveNFORatings extracts the default or first rating from NFO ratings.
func resolveNFORatings(nfo *Movie) (score float64, votes int) {
	if len(nfo.Ratings.Rating) == 0 {
		return 0, 0
	}

	var rating *rating
	for i := range nfo.Ratings.Rating {
		if nfo.Ratings.Rating[i].Default {
			rating = &nfo.Ratings.Rating[i]
			break
		}
	}
	if rating == nil {
		rating = &nfo.Ratings.Rating[0]
	}

	return rating.Value, rating.Votes
}

// resolveNFOMedia extracts cover URL and screenshot URLs from NFO thumbs and fanart.
func resolveNFOMedia(nfo *Movie) (coverURL string, screenshots []string) {
	// Extract cover URL from thumbs
	for _, thumb := range nfo.Thumb {
		if thumb.Aspect == "poster" && thumb.Value != "" {
			coverURL = thumb.Value
			break
		}
	}
	// Fallback to first non-empty thumb if no poster aspect found
	if coverURL == "" {
		for _, thumb := range nfo.Thumb {
			if thumb.Value != "" {
				coverURL = thumb.Value
				break
			}
		}
	}

	// Extract screenshot URLs from fanart
	if nfo.Fanart != nil && len(nfo.Fanart.Thumbs) > 0 {
		screenshots = make([]string, 0, len(nfo.Fanart.Thumbs))
		for _, thumb := range nfo.Fanart.Thumbs {
			if thumb.Value != "" {
				screenshots = append(screenshots, thumb.Value)
			}
		}
	}

	return coverURL, screenshots
}

// nfoToMovie converts an NFO Movie struct to a models.Movie
func nfoToMovie(nfo *Movie) (*models.Movie, []string) {
	var warnings []string

	// Construct movie with basic field mapping
	movie := &models.Movie{
		Title:         nfo.Title,
		OriginalTitle: nfo.OriginalTitle,
		Description:   nfo.Plot,
		Director:      nfo.Director,
		Maker:         nfo.Maker,
		Label:         nfo.Label,
		Series:        nfo.Set,
		Runtime:       nfo.Runtime,
		ReleaseYear:   nfo.Year,
	}

	// Fallback: Use Studio if Maker is empty (standard Kodi field → JAV custom field)
	if movie.Maker == "" && nfo.Studio != "" {
		movie.Maker = nfo.Studio
	}

	// Resolve IDs
	movie.ID, movie.ContentID = resolveNFOIDs(nfo)

	// Resolve release date (prefer ReleaseDate over Premiered)
	dateStr := nfo.ReleaseDate
	if dateStr == "" {
		dateStr = nfo.Premiered
	}
	if dateStr != "" {
		if parsedDate, err := parseDate(dateStr); err == nil {
			movie.ReleaseDate = &parsedDate
			// Update ReleaseYear from parsed date if not set
			if movie.ReleaseYear == 0 {
				movie.ReleaseYear = parsedDate.Year()
			}
		} else {
			warnings = append(warnings, fmt.Sprintf("failed to parse date %q: %v", dateStr, err))
		}
	}

	// Resolve ratings
	movie.RatingScore, movie.RatingVotes = resolveNFORatings(nfo)

	// Convert actors to actresses
	if len(nfo.Actors) > 0 {
		movie.Actresses = make([]models.Actress, 0, len(nfo.Actors))
		for _, actor := range nfo.Actors {
			actress := parseActorToActress(actor)
			movie.Actresses = append(movie.Actresses, actress)
		}
	}

	// Convert genres
	if len(nfo.Genres) > 0 {
		movie.Genres = make([]models.Genre, 0, len(nfo.Genres))
		genreMap := make(map[string]bool) // For deduplication
		for _, genreName := range nfo.Genres {
			genreName = strings.TrimSpace(genreName)
			if genreName != "" && !genreMap[genreName] {
				movie.Genres = append(movie.Genres, models.Genre{Name: genreName})
				genreMap[genreName] = true
			}
		}
	}

	// Resolve media (cover and screenshots)
	coverURL, screenshots := resolveNFOMedia(nfo)
	movie.Poster.CoverURL = coverURL
	movie.Screenshots = screenshots

	// Extract trailer URL
	if nfo.Trailer != "" {
		movie.TrailerURL = nfo.Trailer
	}

	// Extract original filename
	if nfo.OriginalPath != "" {
		movie.OriginalFileName = nfo.OriginalPath
	}

	// Set source info
	movie.SourceName = "nfo"

	return movie, warnings
}

// parseActorToActress converts an NFO Actor to a models.Actress
func parseActorToActress(actor actor) models.Actress {
	actress := models.Actress{
		ThumbURL: actor.Thumb,
	}

	// Determine which field has Japanese and which has romanized text
	nameHasJapanese := scraperutil.HasJapanese(actor.Name)
	altNameHasJapanese := scraperutil.HasJapanese(actor.AltName)
	roleHasJapanese := actor.Role != "" && scraperutil.HasJapanese(actor.Role)

	// Priority for FirstName/LastName (romanized):
	// 1. Use AltName if it's romanized (not Japanese)
	// 2. Use Name if it's romanized (not Japanese)
	// 3. Otherwise leave empty (both are Japanese)
	if actor.AltName != "" && !altNameHasJapanese {
		// AltName is romanized, use it
		firstName, lastName := models.SplitFullName(actor.AltName)
		actress.FirstName = firstName
		actress.LastName = lastName
	} else if actor.Name != "" && !nameHasJapanese {
		// Name is romanized, use it
		firstName, lastName := models.SplitFullName(actor.Name)
		actress.FirstName = firstName
		actress.LastName = lastName
	}

	// Priority for JapaneseName:
	// 1. Use Role if it contains Japanese (custom JAV field)
	// 2. Use AltName if it contains Japanese (reverse format NFO)
	// 3. Use Name if it contains Japanese (standard Kodi field)
	if roleHasJapanese {
		actress.JapaneseName = actor.Role
	} else if altNameHasJapanese {
		actress.JapaneseName = actor.AltName
	} else if nameHasJapanese {
		actress.JapaneseName = actor.Name
	}

	return actress
}

// parseDate parses various date formats commonly found in NFO files
func parseDate(dateStr string) (time.Time, error) {
	dateStr = strings.TrimSpace(dateStr)
	if dateStr == "" {
		return time.Time{}, fmt.Errorf("empty date string")
	}

	// Try RFC3339 formats first (most strict, timezone-aware)
	if t, err := time.Parse(time.RFC3339, dateStr); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339Nano, dateStr); err == nil {
		return t, nil
	}

	// Try common formats with timezone support
	formats := []string{
		"2006-01-02",                // YYYY-MM-DD (ISO 8601, most common)
		"2006/01/02",                // YYYY/MM/DD
		"2006-01-02 15:04:05",       // YYYY-MM-DD HH:MM:SS
		"2006-01-02T15:04:05Z",      // ISO 8601 with time
		"2006-01-02T15:04:05Z07:00", // ISO 8601 with timezone offset
		"2006-01-02T15:04:05-07:00", // ISO 8601 with negative offset
		"02-01-2006",                // DD-MM-YYYY
		"01/02/2006",                // MM/DD/YYYY (ambiguous, US format)
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse date %q with known formats", dateStr)
}
