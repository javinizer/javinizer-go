package formatter

import (
	"fmt"
	"io"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// fieldSpec defines a conditional table row: if condition() is true,
// the row is included with the given label and value from valueFunc().
type fieldSpec struct {
	label     string
	valueFunc func() string
	condition func() bool
}

// WriteMovie writes formatted movie metadata to w.
// It renders a key-value table with sections for metadata, source URLs,
// media URLs, and description. Accessible to CLI, TUI, and API packages.
func WriteMovie(w io.Writer, movie *models.Movie, scraperResults []*models.ScraperResult) {
	if movie == nil {
		return
	}
	fmt.Fprintln(w)

	rows := buildMovieRows(movie)
	renderMovieTable(w, rows, movie, scraperResults)
}

// buildMovieRows produces the data rows for the movie metadata table.
// It separates data assembly from rendering so that row construction
// can be tested independently of output formatting.
func buildMovieRows(movie *models.Movie) [][]string {
	rows := [][]string{}

	specs := movieFieldSpecs(movie)
	for _, spec := range specs {
		if spec.condition() {
			rows = append(rows, []string{spec.label, spec.valueFunc()})
		}
	}

	// Actresses — multi-row section (not a simple fieldSpec)
	if len(movie.Actresses) > 0 {
		rows = appendActressRows(rows, movie.Actresses)
	}

	// Genres
	if len(movie.Genres) > 0 {
		rows = append(rows, []string{"Genres", formatGenres(movie.Genres)})
	}

	// Translations
	if len(movie.Translations) > 1 {
		rows = append(rows, []string{"Translations", formatTranslations(movie.Translations)})
	}

	// Sources
	if sources := collectSources(movie); len(sources) > 0 {
		rows = append(rows, []string{"Sources", strings.Join(sources, ", ")})
	}

	return rows
}

// movieFieldSpecs returns the ordered list of simple conditional fields
// for the movie metadata table. Each spec evaluates lazily.
func movieFieldSpecs(movie *models.Movie) []fieldSpec {
	return []fieldSpec{
		{
			label:     "ID",
			valueFunc: func() string { return movie.ID },
			condition: func() bool { return true },
		},
		{
			label:     "ContentID",
			valueFunc: func() string { return movie.ContentID },
			condition: func() bool { return movie.ContentID != "" && movie.ContentID != movie.ID },
		},
		{
			label:     "Title",
			valueFunc: func() string { return movie.Title },
			condition: func() bool { return movie.Title != "" },
		},
		{
			label:     "ReleaseDate",
			valueFunc: func() string { return movie.ReleaseDate.Format("2006-01-02") },
			condition: func() bool { return movie.ReleaseDate != nil },
		},
		{
			label:     "Runtime",
			valueFunc: func() string { return fmt.Sprintf("%d min", movie.Runtime) },
			condition: func() bool { return movie.Runtime > 0 },
		},
		{
			label:     "Director",
			valueFunc: func() string { return movie.Director },
			condition: func() bool { return movie.Director != "" },
		},
		{
			label:     "Maker",
			valueFunc: func() string { return movie.Maker },
			condition: func() bool { return movie.Maker != "" },
		},
		{
			label:     "Label",
			valueFunc: func() string { return movie.Label },
			condition: func() bool { return movie.Label != "" },
		},
		{
			label:     "Series",
			valueFunc: func() string { return movie.Series },
			condition: func() bool { return movie.Series != "" },
		},
		{
			label:     "Rating",
			valueFunc: func() string { return formatRating(movie.RatingScore, movie.RatingVotes) },
			condition: func() bool { return movie.RatingScore > 0 },
		},
	}
}

// appendActressRows adds actress detail rows to the table.
func appendActressRows(rows [][]string, actresses []models.Actress) [][]string {
	actressHeader := fmt.Sprintf("Actresses (%d)", len(actresses))
	rows = append(rows, []string{actressHeader, ""})

	for i, actress := range actresses {
		name := actress.FullName()
		if actress.JapaneseName != "" && name != actress.JapaneseName {
			name += fmt.Sprintf(" (%s)", actress.JapaneseName)
		}

		actressLine := fmt.Sprintf("  [%d] %s", i+1, name)
		if actress.DMMID > 0 {
			actressLine += fmt.Sprintf(" - ID: %d", actress.DMMID)
		}
		rows = append(rows, []string{"", actressLine})

		if actress.ThumbURL != "" {
			rows = append(rows, []string{"", fmt.Sprintf("      Thumb: %s", actress.ThumbURL)})
		}
	}
	return rows
}

// formatGenres formats the genre list, truncating after 8 if there are more than 9.
func formatGenres(genres []models.Genre) string {
	genreNames := make([]string, 0, len(genres))
	for i, genre := range genres {
		if i < 8 || len(genres) <= 9 {
			genreNames = append(genreNames, genre.Name)
		} else if i == 8 {
			genreNames = append(genreNames, fmt.Sprintf("... and %d more", len(genres)-8))
			break
		}
	}
	return strings.Join(genreNames, ", ")
}

// formatTranslations formats the translation list with human-readable language names.
func formatTranslations(translations []models.MovieTranslation) string {
	langNames := []string{}
	for _, trans := range translations {
		langName := map[string]string{
			"en": "English",
			"ja": "Japanese",
			"zh": "Chinese",
			"ko": "Korean",
		}[trans.Language]
		if langName == "" {
			langName = trans.Language
		}
		langNames = append(langNames, fmt.Sprintf("%s (%s)", langName, trans.SourceName))
	}
	return strings.Join(langNames, ", ")
}

// formatRating formats the rating display string.
func formatRating(score float64, votes int) string {
	if score > 0 && votes > 0 {
		return fmt.Sprintf("%.1f/10 (%d votes)", score, votes)
	}
	return fmt.Sprintf("%.1f/10", score)
}

// collectSources gathers unique source names from translations,
// falling back to movie.SourceName if no translations exist.
func collectSources(movie *models.Movie) []string {
	sourcesMap := make(map[string]bool)
	var sources []string

	for _, trans := range movie.Translations {
		if trans.SourceName != "" && !sourcesMap[trans.SourceName] {
			sourcesMap[trans.SourceName] = true
			sources = append(sources, trans.SourceName)
		}
	}

	if len(sources) == 0 && movie.SourceName != "" {
		sources = append(sources, movie.SourceName)
	}

	return sources
}

// renderMovieTable handles all rendering logic for the movie display.
// It receives pre-built data rows and renders them along with
// the source URLs, media URLs, and description sections.
func renderMovieTable(w io.Writer, rows [][]string, movie *models.Movie, scraperResults []*models.ScraperResult) {
	// Calculate column widths
	maxLabelWidth := 0
	for _, row := range rows {
		if len(row[0]) > maxLabelWidth {
			maxLabelWidth = len(row[0])
		}
	}

	// Terminal width for wrapping (default 120, can be adjusted)
	terminalWidth := 120
	valueWidth := terminalWidth - maxLabelWidth - 5 // Account for label, " : ", and padding

	// Print table header
	fmt.Fprintln(w, strings.Repeat("-", maxLabelWidth+2)+" "+strings.Repeat("-", 100))

	// Print rows with proper wrapping
	for _, row := range rows {
		label := row[0]
		value := row[1]

		// For multi-line values (description, media URLs), wrap them
		lines := wrapText(value, valueWidth)

		for i, line := range lines {
			if i == 0 {
				// First line: show label
				paddedLabel := label + strings.Repeat(" ", maxLabelWidth-len(label))
				fmt.Fprintf(w, "%-*s : %s\n", maxLabelWidth, paddedLabel, line)
			} else {
				fmt.Fprintf(w, "%*s   %s\n", maxLabelWidth, "", line)
			}
		}
	}

	// Print Source URLs section (if we have scraper results from fresh scrape)
	if len(scraperResults) > 0 {
		fmt.Fprintln(w, strings.Repeat("-", maxLabelWidth+2)+" "+strings.Repeat("-", 100))
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Source URLs:")
		fmt.Fprintln(w)

		for _, result := range scraperResults {
			fmt.Fprintf(w, "  %-12s : %s\n", result.Source, result.SourceURL)
		}
	}

	// Now print expanded media section
	if movie.Poster.CoverURL != "" || movie.Poster.PosterURL != "" || len(movie.Screenshots) > 0 || movie.TrailerURL != "" {
		fmt.Fprintln(w, strings.Repeat("-", maxLabelWidth+2)+" "+strings.Repeat("-", 100))
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Media URLs:")
		fmt.Fprintln(w)

		if movie.Poster.CoverURL != "" {
			fmt.Fprintf(w, "  Cover URL    : %s\n", movie.Poster.CoverURL)
		}
		if movie.Poster.PosterURL != "" && movie.Poster.PosterURL != movie.Poster.CoverURL {
			fmt.Fprintf(w, "  Poster URL   : %s\n", movie.Poster.PosterURL)
		}
		if movie.TrailerURL != "" {
			fmt.Fprintf(w, "  Trailer URL  : %s\n", movie.TrailerURL)
		}
		if len(movie.Screenshots) > 0 {
			fmt.Fprintf(w, "  Screenshots  : %d total\n", len(movie.Screenshots))
			for i, url := range movie.Screenshots {
				fmt.Fprintf(w, "    [%2d] %s\n", i+1, url)
			}
		}
	}

	// Description section (full text, properly wrapped)
	if movie.Description != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, strings.Repeat("-", maxLabelWidth+2)+" "+strings.Repeat("-", 100))
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Description:")
		fmt.Fprintln(w)

		// Wrap description to terminal width with some padding
		descLines := wrapText(movie.Description, terminalWidth-4)
		for _, line := range descLines {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}

	// Print table footer
	fmt.Fprintln(w)
	fmt.Fprintln(w, strings.Repeat("-", maxLabelWidth+2)+" "+strings.Repeat("-", 100))
	fmt.Fprintln(w)
}

// wrapText wraps text to the specified width, breaking at word boundaries.
// Returns a slice of lines. If width <= 0, defaults to 80.
func wrapText(text string, width int) []string {
	if width <= 0 {
		width = 80
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}

	var lines []string
	currentLine := ""

	for _, word := range words {
		if currentLine == "" {
			currentLine = word
		} else if len(currentLine)+1+len(word) <= width {
			currentLine += " " + word
		} else {
			lines = append(lines, currentLine)
			currentLine = word
		}
	}
	if currentLine != "" {
		lines = append(lines, currentLine)
	}
	return lines
}
