package dump

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/require"
)

func TestRunSearchMarkerIdentityBoundary(t *testing.T) {
	for _, tc := range []struct{ query, cid, dvd, want string }{
		{"T-28123H", "9t28123h", "T28-123-HD", "No match"},
		{"T-28123-HD", "55t28123h", "T28-123-HD", "No match"},
		{"T28-123H", "t28123h", "T-28123-HD", "No match"},
		{"T28-123-HD", "9t2800123h", "T-28123-HD", "No match"},
		{"T28\u00a0123\u00a0HD", "9t28123h", "T28-123-HD", "content_id: 9t28123h"},
		{"T\t28123\tHD", "55t28123h", "T-28123-HD", "content_id: 55t28123h"},
		{"T-28123-Z-HD", "55t28123zh", `\N`, "has no dvd_id"},
		{"55t28123zh", "55t28123zh", `\N`, "has no dvd_id"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			runInDir(t, t.TempDir(), func() {
				dbPath := filepath.Join("data", "r18dev", "r18dev_dump.db")
				input := "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n" + tc.cid + "\t" + tc.dvd + "\n\\.\n"
				_, err := r18devdump.Import(context.Background(), strings.NewReader(input), dbPath, r18devdump.ImportOptions{})
				require.NoError(t, err)
				var output bytes.Buffer
				require.NoError(t, runSearch(&output, "config.yaml", tc.query))
				require.Contains(t, output.String(), tc.want)
				if tc.dvd == `\N` {
					require.NotContains(t, output.String(), " -> ")
					require.Contains(t, output.String(), tc.cid)
				}
			})
		})
	}
}
