package workflow

import (
	"fmt"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

// diffSpec defines a field comparison rule for identifyDifferences.
// Each spec provides a field name, value getters for each movie variant,
// and a diff predicate that returns true when NFO and scraped values differ.
type diffSpec struct {
	field      string
	getNFO     func(m *models.Movie) any
	getScraped func(m *models.Movie) any
	getMerged  func(m *models.Movie) any
	diff       func(nfo, scraped *models.Movie) bool
}

// diffSpecs is the table of field comparison rules.
// identifyDifferences loops over this table instead of repeating
// nearly-identical field comparison blocks.
var diffSpecs = []diffSpec{
	// Simple string fields with != comparison
	{field: "title", getNFO: func(m *models.Movie) any { return m.Title }, getScraped: func(m *models.Movie) any { return m.Title }, getMerged: func(m *models.Movie) any { return m.Title }, diff: func(n, s *models.Movie) bool { return n.Title != s.Title }},
	{field: "description", getNFO: func(m *models.Movie) any { return m.Description }, getScraped: func(m *models.Movie) any { return m.Description }, getMerged: func(m *models.Movie) any { return m.Description }, diff: func(n, s *models.Movie) bool { return n.Description != s.Description }},
	{field: "director", getNFO: func(m *models.Movie) any { return m.Director }, getScraped: func(m *models.Movie) any { return m.Director }, getMerged: func(m *models.Movie) any { return m.Director }, diff: func(n, s *models.Movie) bool { return n.Director != s.Director }},
	{field: "maker", getNFO: func(m *models.Movie) any { return m.Maker }, getScraped: func(m *models.Movie) any { return m.Maker }, getMerged: func(m *models.Movie) any { return m.Maker }, diff: func(n, s *models.Movie) bool { return n.Maker != s.Maker }},
	// Label (sub-label/studio label)
	{field: "label", getNFO: func(m *models.Movie) any { return m.Label }, getScraped: func(m *models.Movie) any { return m.Label }, getMerged: func(m *models.Movie) any { return m.Label }, diff: func(n, s *models.Movie) bool { return n.Label != s.Label }},
	// Series name
	{field: "series", getNFO: func(m *models.Movie) any { return m.Series }, getScraped: func(m *models.Movie) any { return m.Series }, getMerged: func(m *models.Movie) any { return m.Series }, diff: func(n, s *models.Movie) bool { return n.Series != s.Series }},
	// ContentID — show diff when NFO and scraped disagree, with non-empty guard
	{field: "content_id", getNFO: func(m *models.Movie) any { return m.ContentID }, getScraped: func(m *models.Movie) any { return m.ContentID }, getMerged: func(m *models.Movie) any { return m.ContentID }, diff: func(n, s *models.Movie) bool {
		return n.ContentID != s.ContentID && (n.ContentID != "" || s.ContentID != "")
	}},
	// Runtime (int field with != comparison)
	{field: "runtime", getNFO: func(m *models.Movie) any { return m.Runtime }, getScraped: func(m *models.Movie) any { return m.Runtime }, getMerged: func(m *models.Movie) any { return m.Runtime }, diff: func(n, s *models.Movie) bool { return n.Runtime != s.Runtime }},
	// Rating — compare score when either side has a non-zero value
	{field: "rating", getNFO: func(m *models.Movie) any { return m.RatingScore }, getScraped: func(m *models.Movie) any { return m.RatingScore }, getMerged: func(m *models.Movie) any { return m.RatingScore }, diff: func(n, s *models.Movie) bool {
		return n.RatingScore != s.RatingScore && (n.RatingScore != 0 || s.RatingScore != 0)
	}},
	// Release date — compare the date-only FORMATTED values so that
	// timezone/time-of-day differences do not trigger false mismatches. The NFO
	// renders only YYYY-MM-DD (formatTimePtr), so the diff must compare the same
	// date-only representation rather than full time.Time equality.
	{field: "release_date", getNFO: func(m *models.Movie) any { return formatTimePtr(m.ReleaseDate) }, getScraped: func(m *models.Movie) any { return formatTimePtr(m.ReleaseDate) }, getMerged: func(m *models.Movie) any { return formatTimePtr(m.ReleaseDate) }, diff: func(n, s *models.Movie) bool {
		return formatTimePtr(n.ReleaseDate) != formatTimePtr(s.ReleaseDate)
	}},
	// Media URLs — with non-empty guard
	{field: "cover_url", getNFO: func(m *models.Movie) any { return m.Poster.CoverURL }, getScraped: func(m *models.Movie) any { return m.Poster.CoverURL }, getMerged: func(m *models.Movie) any { return m.Poster.CoverURL }, diff: func(n, s *models.Movie) bool {
		return n.Poster.CoverURL != s.Poster.CoverURL && (n.Poster.CoverURL != "" || s.Poster.CoverURL != "")
	}},
	{field: "poster_url", getNFO: func(m *models.Movie) any { return m.Poster.PosterURL }, getScraped: func(m *models.Movie) any { return m.Poster.PosterURL }, getMerged: func(m *models.Movie) any { return m.Poster.PosterURL }, diff: func(n, s *models.Movie) bool {
		return n.Poster.PosterURL != s.Poster.PosterURL && (n.Poster.PosterURL != "" || s.Poster.PosterURL != "")
	}},
	{field: "trailer_url", getNFO: func(m *models.Movie) any { return m.TrailerURL }, getScraped: func(m *models.Movie) any { return m.TrailerURL }, getMerged: func(m *models.Movie) any { return m.TrailerURL }, diff: func(n, s *models.Movie) bool {
		return n.TrailerURL != s.TrailerURL && (n.TrailerURL != "" || s.TrailerURL != "")
	}},
	// Complex slice comparisons with custom formatting
	{field: "actresses", getNFO: func(m *models.Movie) any { return formatActressList(m.Actresses) }, getScraped: func(m *models.Movie) any { return formatActressList(m.Actresses) }, getMerged: func(m *models.Movie) any { return formatActressList(m.Actresses) }, diff: func(n, s *models.Movie) bool {
		return !actressSlicesEqual(n.Actresses, s.Actresses)
	}},
	{field: "genres", getNFO: func(m *models.Movie) any { return formatGenreList(m.Genres) }, getScraped: func(m *models.Movie) any { return formatGenreList(m.Genres) }, getMerged: func(m *models.Movie) any { return formatGenreList(m.Genres) }, diff: func(n, s *models.Movie) bool {
		return !genreSlicesEqual(n.Genres, s.Genres)
	}},
}

// identifyDifferences compares NFO, scraped, and merged data to identify key differences.
// Domain logic: this function lives behind the Compare seam so that the API layer
// does not duplicate field-comparison logic.
func identifyDifferences(nfoMovie, scrapedMovie, mergedMovie *models.Movie) []FieldDifference {
	diffs := make([]FieldDifference, 0, len(diffSpecs))
	for _, spec := range diffSpecs {
		if spec.diff(nfoMovie, scrapedMovie) {
			diffs = append(diffs, FieldDifference{
				Field:        spec.field,
				NFOValue:     spec.getNFO(nfoMovie),
				ScrapedValue: spec.getScraped(scrapedMovie),
				MergedValue:  spec.getMerged(mergedMovie),
			})
		}
	}
	return diffs
}

// actressKey computes a stable identity key for an actress.
// Uses a composite key strategy:
//   - Primary: DMMID (if > 0) — globally unique identifier
//   - Secondary: JapaneseName (if non-empty) — covers Japanese-named actresses
//   - Tertiary: FullName (LastName + FirstName) — covers Western-name-only actresses
//
// This prevents false matches when only one identifier type is available.
func actressKey(a models.Actress) string {
	if a.DMMID > 0 {
		return fmt.Sprintf("dmm:%d", a.DMMID)
	}
	if a.JapaneseName != "" {
		return fmt.Sprintf("ja:%s", a.JapaneseName)
	}
	return fmt.Sprintf("en:%s", a.FullName())
}

// actressSlicesEqual compares two actress slices by composite identity key, ignoring order.
// Uses count maps so duplicate entries are compared correctly as multisets.
func actressSlicesEqual(a, b []models.Actress) bool {
	if len(a) != len(b) {
		return false
	}
	aCounts := make(map[string]int, len(a))
	for _, act := range a {
		aCounts[actressKey(act)]++
	}
	for _, act := range b {
		k := actressKey(act)
		if aCounts[k] == 0 {
			return false
		}
		aCounts[k]--
	}
	return true
}

// genreSlicesEqual compares two genre slices by name, ignoring order.
// Uses count maps so duplicate entries are compared correctly as multisets.
func genreSlicesEqual(a, b []models.Genre) bool {
	if len(a) != len(b) {
		return false
	}
	aCounts := make(map[string]int, len(a))
	for _, g := range a {
		aCounts[g.Name]++
	}
	for _, g := range b {
		if aCounts[g.Name] == 0 {
			return false
		}
		aCounts[g.Name]--
	}
	return true
}

// formatTimePtr formats a *time.Time for display. Returns "<nil>" for nil.
func formatTimePtr(t *time.Time) string {
	if t == nil {
		return "<nil>"
	}
	return t.Format("2006-01-02")
}

// formatList formats a slice as "N <itemType>: name1, name2, ..." truncated to
// 3 names max for readability. The getName callback extracts the display name
// from each item.
func formatList[T any](items []T, getName func(T) string, itemType string) string {
	n := len(items)
	if n == 0 {
		return fmt.Sprintf("0 %s", itemType)
	}
	const maxShow = 3
	names := make([]string, 0, min(n, maxShow))
	for i, item := range items {
		if i >= maxShow {
			break
		}
		names = append(names, getName(item))
	}
	listStr := strings.Join(names, ", ")
	if n > maxShow {
		return fmt.Sprintf("%d %s: %s, ...", n, itemType, listStr)
	}
	return fmt.Sprintf("%d %s: %s", n, itemType, listStr)
}

// formatActressList formats an actress list as "N actresses: name1, name2, ..."
// truncated to 3 names max for readability.
func formatActressList(actresses []models.Actress) string {
	return formatList(actresses, func(a models.Actress) string { return a.FullName() }, "actresses")
}

// formatGenreList formats a genre list as "N genres: name1, name2, ..."
// truncated to 3 names max for readability.
func formatGenreList(genres []models.Genre) string {
	return formatList(genres, func(g models.Genre) string { return g.Name }, "genres")
}
