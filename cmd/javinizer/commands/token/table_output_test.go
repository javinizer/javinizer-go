package token

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWriteTokenListTableBytes(t *testing.T) {
	t.Parallel()
	created := time.Date(2026, time.January, 2, 3, 4, 0, 0, time.UTC)
	used := created.Add(time.Hour)
	entries := []tokenListEntry{
		{ID: "aaaaaaaa-extra", Name: strings.Repeat("x", 31), TokenPrefix: "alpha", CreatedAt: created, LastUsedAt: &used},
		{ID: "bbb", TokenPrefix: "beta", CreatedAt: created},
	}
	var output bytes.Buffer
	require.NoError(t, writeTokenListTable(&output, entries))
	require.Equal(t,
		"ID        PREFIX    NAME                            CREATED           LAST USED\n"+
			"aaaaaaaa  jv_alpha  xxxxxxxxxxxxxxxxxxxxxxxxxxx...  2026-01-02 03:04  2026-01-02 04:04\n"+
			"bbb       jv_beta   (unnamed)                       2026-01-02 03:04  never\n",
		output.String())
}

func TestWriteTokenListTableClosedOutput(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		label string
		name  string
	}{
		{label: "final flush", name: "plain"},
		// A blank line inside the name makes tabwriter flush during the row Write.
		{label: "row write", name: "first\n\nsecond"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			out, err := os.Create(filepath.Join(t.TempDir(), "redirected-output"))
			require.NoError(t, err)
			require.NoError(t, out.Close())
			require.ErrorIs(t, writeTokenListTable(out, []tokenListEntry{{ID: "display-id", Name: tc.name}}), os.ErrClosed)
		})
	}
}
