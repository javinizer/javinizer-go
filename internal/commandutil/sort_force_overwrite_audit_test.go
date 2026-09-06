package commandutil

// Force-overwrite audit crumb through the CLI batch seam: `sort -f` moving a
// file onto a pre-seeded (bytes-bearing) destination must surface the
// "overwrite authorized: replaced existing destination <path>" warning on
// EVERY audit surface the authenticated API flow gets — the console per-file
// warning (same rendering as the dup warnings), one eventlog organize/warn
// entry, the organize history row's warnings metadata, and the revert ledger
// keeps naming the real destination. The negative leg pins the authorized
// intra-batch duplicate SKIP against double-warns: pre-seeding the shared
// destination gives the loser's plan BOTH warning signals (occupation + dup),
// yet the skipped loser must carry ONLY the dup demotion.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forceCrumbPrefix is the audit message stem the organizer composes
// ("overwrite authorized: replaced existing destination <path>").
const forceCrumbPrefix = "overwrite authorized: replaced existing destination"

// warningLines collects the per-file warning lines the CLI printed.
func warningLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "⚠️") {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestRunBatchCommand_ForceOverwriteOccupiedDest_PersistsAuditCrumb is the
// positive pin: sort -f onto a destination already holding resident bytes.
func TestRunBatchCommand_ForceOverwriteOccupiedDest_PersistsAuditCrumb(t *testing.T) {
	configPath, src, dest, dbPath := setupSingleFileBatch(t, "GOOD-703")

	// Pre-seed the computed destination with resident bytes; -f authorizes
	// replacing them, and the replacement must be auditable.
	targetPath := filepath.Join(dest, "GOOD-703", "GOOD-703.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(targetPath), 0o700))
	require.NoError(t, os.WriteFile(targetPath, []byte("resident bytes"), 0o600))

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:        configPath,
		SourcePath:        src,
		Destination:       dest,
		Recursive:         true,
		MoveFiles:         true,
		ForceUpdate:       true, // -f authorizes the overwrite
		GenerateNFO:       true,
		CommandLabel:      "Javinizer Sort",
		ActionVerb:        "Processing files",
		CompletionMessage: "Sort complete!",
		Resolved:          &workflow.ResolvedSeamStrings{},
	})
	require.NoError(t, err)
	out := buf.String()
	batchID := batchIDFromOutput(t, out)

	// Console: the per-file warning prints exactly like the dup warnings.
	assert.Contains(t, out, "⚠️", "the overwrite warning must print to the console\n%s", out)
	assert.Contains(t, out, forceCrumbPrefix, "crumb text must survive to the console\n%s", out)
	assert.Contains(t, out, targetPath, "the crumb names the replaced destination\n%s", out)
	assert.NotContains(t, out, "duplicate destination within batch",
		"a single-file run has no intra-batch duplicate\n%s", out)

	// Journal evidence: the destination now carries the source's bytes; the
	// source moved; the revert ledger row names the real (replaced) target.
	content, readErr := os.ReadFile(targetPath)
	require.NoError(t, readErr)
	assert.Equal(t, "fake video", string(content), "resident bytes were replaced")
	_, statErr := os.Stat(filepath.Join(src, "GOOD-703.mp4"))
	assert.True(t, os.IsNotExist(statErr), "the source moved")

	ctx := context.Background()
	db := openAssertionDB(t, dbPath)
	repos := db.Repositories()

	ops, err := repos.BatchFileOpRepo.FindByBatchJobID(ctx, batchID)
	require.NoError(t, err)
	require.Len(t, ops, 1, "one journaled operation")
	assert.Equal(t, models.RevertStatusApplied, ops[0].RevertStatus)
	assert.Equal(t, targetPath, ops[0].NewPath, "the ledger row names the replaced destination")

	// History: the organize row's warnings metadata carries the crumb.
	rows, err := repos.HistoryRepo.FindByBatchJobID(ctx, batchID)
	require.NoError(t, err)
	crumbRows, dupRows := 0, 0
	for _, h := range rows {
		if h.Operation == models.HistoryOpOrganize && h.Status == models.HistoryStatusSuccess && h.Metadata != "" {
			if strings.Contains(h.Metadata, forceCrumbPrefix) {
				crumbRows++
			}
			if strings.Contains(h.Metadata, "duplicate destination within batch") {
				dupRows++
			}
		}
	}
	assert.Equal(t, 1, crumbRows, "the organize history row carries the crumb in its warnings metadata")
	assert.Zero(t, dupRows, "no batch-duplicate warning in a single-file run")

	// Eventlog: one organize/warn audit entry with the force message.
	events, err := repos.EventRepo.FindFiltered(ctx, database.EventFilter{
		EventType: models.EventCategoryOrganize,
		Severity:  models.SeverityWarn,
	}, 50, 0)
	require.NoError(t, err)
	require.Len(t, events, 1, "exactly one warn event — the crumb")
	assert.Contains(t, events[0].Message, forceCrumbPrefix)
	assert.Contains(t, events[0].Message, targetPath)
}

// TestRunBatchCommand_ForceDuplicateOntoOccupied_NoDoubleWarn is the
// negative pin: an AUTHORIZED intra-batch duplicate whose shared destination
// was ALSO pre-seeded gets both warning signals on its plan — the skipped
// loser must carry ONLY the dup demotion warning (the overwrite crumb belongs
// to the winner, whose bytes actually replaced the occupant).
func TestRunBatchCommand_ForceDuplicateOntoOccupied_NoDoubleWarn(t *testing.T) {
	configPath, src, dest, dbPath := setupDuplicateBatch(t)

	// Pre-seed the shared destination both files compute (<dest>/GOOD-700/
	// GOOD-700.mp4): the winner force-replaces it; the loser skips as an
	// authorized duplicate of the now-occupied destination.
	targetPath := filepath.Join(dest, "GOOD-700", "GOOD-700.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(targetPath), 0o700))
	require.NoError(t, os.WriteFile(targetPath, []byte("resident bytes"), 0o600))

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:        configPath,
		SourcePath:        src,
		Destination:       dest,
		Recursive:         true,
		MoveFiles:         true,
		ForceUpdate:       true,
		GenerateNFO:       true,
		CommandLabel:      "Javinizer Sort",
		ActionVerb:        "Processing files",
		CompletionMessage: "Sort complete!",
		Resolved:          &workflow.ResolvedSeamStrings{},
	})
	require.NoError(t, err)
	out := buf.String()
	batchID := batchIDFromOutput(t, out)

	// Console: exactly two per-file warnings — one crumb (winner), one dup
	// (loser); no line carries both, and the loser's line warns ONCE.
	warns := warningLines(out)
	require.Len(t, warns, 2, "winner crumb + loser dup warning, nothing doubled\n%s", out)
	var crumbLines, dupLines int
	for _, line := range warns {
		hasCrumb := strings.Contains(line, forceCrumbPrefix)
		hasDup := strings.Contains(line, "duplicate destination within batch")
		assert.False(t, hasCrumb && hasDup, "no single file carries both warnings: %s", line)
		if hasCrumb {
			crumbLines++
		}
		if hasDup {
			dupLines++
		}
	}
	assert.Equal(t, 1, crumbLines, "exactly one overwrite crumb (the winner)")
	assert.Equal(t, 1, dupLines, "exactly one dup warning (the skipped loser — no double-warn)")

	// The destination carries one of the batch files' bytes, never the
	// pre-seeded resident's.
	content, readErr := os.ReadFile(targetPath)
	require.NoError(t, readErr)
	assert.Contains(t, []string{"fake video a", "fake video b"}, string(content),
		"the winner replaced the resident bytes")

	ctx := context.Background()
	db := openAssertionDB(t, dbPath)
	repos := db.Repositories()

	// Eventlog: exactly two warn entries — the winner's crumb and the loser's
	// dup warning; the loser did NOT also emit a crumb.
	events, err := repos.EventRepo.FindFiltered(ctx, database.EventFilter{
		EventType: models.EventCategoryOrganize,
		Severity:  models.SeverityWarn,
	}, 50, 0)
	require.NoError(t, err)
	require.Len(t, events, 2, "one warn event per warning, none doubled")
	var crumbEvents, dupEvents int
	for _, ev := range events {
		if strings.Contains(ev.Message, forceCrumbPrefix) {
			crumbEvents++
		}
		if strings.Contains(ev.Message, "duplicate destination within batch") {
			dupEvents++
		}
	}
	assert.Equal(t, 1, crumbEvents)
	assert.Equal(t, 1, dupEvents)

	// History: two successful organize rows. The winner's warnings metadata
	// carries ONLY the crumb; the skipped loser's carries ONLY the dup
	// demotion — the double-signal plan did not double-warn.
	rows, err := repos.HistoryRepo.FindByBatchJobID(ctx, batchID)
	require.NoError(t, err)
	var organizeSuccesses int
	for _, h := range rows {
		if h.Operation != models.HistoryOpOrganize || h.Status != models.HistoryStatusSuccess {
			continue
		}
		organizeSuccesses++
		hasCrumb := strings.Contains(h.Metadata, forceCrumbPrefix)
		hasDup := strings.Contains(h.Metadata, "duplicate destination within batch")
		assert.False(t, hasCrumb && hasDup,
			"no history row may carry both warnings (double-warn): %s", h.Metadata)
		assert.True(t, hasCrumb || hasDup,
			"every organize success row carries exactly one warning: %s", h.Metadata)
	}
	assert.Equal(t, 2, organizeSuccesses, "winner + skipped loser rows")
}
