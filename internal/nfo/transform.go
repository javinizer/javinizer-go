package nfo

import (
	"context"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// nfoRating captures a resolved rating for NFO generation.
type nfoRating struct {
	source string
	max    int
	score  float64
	votes  int
}

// nfoInput captures the fully-resolved data for NFO generation.
// All data transformation (actress dedup, stream details, tag merging,
// config-based filtering) is complete before this struct is created.
// buildNFO maps nfoInput to *Movie with no further decision logic.
type nfoInput struct {
	id            string
	contentID     string
	title         string
	originalTitle string
	plot          string
	director      string
	maker         string
	label         string
	series        string
	releaseDate   string // formatted "2006-01-02" or empty
	year          int
	runtime       int
	rating        *nfoRating // nil if no rating
	actors        []actor    // deduplicated, role-assigned
	genres        []string
	tags          []string
	thumbs        []Thumb // poster thumbs
	fanart        *fanart // nil if no fanart
	trailerURL    string
	fileInfo      *fileInfo // nil if no stream details
	originalPath  string
	tagline       string
	credits       string
}

// transformMovieForNFO handles all data transformation: actress formatting and deduplication,
// stream details extraction, date normalization, rating resolution, and tag merging.
// The returned nfoInput is fully resolved — buildNFO maps it to *Movie with no further decisions.
func (g *Generator) transformMovieForNFO(ctx context.Context, movie *models.Movie, videoFilePath string, tags []string) nfoInput {
	title := g.resolveTitle(movie)
	genres := g.resolveGenres(movie)
	actors := g.buildActors(movie.Actresses)
	releaseDate, year := g.resolveReleaseDate(movie)
	rating := g.resolveRating(movie)
	thumbs := g.resolvePosterThumbs(movie)
	fanartThumbs := g.resolveFanart(movie)
	trailerURL := g.resolveTrailer(movie)
	fi := g.resolveStreamDetails(ctx, videoFilePath)
	originalPath := g.resolveOriginalPath(movie)
	tagline := g.resolveTagline()
	credits := g.resolveCredits()

	// Tag merging: actress-as-tag + caller tags + config tags, deduplicated
	tagList := g.mergeTags(actors, tags)

	return nfoInput{
		id:            movie.ID,
		contentID:     movie.ContentID,
		title:         title,
		originalTitle: movie.OriginalTitle,
		plot:          movie.Description,
		director:      movie.Director,
		maker:         movie.Maker,
		label:         movie.Label,
		series:        movie.Series,
		releaseDate:   releaseDate,
		year:          year,
		runtime:       movie.Runtime,
		rating:        rating,
		actors:        actors,
		genres:        genres,
		tags:          tagList,
		thumbs:        thumbs,
		fanart:        fanartThumbs,
		trailerURL:    trailerURL,
		fileInfo:      fi,
		originalPath:  originalPath,
		tagline:       tagline,
		credits:       credits,
	}
}

// resolveTitle returns the display title, falling back to the base title.
func (g *Generator) resolveTitle(movie *models.Movie) string {
	if movie.DisplayTitle != "" {
		return movie.DisplayTitle
	}
	return movie.Title
}

// resolveGenres extracts genre names from the movie's genre list.
func (g *Generator) resolveGenres(movie *models.Movie) []string {
	genres := make([]string, 0, len(movie.Genres))
	for _, genre := range movie.Genres {
		genres = append(genres, genre.Name)
	}
	return genres
}

// resolveReleaseDate normalizes the release date and year from the movie.
func (g *Generator) resolveReleaseDate(movie *models.Movie) (date string, year int) {
	if movie.ReleaseDate != nil {
		return movie.ReleaseDate.Format("2006-01-02"), movie.ReleaseDate.Year()
	}
	if movie.ReleaseYear > 0 {
		return "", movie.ReleaseYear
	}
	return "", 0
}

// resolveRating builds an nfoRating if the movie has a score.
func (g *Generator) resolveRating(movie *models.Movie) *nfoRating {
	if movie.RatingScore <= 0 {
		return nil
	}
	return &nfoRating{
		source: g.config.RatingSource,
		max:    10,
		score:  movie.RatingScore,
		votes:  movie.RatingVotes,
	}
}

// resolvePosterThumbs builds poster thumbs from the movie's cover URL.
func (g *Generator) resolvePosterThumbs(movie *models.Movie) []Thumb {
	if movie.Poster.CoverURL == "" {
		return nil
	}
	return []Thumb{{Aspect: "poster", Value: movie.Poster.CoverURL}}
}

// resolveFanart builds fanart thumbs from screenshots when IncludeFanart is enabled.
func (g *Generator) resolveFanart(movie *models.Movie) *fanart {
	if !g.config.IncludeFanart || len(movie.Screenshots) == 0 {
		return nil
	}
	thumbs := make([]Thumb, 0, len(movie.Screenshots))
	for _, url := range movie.Screenshots {
		thumbs = append(thumbs, Thumb{Value: url})
	}
	return &fanart{Thumbs: thumbs}
}

// resolveTrailer returns the trailer URL when IncludeTrailer is enabled.
func (g *Generator) resolveTrailer(movie *models.Movie) string {
	if g.config.IncludeTrailer && movie.TrailerURL != "" {
		return movie.TrailerURL
	}
	return ""
}

// resolveStreamDetails extracts stream details from the video file when IncludeStreamDetails is enabled.
func (g *Generator) resolveStreamDetails(ctx context.Context, videoFilePath string) *fileInfo {
	if !g.config.IncludeStreamDetails || videoFilePath == "" {
		return nil
	}
	if streamDetails := g.extractStreamDetails(ctx, videoFilePath); streamDetails != nil {
		return &fileInfo{StreamDetails: streamDetails}
	}
	return nil
}

// resolveOriginalPath returns the original file name when IncludeOriginalPath is enabled.
func (g *Generator) resolveOriginalPath(movie *models.Movie) string {
	if g.config.IncludeOriginalPath && movie.OriginalFileName != "" {
		return movie.OriginalFileName
	}
	return ""
}

// resolveTagline returns the configured tagline.
func (g *Generator) resolveTagline() string {
	return g.config.Tagline
}

// resolveCredits returns the configured credits as a comma-separated string.
func (g *Generator) resolveCredits() string {
	if len(g.config.Credits) == 0 {
		return ""
	}
	return strings.Join(g.config.Credits, ", ")
}

// buildActors formats actresses and deduplicates by DMMID or name.
func (g *Generator) buildActors(movieActresses []models.Actress) []actor {
	if len(movieActresses) == 0 {
		return nil
	}

	actors := make([]actor, 0, len(movieActresses))
	seenDMMID := make(map[int]struct{})
	seenNames := make(map[string]struct{})

	for _, a := range movieActresses {
		actorName := g.formatActressName(a)
		nameKey := normalizeActressNameForDedup(actorName)

		if a.DMMID > 0 {
			if _, exists := seenDMMID[a.DMMID]; exists {
				continue
			}
			// Also dedupe by normalized name so the same actress isn't emitted twice
			// when one entry has a DMMID and another does not.
			if nameKey != "" {
				if _, exists := seenNames[nameKey]; exists {
					continue
				}
			}
			seenDMMID[a.DMMID] = struct{}{}
			if nameKey != "" {
				seenNames[nameKey] = struct{}{}
			}
		} else {
			if _, exists := seenNames[nameKey]; exists {
				continue
			}
			seenNames[nameKey] = struct{}{}
		}

		act := actor{
			Name:  actorName,
			Order: len(actors),
		}

		if g.config.AddGenericRole {
			act.Role = "Actress"
		}

		if g.config.AltNameRole && a.JapaneseName != "" {
			act.Role = a.JapaneseName
		}

		if a.ThumbURL != "" {
			act.Thumb = a.ThumbURL
		}

		actors = append(actors, act)
	}

	return actors
}

// mergeTags combines actress-as-tag entries, caller-provided tags, and config tags,
// deduplicating by name.
func (g *Generator) mergeTags(actors []actor, callerTags []string) []string {
	var tags []string
	tagSet := make(map[string]bool)

	addTag := func(tag string) {
		if tag != "" && !tagSet[tag] {
			tags = append(tags, tag)
			tagSet[tag] = true
		}
	}

	// Actress-as-tag (config-gated)
	if g.config.ActressAsTag {
		for _, act := range actors {
			skipUnknownTag := g.config.UnknownActressMode != models.UnknownActressModeFallback && act.Name == g.config.UnknownActressText
			if !skipUnknownTag {
				addTag(act.Name)
			}
		}
	}

	// Caller-provided tags
	for _, tag := range callerTags {
		addTag(tag)
	}

	// Config tags
	for _, tag := range g.config.Tag {
		addTag(tag)
	}

	return tags
}

// buildNFO maps a fully-resolved nfoInput to a *Movie XML struct.
// All data transformation is handled by transformMovieForNFO;
// this function performs no decision logic — pure field mapping.
func (g *Generator) buildNFO(input nfoInput) *Movie {
	nfo := &Movie{
		ID:            input.id,
		Title:         input.title,
		OriginalTitle: input.originalTitle,
		SortTitle:     input.id,
		Plot:          input.plot,
		Director:      input.director,
		Studio:        input.maker,
		Maker:         input.maker,
		Label:         input.label,
		Set:           input.series,
	}

	if input.contentID != "" {
		nfo.UniqueID = append(nfo.UniqueID, uniqueID{
			Type:    "contentid",
			Default: true,
			Value:   input.contentID,
		})
	}

	if input.releaseDate != "" {
		nfo.ReleaseDate = input.releaseDate
		nfo.Premiered = input.releaseDate
	}
	if input.year > 0 {
		nfo.Year = input.year
	}

	if input.runtime > 0 {
		nfo.Runtime = input.runtime
	}

	if input.rating != nil {
		nfo.Ratings = ratings{
			Rating: []rating{
				{
					Name:    input.rating.source,
					Max:     input.rating.max,
					Default: true,
					Value:   input.rating.score,
					Votes:   input.rating.votes,
				},
			},
		}
	}

	if len(input.actors) > 0 {
		nfo.Actors = input.actors
	}

	if len(input.genres) > 0 {
		nfo.Genres = input.genres
	}

	if len(input.thumbs) > 0 {
		nfo.Thumb = input.thumbs
	}

	if input.fanart != nil {
		nfo.Fanart = input.fanart
	}

	if input.trailerURL != "" {
		nfo.Trailer = input.trailerURL
	}

	if input.fileInfo != nil {
		nfo.FileInfo = input.fileInfo
	}

	if input.originalPath != "" {
		nfo.OriginalPath = input.originalPath
	}

	if len(input.tags) > 0 {
		nfo.Tags = input.tags
	}

	if input.tagline != "" {
		nfo.Tagline = input.tagline
	}

	if input.credits != "" {
		nfo.Credits = input.credits
	}

	return nfo
}

// normalizeActressNameForDedup normalizes an actress name for deduplication.
func normalizeActressNameForDedup(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(trimmed), " "))
}

// formatActressName formats an actress name according to config
func (g *Generator) formatActressName(actress models.Actress) string {
	return models.FormatActressName(actress, models.FormatActressNameOptions{
		JapaneseNames:      g.config.ActressLanguageJA,
		FirstNameOrder:     g.config.FirstNameOrder,
		UnknownActress:     g.config.UnknownActressText,
		UnknownActressMode: g.config.UnknownActressMode,
	})
}
