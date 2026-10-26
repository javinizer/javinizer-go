package batch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBatchBrowseSourcePath(t *testing.T) {
	source := t.TempDir()
	file := filepath.Join(source, "SYNTHETIC-001.mp4")
	other := t.TempDir()
	regular := filepath.Join(source, "not-a-directory")
	require.NoError(t, os.WriteFile(regular, []byte("fixture"), 0600))
	root := filepath.VolumeName(source) + string(filepath.Separator)
	type sourceCase struct {
		name  string
		files []string
		total int
		want  string
	}
	cases := []sourceCase{
		{"one original file", []string{file}, 1, source},
		{"same parent multipart", []string{file, filepath.Join(source, "SYNTHETIC-001-pt2.mp4")}, 2, source},
		{"legacy files absent", nil, 1, ""},
		{"empty job", nil, 0, ""},
		{"incomplete files", []string{file}, 2, ""},
		{"invalid total", []string{file}, -1, ""},
		{"different parents", []string{file, filepath.Join(other, "SYNTHETIC-002.mp4")}, 2, ""},
		{"nested sources not common ancestor", []string{file, filepath.Join(source, "nested", "SYNTHETIC-002.mp4")}, 2, ""},
		{"empty path", []string{""}, 1, ""},
		{"relative source", []string{"relative/video.mp4"}, 1, ""},
		{"unclean source", []string{source + string(filepath.Separator) + ".." + string(filepath.Separator) + "video.mp4"}, 1, ""},
		{"invalid filename", []string{filepath.Join(source, "bad\x00.mp4")}, 1, ""},
		{"missing parent is only a candidate", []string{filepath.Join(source, "missing", "video.mp4")}, 1, filepath.Join(source, "missing")},
		{"non-directory parent is only a candidate", []string{filepath.Join(regular, "video.mp4")}, 1, regular},
		{"filesystem root", []string{filepath.Join(root, "video.mp4")}, 1, ""},
	}
	if runtime.GOOS != "windows" {
		// Backslash is a legal POSIX filename character, not a separator. Do not
		// reinterpret native paths using browser-side Windows/POSIX heuristics.
		cases = append(cases,
			sourceCase{"native backslash filename", []string{filepath.Join(source, `part\one.mp4`)}, 1, source},
			sourceCase{"foreign drive path", []string{`C:\videos\movie.mp4`}, 1, ""},
			sourceCase{"foreign UNC path", []string{`\\server\share\movie.mp4`}, 1, ""},
		)
	}
	if runtime.GOOS == "windows" {
		cases = append(cases,
			sourceCase{"native drive directory", []string{`C:\videos\movie.mp4`}, 1, `C:\videos`},
			sourceCase{"different drives", []string{`C:\videos\one.mp4`, `D:\videos\two.mp4`}, 2, ""},
			sourceCase{"native UNC directory", []string{`\\server\share\videos\movie.mp4`}, 1, `\\server\share\videos`},
			sourceCase{"different UNC shares", []string{`\\server\one\videos\a.mp4`, `\\server\two\videos\b.mp4`}, 2, ""},
			sourceCase{"drive root", []string{`C:\movie.mp4`}, 1, ""},
			sourceCase{"UNC share root", []string{`\\server\share\movie.mp4`}, 1, ""},
		)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, batchBrowseSourcePath(tc.files, tc.total))
		})
	}
}

func TestBatchResponseBrowseSourceIsOnlyAnOriginalFilesHint(t *testing.T) {
	// Intentionally absent: deriving a hint must not perform filesystem I/O.
	source := filepath.Join(t.TempDir(), "missing-source")
	file := filepath.Join(source, "SYNTHETIC-001.mp4")
	payload, err := json.Marshal(map[string]any{
		"id": "synthetic-return-job", "total_files": 1, "files": []string{file},
		"destination": t.TempDir(), "results": map[string]any{},
	})
	require.NoError(t, err)
	job := makeBatchJobStatus(t, string(payload))
	require.Equal(t, source, buildBatchJobResponse(job).BrowseSourcePath)
	require.Equal(t, source, buildBatchJobSlimResponse(job).BrowseSourcePath)
	for _, response := range []any{buildBatchJobResponse(job), buildBatchJobSlimResponse(job)} {
		encoded, err := json.Marshal(response)
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(encoded, &fields))
		require.Equal(t, source, fields["browse_source_path"])
	}
	// A legacy response with no complete original source list must not use its
	// destination or results to guess a return path.
	job.Files = nil
	for _, response := range []any{buildBatchJobResponse(job), buildBatchJobSlimResponse(job)} {
		encoded, err := json.Marshal(response)
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(encoded, &fields))
		require.NotContains(t, fields, "browse_source_path")
	}
}
