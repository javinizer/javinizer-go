package organizer

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubtitleSourceSelectionPolicy(t *testing.T) {
	for _, tc := range []struct {
		name       string
		enabled    bool
		extensions []string
		want       []string
	}{
		{name: "disabled", extensions: []string{".srt"}},
		{name: "empty extensions", enabled: true},
		{name: "excluded extension", enabled: true, extensions: []string{".ass"}},
		{name: "case insensitive and language suffix", enabled: true, extensions: []string{".SRT"}, want: []string{"ABC-123.srt", "abc-123.en.SRT"}},
		{name: "custom extension", enabled: true, extensions: []string{".ttml"}, want: []string{"ABC-123.ttml"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			sourceDir := filepath.Join(t.TempDir(), "source")
			require.NoError(t, fs.MkdirAll(sourceDir, 0o755))
			for _, name := range []string{"ABC-123.mp4", "ABC-123.srt", "abc-123.en.SRT", "ABC-123.ttml", "ABC-1234.srt", "other.srt"} {
				require.NoError(t, afero.WriteFile(fs, filepath.Join(sourceDir, name), []byte(name), 0o644))
			}
			require.NoError(t, fs.MkdirAll(filepath.Join(sourceDir, "ABC-123.dir.srt"), 0o755))
			org := NewOrganizer(fs, &Config{MoveSubtitles: tc.enabled, SubtitleExtensions: tc.extensions}, nil, nil)
			match := models.FileMatchInfo{Path: filepath.Join(sourceDir, "ABC-123.mp4"), Name: "ABC-123.mp4", Extension: ".mp4"}
			want := make([]string, 0, len(tc.want))
			for _, name := range tc.want {
				want = append(want, filepath.Join(sourceDir, name))
			}
			assert.ElementsMatch(t, want, org.SubtitleSourcePaths(match))
			plan := &OrganizePlan{Match: match, SourcePath: match.Path, TargetDir: filepath.Join(sourceDir, "destination"), TargetFile: "movie.mp4"}
			moves := org.PlanSubtitleMoves(plan)
			got := make([]string, 0, len(moves))
			for _, move := range moves {
				got = append(got, move.OriginalPath)
			}
			assert.ElementsMatch(t, want, got, "admission and publication use the same configured selection")
			result, err := org.execute(plan)
			require.NoError(t, err)
			assert.Len(t, result.Subtitles, len(want), "no-op planning also respects the subtitle policy")
			for _, path := range want {
				body, err := afero.ReadFile(fs, path)
				require.NoError(t, err)
				assert.Equal(t, filepath.Base(path), string(body), "selection must not change source files")
			}
		})
	}
}
