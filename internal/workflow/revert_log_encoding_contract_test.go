package workflow

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestBuildGeneratedFilesJSONEncodingContract(t *testing.T) {
	for _, tc := range []struct {
		name      string
		nfo       string
		downloads []string
		subtitles []models.SubtitleMove
		resident  []bool
		want      string
	}{
		{name: "empty"},
		{name: "explicit_nonresident", resident: []bool{false}},
		{name: "resident_without_artifacts", resident: []bool{true}, want: `{"primary_resident":true}`},
		{name: "first_resident_flag_wins", resident: []bool{false, true}},
		{name: "delete_order_and_duplicates", nfo: "movie.nfo", downloads: []string{"poster.jpg", "movie.nfo", ""}, want: `{"delete":["movie.nfo","poster.jpg","movie.nfo",""]}`},
		{
			name: "subtitle_inverse_ownership", resident: []bool{true}, nfo: "movie.nfo", downloads: []string{"poster.jpg"},
			subtitles: []models.SubtitleMove{
				{OriginalPath: "copy-source", NewPath: "copy-target", Copied: true},
				{OriginalPath: "move-source", NewPath: "move-target", Moved: true},
				{OriginalPath: "both-source", NewPath: "both-target", Copied: true, Moved: true},
				{OriginalPath: "untouched-source", NewPath: "untouched-target"},
				{OriginalPath: "missing-target", Moved: true},
				{NewPath: "missing-source", Moved: true},
				{OriginalPath: "missing-copy-target", Copied: true},
			},
			want: `{"primary_resident":true,"delete":["movie.nfo","poster.jpg","copy-target","both-target"],"move_back":[{"original_path":"move-source","new_path":"move-target"}]}`,
		},
		{
			name: "json_string_encoding", nfo: "quote\"\n\x00<>&\xff",
			subtitles: []models.SubtitleMove{{OriginalPath: "日本語\\source", NewPath: "line\t\xfe", Moved: true}},
			want:      `{"delete":["quote\"\n\u0000\u003c\u003e\u0026�"],"move_back":[{"original_path":"日本語\\source","new_path":"line\t�"}]}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := buildGeneratedFilesJSON(nil, tc.nfo, tc.subtitles, tc.downloads, tc.resident...)
			require.Equal(t, tc.want, got)
			_, err := models.ParseGeneratedFiles(got)
			require.NoError(t, err)
		})
	}
}
