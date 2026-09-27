package r18devdump

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImport_RejectsNoRecognizedRows(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"empty input", ""},
		{"garbage text", "<html><body>503 Service Unavailable</body></html>\n"},
		{"copy header zero rows", "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n\\.\n"},
		{"unknown table with rows", "COPY public.not_a_dump_table (a) FROM stdin;\n1\n2\n\\.\n"},
		// Codex round on #273: rows only for a non-video table are desynced
		// garbage, not a dump — reject them like other tableless inputs.
		{"non-video table rows only", "COPY public.derived_actress (id, name_romaji) FROM stdin;\n1\tJane\n\\.\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.db")
			_, err := Import(context.Background(), strings.NewReader(tc.in), path, ImportOptions{})
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrDumpNoRows), "want ErrDumpNoRows, got %v", err)
			_, statErr := os.Stat(path)
			assert.True(t, os.IsNotExist(statErr), "no sidecar file may be installed on invariant failure")
		})
	}
}
