//go:build e2e

package cli_e2e

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCLI_SortDuplicateSkip_PersistsHistoryAndLogs pins issue #244 end-to-end
// through the real binary: an authorized intra-batch duplicate skip from
// `javinizer sort -f` must persist the same audit trail an API batch writes —
// a queryable batch job (history list --batch) carrying the winner's applied
// row AND the loser's completed-noop row, per-movie history whose organize
// metadata keeps the skip warning text, and an eventlog warning entry —
// instead of the console claiming "Applied ✓" with nothing persisted.
//
// Two files scrape to GOOD-700 and plan onto the SAME destination because
// file_format is bare <ID>; the -f flag authorizes overwrite, so the second
// claimant is demoted to a warning + skip. All assertions are substring-based
// (never raw produced paths) so the test stays Windows-safe.
func TestCLI_SortDuplicateSkip_PersistsHistoryAndLogs(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeConfig(t, dir)
	src := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(filepath.Join(src, "a"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(src, "b"), 0o700))
	writeFile(t, filepath.Join(src, "a", "GOOD-700.mp4"))
	writeFile(t, filepath.Join(src, "b", "GOOD-700.mp4"))

	out, code := run(t, cfgPath, "sort", "-f", "--move", src)
	require.Equal(t, 0, code, "sort exited %d\n%s", code, out)
	assert.Contains(t, out, "Sort complete!")

	// Console truth: the skip is surfaced per file and counted in the summary.
	assert.Contains(t, out, "duplicate destination within batch",
		"the skip warning must print\n%s", out)
	assert.Contains(t, out, "Skipped (authorized duplicates): 1",
		"the summary must count the skip\n%s", out)

	m := regexp.MustCompile(`Batch Job: ([0-9a-f-]{36})`).FindStringSubmatch(out)
	require.Len(t, m, 2, "the header must name the persisted batch identity\n%s", out)
	batchID := m[1]

	// Exactly one file may land: the loser's bytes stay in place.
	assert.FileExists(t, filepath.Join(src, "GOOD-700", "GOOD-700.mp4"),
		"the batch winner is organized\n%s", out)

	// (i) `javinizer history list --batch` lists both batch ops: the winner
	// applied, the loser completed-noop (previously NOTHING persisted for the
	// CLI flow — the noop row landed under "" and was invisible).
	histOut, histCode := run(t, cfgPath, "history", "list", "--batch", batchID)
	require.Equal(t, 0, histCode, "history --batch exited %d\n%s", histCode, histOut)
	assert.Contains(t, histOut, "noop",
		"the authorized duplicate skip must be listed as a noop op\n%s", histOut)
	assert.Contains(t, histOut, "applied",
		"the batch winner's move op must be listed as applied\n%s", histOut)

	// (i) `javinizer history movie` keeps the skip warning text in the
	// organize row's metadata (the text that used to vanish).
	movieOut, movieCode := run(t, cfgPath, "history", "movie", "GOOD-700")
	require.Equal(t, 0, movieCode, "history movie exited %d\n%s", movieCode, movieOut)
	assert.Contains(t, movieOut, "duplicate destination within batch",
		"the organize history metadata must carry the warning text\n%s", movieOut)

	// (ii) `javinizer logs` exposes the eventlog audit entry for the skip.
	logsOut, logsCode := run(t, cfgPath, "logs", "list", "-t", "organize", "-s", "warn")
	require.Equal(t, 0, logsCode, "logs exited %d\n%s", logsCode, logsOut)
	assert.Contains(t, logsOut, "Organize warning for GOOD-700",
		"the eventlog must carry the organize warning audit entry\n%s", logsOut)
	// Logs output truncates messages at 60 chars; the full text lives in the
	// events table and the history metadata (asserted above).
	assert.Contains(t, logsOut, "duplicate destination with", "\n%s", logsOut)
}
