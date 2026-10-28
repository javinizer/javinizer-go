package formatter_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/formatter"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestWriteMovie_MultilingualMetadataContract(t *testing.T) {
	for _, tt := range []struct {
		name      string
		title     string
		wantWraps bool
	}{
		{
			name:  "short title stays on one row",
			title: "A quiet morning by the river",
		},
		{
			name: "long title preserves every word across continuation rows",
			title: "A quiet morning by the river where the city wakes slowly and neighbors walk " +
				"past the market with fresh flowers while sunlight reaches the old bridge " +
				"and the first boats begin their journey toward the sea",
			wantWraps: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			movie := &models.Movie{
				ID:    "FIX-001",
				Title: tt.title,
				Translations: []models.MovieTranslation{
					{MovieID: "FIX-001", Language: "en", SourceName: "r18dev", Title: tt.title},
					{MovieID: "FIX-001", Language: "fr", SourceName: "translation:deepl", Title: "Un matin calme au bord du fleuve"},
				},
			}
			before := *movie
			before.Translations = append([]models.MovieTranslation(nil), movie.Translations...)

			var buf bytes.Buffer
			formatter.WriteMovie(&buf, movie, nil)
			output := buf.String()
			require.Equal(t, before, *movie, "rendering must not mutate metadata or translations")

			// Translations is the longest label. Every value, including a wrapped
			// title fragment, must start at the same column after its 12-character label.
			const labelWidth = 12
			continuationPrefix := strings.Repeat(" ", labelWidth+3)
			expectedRows := map[string]string{
				"ID":           "FIX-001",
				"Title":        strings.Join(strings.Fields(tt.title), " "),
				"Translations": "English (r18dev), fr (translation:deepl)",
				"Sources":      "r18dev, translation:deepl",
			}
			seen := make(map[string]bool)
			var titleFragments []string
			previousLabel := ""
			continuations := 0
			for _, line := range strings.Split(output, "\n") {
				require.LessOrEqual(t, len(line), 120, "ASCII output must fit the terminal: %q", line)
				if line == "" || strings.Trim(line, "- ") == "" {
					continue
				}
				if strings.HasPrefix(line, continuationPrefix) {
					require.Equal(t, "Title", previousLabel, "only the long title needs continuation rows")
					fragment := strings.TrimPrefix(line, continuationPrefix)
					require.NotEmpty(t, fragment)
					require.Equal(t, strings.TrimSpace(fragment), fragment, "continuation must align with the first value")
					require.NotContains(t, fragment, ":", "continuation must not repeat a label or separator")
					titleFragments = append(titleFragments, fragment)
					continuations++
					continue
				}

				labelColumn, value, ok := strings.Cut(line, " : ")
				require.True(t, ok, "expected a labeled metadata row: %q", line)
				label := strings.TrimSpace(labelColumn)
				want, exists := expectedRows[label]
				require.True(t, exists, "unexpected metadata row: %q", label)
				require.False(t, seen[label], "metadata labels must appear exactly once: %q", label)
				require.Equal(t, label+strings.Repeat(" ", labelWidth-len(label)), labelColumn)
				seen[label] = true
				previousLabel = label
				if label == "Title" {
					titleFragments = append(titleFragments, value)
				} else {
					require.Equal(t, want, value, "metadata values must retain language and source provenance")
				}
			}
			require.Len(t, seen, len(expectedRows), "every expected metadata row must be present")
			require.Equal(t, expectedRows["Title"], strings.Join(titleFragments, " "), "title words must not be lost, repeated, or reordered")
			if tt.wantWraps {
				require.Positive(t, continuations, "long title must exercise continuation rendering")
			} else {
				require.Zero(t, continuations, "short title must not gain a continuation")
			}
			for _, absent := range []string{"Source URLs:", "Media URLs:", "Description:"} {
				require.NotContains(t, output, absent, "absent data must not create extra sections")
			}

			var repeated bytes.Buffer
			formatter.WriteMovie(&repeated, movie, nil)
			require.Equal(t, output, repeated.String(), "rendering the same metadata must be repeatable")
			require.Equal(t, before, *movie, "repeated rendering must not mutate its input")
		})
	}
}
