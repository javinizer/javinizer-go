package organizer

// Force-overwrite audit crumb: an overwrite-AUTHORIZED terminal leg that
// replaces a bytes-bearing destination must carry the
// "overwrite authorized: replaced existing destination <path>" warning on its
// OrganizeResult so the audit surfaces downstream (worker history metadata,
// API eventlog resolver, CLI console print) keep the replacement visible.
// The crumb keys on EXECUTE-TIME occupancy (the leg's own classification,
// taken under the held destination locks) — never plan-time evidence: a
// post-plan occupant swap can neither forge a replacement (occupied@plan →
// vacant@execute: silent) nor hide one (vacant@plan → occupied@execute:
// crumb). Wired lanes: organize move, organize copy, organize link install
// (hard/soft), in-place inner file rename, in-place non-rename file move,
// in-place-norenamefolder rename. Absent destinations, self/same-inode no-ops
// (including authorized link installs recycling a hardlink alias name),
// non-authorized runs, the pure in-place directory rename (replaces no
// foreign bytes by construction), and the authorized intra-batch duplicate
// skip keep their pre-existing behavior: no crumb, and the dup skip never
// double-warns.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// forceAuditOrganizer builds the organize-mode organizer over a fresh
// MemMapFs — the duplex fixture convention of dupBatchFixture: sources under
// /in, destinations compute as /dest/<ID>/<ID>.mkv.
func forceAuditOrganizer(t *testing.T) (*Organizer, afero.Fs) {
	t.Helper()
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)
	require.NoError(t, fs.MkdirAll("/in", 0o755))
	return org, fs
}

// forceAuditCmd builds the single-file organize command onto the /dest
// library; force maps to the CLI's -f overwrite authorization.
func forceAuditCmd(path string, force, move bool) OrganizeCmd {
	return OrganizeCmd{
		Match:       models.FileMatchInfo{MovieID: "ABC-123", Path: path, Name: filepath.Base(path), Extension: filepath.Ext(path)},
		Movie:       &models.Movie{ID: "ABC-123"},
		DestDir:     "/dest",
		MoveFiles:   move,
		LinkMode:    LinkModeNone,
		ForceUpdate: force,
	}
}

// plantOccupiedDest pre-seeds the computed destination with resident bytes —
// the foreign occupant a -f run must visibly replace.
func plantOccupiedDest(t *testing.T, fs afero.Fs) string {
	t.Helper()
	const dst = "/dest/ABC-123/ABC-123.mkv"
	require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("resident-bytes"), 0o644))
	return dst
}

func TestForceOverwriteAudit_MoveLeg(t *testing.T) {
	t.Run("force move onto occupied dest warns and replaces", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		dst := plantOccupiedDest(t, fs)

		result, err := org.Organize(context.Background(), forceAuditCmd("/in/A.mkv", true, true))
		require.NoError(t, err)
		require.True(t, result.Moved, "the authorized move executed")
		require.Len(t, result.Warnings, 1, "exactly one audit crumb — the resident bytes' replacement")
		assert.Equal(t, authorizedOverwriteWarning(dst), result.Warnings[0])
		assert.Contains(t, filepath.ToSlash(result.Warnings[0]),
			"overwrite authorized: replaced existing destination /dest/ABC-123/ABC-123.mkv")

		// Journal evidence: the destination now carries the winner's bytes and
		// the source path is gone (the move actually replaced the occupant).
		content, readErr := afero.ReadFile(fs, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), content, "resident bytes were replaced")
		srcExists, statErr := afero.Exists(fs, "/in/A.mkv")
		require.NoError(t, statErr)
		assert.False(t, srcExists, "the winner's source moved")
	})

	t.Run("force move onto vacant dest stays silent", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))

		result, err := org.Organize(context.Background(), forceAuditCmd("/in/A.mkv", true, true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		assert.Empty(t, result.Warnings, "no occupant existed at plan time — no crumb")
		content, readErr := afero.ReadFile(fs, "/dest/ABC-123/ABC-123.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), content)
	})

	t.Run("non-force onto occupied dest conflicts exactly as before", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		dst := plantOccupiedDest(t, fs)

		_, err := org.Organize(context.Background(), forceAuditCmd("/in/A.mkv", false, true))
		require.Error(t, err, "an occupied destination still blocks an unauthorized run")
		assert.Contains(t, err.Error(), "organization validation failed")
		assert.Contains(t, filepath.ToSlash(err.Error()), dst)

		// Neither side changed: the refusal replaced nothing.
		content, readErr := afero.ReadFile(fs, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("resident-bytes"), content, "the occupant's bytes are intact")
		srcContent, readErr := afero.ReadFile(fs, "/in/A.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), srcContent, "the source stayed put")
	})
}

func TestForceOverwriteAudit_CopyLeg(t *testing.T) {
	t.Run("force copy onto occupied dest warns and replaces", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		dst := plantOccupiedDest(t, fs)

		result, err := org.Organize(context.Background(), forceAuditCmd("/in/A.mkv", true, false))
		require.NoError(t, err)
		require.True(t, result.Moved, "the authorized copy executed")
		require.Len(t, result.Warnings, 1, "exactly one audit crumb")
		assert.Equal(t, authorizedOverwriteWarning(dst), result.Warnings[0])

		content, readErr := afero.ReadFile(fs, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), content, "resident bytes were replaced")
		srcExists, statErr := afero.Exists(fs, "/in/A.mkv")
		require.NoError(t, statErr)
		assert.True(t, srcExists, "a copy retains its source")
	})

	t.Run("force copy onto vacant dest stays silent", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))

		result, err := org.Organize(context.Background(), forceAuditCmd("/in/A.mkv", true, false))
		require.NoError(t, err)
		require.True(t, result.Moved)
		assert.Empty(t, result.Warnings, "no occupant existed at plan time — no crumb")
	})

	t.Run("non-force copy onto occupied dest conflicts exactly as before", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		dst := plantOccupiedDest(t, fs)

		_, err := org.Organize(context.Background(), forceAuditCmd("/in/A.mkv", false, false))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "organization validation failed")
		content, readErr := afero.ReadFile(fs, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("resident-bytes"), content, "the occupant's bytes are intact")
	})
}

// Same-inode alias no-op (requirement: NO crumb when nothing is replaced):
// source and destination are hardlink aliases of one inode — even under -f
// the plan-time classifier reports no occupation and execution no-ops.
func TestForceOverwriteAudit_SameInodeNoOp_Silent(t *testing.T) {
	dir := t.TempDir()
	fs := afero.NewOsFs()
	src := filepath.Join(dir, "in", "ABC-123.mkv")
	dst := filepath.Join(dir, "dest", "ABC-123", "ABC-123.mkv")
	require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	require.NoError(t, os.WriteFile(src, []byte("shared-bytes"), 0o644))
	require.NoError(t, os.Link(src, dst))

	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match:       models.FileMatchInfo{MovieID: "ABC-123", Path: src, Name: "ABC-123.mkv", Extension: ".mkv"},
		Movie:       &models.Movie{ID: "ABC-123"},
		DestDir:     filepath.Join(dir, "dest"),
		MoveFiles:   true,
		ForceUpdate: true,
	})
	require.NoError(t, err)
	assert.Empty(t, result.Warnings, "a same-inode no-op replaced nothing — no crumb")
	content, readErr := os.ReadFile(dst)
	require.NoError(t, readErr)
	assert.Equal(t, []byte("shared-bytes"), content)
	srcExists, statErr := afero.Exists(fs, src)
	require.NoError(t, statErr)
	assert.True(t, srcExists, "the no-op never removed the source")
}

// The authorized intra-batch duplicate skip carries ONLY its demotion
// warning — even when its plan ALSO saw a bytes-bearing occupant on the
// claimed destination, the skipped loser must never double-warn with the
// overwrite crumb (it executed nothing and replaced nothing).
func TestForceOverwriteAudit_DuplicateSkip_NoDoubleWarn(t *testing.T) {
	forceCasePosture(t, true)
	org, fs := forceAuditOrganizer(t)
	require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("a-bytes"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/in/B.mkv", []byte("b-bytes"), 0o644))
	dst := plantOccupiedDest(t, fs)
	tracker := NewDuplicateTracker(false)

	// Winner A force-organizes FIRST onto the occupied destination: the crumb
	// fires there (it really replaced the resident bytes).
	winner, err := org.Organize(context.Background(), OrganizeCmd{
		Match:            models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/A.mkv", Name: "A.mkv", Extension: ".mkv"},
		Movie:            &models.Movie{ID: "ABC-123"},
		DestDir:          "/dest",
		MoveFiles:        true,
		ForceUpdate:      true,
		DuplicateTracker: tracker,
	})
	require.NoError(t, err)
	require.Len(t, winner.Warnings, 1)
	assert.Equal(t, authorizedOverwriteWarning(dst), winner.Warnings[0],
		"the winner replaced the pre-seeded occupant")
	assert.True(t, winner.Moved)
	assert.False(t, winner.DuplicateSkipped)

	// Loser B: batch duplicate AND (at plan time) an occupied destination —
	// both warning signals exist, but the skip demotes to the dup warning
	// alone; the overwrite crumb belongs to whoever moved bytes (winner A).
	loser, err := org.Organize(context.Background(), OrganizeCmd{
		Match:            models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/B.mkv", Name: "B.mkv", Extension: ".mkv"},
		Movie:            &models.Movie{ID: "ABC-123"},
		DestDir:          "/dest",
		MoveFiles:        true,
		ForceUpdate:      true,
		DuplicateTracker: tracker,
	})
	require.NoError(t, err)
	assert.True(t, loser.DuplicateSkipped)
	require.Len(t, loser.Warnings, 1, "exactly one warning — the dup demotion, never doubled")
	assert.Contains(t, loser.Warnings[0], "duplicate destination within batch")
	assert.NotContains(t, loser.Warnings[0], "overwrite authorized: replaced existing destination",
		"the skipped loser replaced nothing — no overwrite crumb")

	// The destination carries exactly the winner's bytes.
	content, readErr := afero.ReadFile(fs, dst)
	require.NoError(t, readErr)
	assert.Equal(t, []byte("a-bytes"), content)
}

// ---------------------------------------------------------------------------
// P2 (review round 1): TOCTOU pins — the crumb must key on EXECUTE-TIME
// occupancy, never plan-time evidence. Two failure classes are pinned shut:
//   false positive — occupied@plan → vacant@execute: nothing is replaced, so
//     the crumb must NOT fire (plan-time evidence claimed a replacement that
//     never happened);
//   false negative — vacant@plan → occupied@execute: the execute leg replaces
//     resident bytes the plan never saw, so the crumb MUST fire.
// ---------------------------------------------------------------------------

// toctouWedgeFs wedges a filesystem mutation INTO the plan→execute window of
// one fused Organize call: the target's FIRST LstatIfPossible probe (the
// plan-time classification) answers truthfully; the wedge fires exactly once,
// just before the SECOND probe — the execute-time classification — answers.
// With plan-time crumb evidence (pre-fix) the wedge was invisible to the
// execute legs; the execute-keyed crumb must observe it.
type toctouWedgeFs struct {
	afero.Fs
	target string
	probes int
	wedge  func(fs afero.Fs)
}

func (w *toctouWedgeFs) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	if filepath.Clean(path) == filepath.Clean(w.target) {
		w.probes++
		if w.probes == 2 && w.wedge != nil {
			w.wedge(w.Fs)
			w.wedge = nil
		}
	}
	if lst, ok := w.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(path)
	}
	info, err := w.Fs.Stat(path)
	return info, false, err
}

// forceAuditPlan runs the EXACT plan the fused seam computes, then returns it
// for a manual execute through the plan's captured strategy — the
// "pre-create dest post-plan" TOCTOU wedge without racing goroutines.
func forceAuditPlan(t *testing.T, org *Organizer, cmd OrganizeCmd) *OrganizePlan {
	t.Helper()
	plan, err := org.PlanOrganize(context.Background(), cmd)
	require.NoError(t, err)
	require.Empty(t, plan.Conflicts, "force suppresses the file-occupation conflict at plan; other kinds must not appear in these fixtures")
	require.NotNil(t, plan.executeStrategy)
	return plan
}

// forceAuditPlanCopy repoints a planned move onto the copy leg exactly like
// the fused seam does (Organize sets moveFiles/LinkMode after planning).
func forceAuditPlanCopy(t *testing.T, org *Organizer, cmd OrganizeCmd) *OrganizePlan {
	t.Helper()
	plan := forceAuditPlan(t, org, cmd)
	plan.moveFiles = false
	plan.LinkMode = LinkModeNone
	return plan
}

func TestForceOverwriteAudit_TOCTOU_OccupiedOnlyAtExecute(t *testing.T) {
	t.Run("move leg: occupant planted post-plan crumbs", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		plan := forceAuditPlan(t, org, forceAuditCmd("/in/A.mkv", true, true))

		// TOCTOU: pre-create the destination POST-plan (vacant at plan time).
		dst := plantOccupiedDest(t, fs)

		result, err := plan.executeStrategy.Execute(plan)
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.Len(t, result.Warnings, 1,
			"the execute leg replaced resident bytes the plan never saw — the crumb must fire")
		assert.Equal(t, authorizedOverwriteWarning(dst), result.Warnings[0])

		content, readErr := afero.ReadFile(fs, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), content, "the late resident's bytes were really replaced")
	})

	t.Run("copy leg: occupant planted post-plan crumbs", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		plan := forceAuditPlanCopy(t, org, forceAuditCmd("/in/A.mkv", true, false))

		dst := plantOccupiedDest(t, fs) // occupied only at execute time

		result, err := plan.executeStrategy.Execute(plan)
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.Len(t, result.Warnings, 1)
		assert.Equal(t, authorizedOverwriteWarning(dst), result.Warnings[0])

		content, readErr := afero.ReadFile(fs, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), content)
		srcExists, statErr := afero.Exists(fs, "/in/A.mkv")
		require.NoError(t, statErr)
		assert.True(t, srcExists, "a copy retains its source")
	})
}

func TestForceOverwriteAudit_TOCTOU_VacatedAtExecute(t *testing.T) {
	t.Run("move leg: occupant removed post-plan stays silent", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		dst := plantOccupiedDest(t, fs)
		plan := forceAuditPlan(t, org, forceAuditCmd("/in/A.mkv", true, true))

		// TOCTOU: vacate the destination POST-plan (occupied at plan time).
		require.NoError(t, fs.Remove(dst))

		result, err := plan.executeStrategy.Execute(plan)
		require.NoError(t, err)
		require.True(t, result.Moved)
		assert.Empty(t, result.Warnings,
			"the execute leg replaced NOTHING (vacant at execute) — plan-time evidence would have claimed a replacement that never happened")

		content, readErr := afero.ReadFile(fs, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), content, "the move still landed — onto a vacant name")
	})

	t.Run("copy leg: occupant removed post-plan stays silent", func(t *testing.T) {
		org, fs := forceAuditOrganizer(t)
		require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		dst := plantOccupiedDest(t, fs)
		plan := forceAuditPlanCopy(t, org, forceAuditCmd("/in/A.mkv", true, false))

		require.NoError(t, fs.Remove(dst))

		result, err := plan.executeStrategy.Execute(plan)
		require.NoError(t, err)
		require.True(t, result.Moved)
		assert.Empty(t, result.Warnings)
		content, readErr := afero.ReadFile(fs, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), content)
	})
}

// Fused-seam TOCTOU pins: same two wedges, but driven through the single
// Organize call (plan and execute fused) with the injectable fs wrapper —
// proving the production call shape observes the post-plan mutation.
func TestForceOverwriteAudit_TOCTOU_FusedWedge(t *testing.T) {
	fusedCfg := func() *Config {
		return &Config{
			FolderFormat:  "<ID>",
			FileFormat:    "<ID>",
			RenameFile:    true,
			OperationMode: operationmode.OperationModeOrganize,
		}
	}
	const dst = "/dest/ABC-123/ABC-123.mkv"

	t.Run("move: occupant planted inside the plan-execute window crumbs", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/in", 0o755))
		require.NoError(t, afero.WriteFile(base, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		wedge := &toctouWedgeFs{Fs: base, target: dst, wedge: func(fs afero.Fs) {
			require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
			require.NoError(t, afero.WriteFile(fs, dst, []byte("late-resident"), 0o644))
		}}
		org := NewOrganizer(wedge, fusedCfg(), nil, nil)

		result, err := org.Organize(context.Background(), forceAuditCmd("/in/A.mkv", true, true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.Len(t, result.Warnings, 1, "the execute-time classification saw the wedged-in occupant")
		assert.Equal(t, authorizedOverwriteWarning(dst), result.Warnings[0])
		assert.GreaterOrEqual(t, wedge.probes, 2, "the wedge really fired between plan and execute")
		content, readErr := afero.ReadFile(base, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), content)
	})

	t.Run("copy: occupant planted inside the plan-execute window crumbs", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/in", 0o755))
		require.NoError(t, afero.WriteFile(base, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		wedge := &toctouWedgeFs{Fs: base, target: dst, wedge: func(fs afero.Fs) {
			require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
			require.NoError(t, afero.WriteFile(fs, dst, []byte("late-resident"), 0o644))
		}}
		org := NewOrganizer(wedge, fusedCfg(), nil, nil)

		result, err := org.Organize(context.Background(), forceAuditCmd("/in/A.mkv", true, false))
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.Len(t, result.Warnings, 1)
		assert.Equal(t, authorizedOverwriteWarning(dst), result.Warnings[0])
	})

	t.Run("move: occupant vacated inside the plan-execute window stays silent", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/in", 0o755))
		require.NoError(t, afero.WriteFile(base, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		require.NoError(t, base.MkdirAll(filepath.Dir(dst), 0o755))
		require.NoError(t, afero.WriteFile(base, dst, []byte("plan-time-resident"), 0o644))
		wedge := &toctouWedgeFs{Fs: base, target: dst, wedge: func(fs afero.Fs) {
			require.NoError(t, fs.Remove(dst))
		}}
		org := NewOrganizer(wedge, fusedCfg(), nil, nil)

		result, err := org.Organize(context.Background(), forceAuditCmd("/in/A.mkv", true, true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		assert.Empty(t, result.Warnings,
			"vacant at execute: nothing was replaced — plan-time evidence would have falsely crumbed")
		content, readErr := afero.ReadFile(base, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), content)
	})
}

// Batch-dup + occupied interplay under the execute-time keying: the crumb
// belongs to whichever execute leg ACTUALLY replaced bytes; the skipped loser
// never carries it regardless of what any plan observed.
func TestForceOverwriteAudit_BatchDup_TOCTOUInterplay(t *testing.T) {
	const dst = "/dest/ABC-123/ABC-123.mkv"
	batchOrg := func(base afero.Fs) *Organizer {
		require.NoError(t, base.MkdirAll("/in", 0o755))
		require.NoError(t, afero.WriteFile(base, "/in/A.mkv", []byte("a-bytes"), 0o644))
		require.NoError(t, afero.WriteFile(base, "/in/B.mkv", []byte("b-bytes"), 0o644))
		return NewOrganizer(base, &Config{
			FolderFormat:  "<ID>",
			FileFormat:    "<ID>",
			RenameFile:    true,
			OperationMode: operationmode.OperationModeOrganize,
		}, nil, nil)
	}
	batchCmd := func(src string, tracker *DuplicateTracker) OrganizeCmd {
		return OrganizeCmd{
			Match:            models.FileMatchInfo{MovieID: "ABC-123", Path: src, Name: filepath.Base(src), Extension: ".mkv"},
			Movie:            &models.Movie{ID: "ABC-123"},
			DestDir:          "/dest",
			MoveFiles:        true,
			ForceUpdate:      true,
			DuplicateTracker: tracker,
		}
	}

	t.Run("winner occupied only at execute: crumb lands on the winner, dup warning on the loser", func(t *testing.T) {
		forceCasePosture(t, true)
		base := afero.NewMemMapFs()
		wedge := &toctouWedgeFs{Fs: base, target: dst, wedge: func(fs afero.Fs) {
			// Both plans see a VACANT destination; the winner's execute-classify
			// probe plants the occupant mid-flight (false-negative wedge).
			require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
			require.NoError(t, afero.WriteFile(fs, dst, []byte("late-resident"), 0o644))
		}}
		org := batchOrg(wedge)
		tracker := NewDuplicateTracker(false)

		winner, err := org.Organize(context.Background(), batchCmd("/in/A.mkv", tracker))
		require.NoError(t, err)
		require.True(t, winner.Moved)
		require.Len(t, winner.Warnings, 1,
			"plan-time evidence (pre-fix) missed this replacement entirely — execute-time keying catches it")
		assert.Equal(t, authorizedOverwriteWarning(dst), winner.Warnings[0])

		loser, err := org.Organize(context.Background(), batchCmd("/in/B.mkv", tracker))
		require.NoError(t, err)
		require.True(t, loser.DuplicateSkipped)
		require.Len(t, loser.Warnings, 1)
		assert.Contains(t, loser.Warnings[0], "duplicate destination within batch")
		assert.NotContains(t, loser.Warnings[0], "overwrite authorized: replaced existing destination")

		content, readErr := afero.ReadFile(base, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("a-bytes"), content, "the winner's bytes won both the plant and the batch")
	})

	t.Run("winner occupied only at plan: no crumb anywhere — only the loser dup demotion", func(t *testing.T) {
		forceCasePosture(t, true)
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll(filepath.Dir(dst), 0o755))
		require.NoError(t, afero.WriteFile(base, dst, []byte("plan-time-resident"), 0o644))
		wedge := &toctouWedgeFs{Fs: base, target: dst, wedge: func(fs afero.Fs) {
			require.NoError(t, fs.Remove(dst))
		}}
		org := batchOrg(wedge)
		tracker := NewDuplicateTracker(false)

		winner, err := org.Organize(context.Background(), batchCmd("/in/A.mkv", tracker))
		require.NoError(t, err)
		require.True(t, winner.Moved)
		assert.Empty(t, winner.Warnings,
			"occupied at plan, vacant at execute: the winner replaced NOTHING (false-positive wedge)")

		loser, err := org.Organize(context.Background(), batchCmd("/in/B.mkv", tracker))
		require.NoError(t, err)
		require.True(t, loser.DuplicateSkipped)
		require.Len(t, loser.Warnings, 1, "exactly the dup demotion — no crumb leaked from any plan")
		assert.Contains(t, loser.Warnings[0], "duplicate destination within batch")

		content, readErr := afero.ReadFile(base, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("a-bytes"), content)
	})
}

// ---------------------------------------------------------------------------
// P3 (review round 1): link-install + in-place authorized lanes. Every lane
// whose execute leg replaces resident bytes under -f carries the same crumb,
// keyed to execute-time occupancy; lanes that only reshape names without
// destroying foreign bytes stay silent deliberately.
// ---------------------------------------------------------------------------

// forceAuditStrategyFixture builds a MemMapFs with a source under /in and an
// organize-strategy wired to the given linker (MemLinker records installs;
// nil uses OSLinker for OsFs fixtures).
func forceAuditStrategy(t *testing.T, l linker) (*organizeStrategy, afero.Fs, string) {
	t.Helper()
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/in", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/in/A.mkv", []byte("winner-bytes"), 0o644))
	strategy := newOrganizeStrategy(fs, &Config{
		FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true,
	}, nil, l)
	return strategy, fs, "/in/A.mkv"
}

func forceAuditLinkPlan(src, dst string, mode LinkMode, force bool) *OrganizePlan {
	return &OrganizePlan{
		Match:      models.FileMatchInfo{MovieID: "ABC-123", Path: src, Name: "A.mkv", Extension: ".mkv"},
		SourcePath: src, TargetDir: filepath.Dir(dst), TargetPath: dst, TargetFile: filepath.Base(dst),
		WillMove: true, moveFiles: false, LinkMode: mode, overwriteAuthorized: force,
	}
}

func TestForceOverwriteAudit_LinkInstallLanes(t *testing.T) {
	const dst = "/dest/ABC-123/ABC-123.mkv"

	t.Run("force hardlink install replacing a resident file crumbs", func(t *testing.T) {
		strategy, fs, src := forceAuditStrategy(t, &MemLinker{})
		ml := strategy.linker.(*MemLinker)
		require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
		require.NoError(t, afero.WriteFile(fs, dst, []byte("resident-bytes"), 0o644))

		result, err := strategy.Execute(forceAuditLinkPlan(src, dst, LinkModeHard, true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.Len(t, result.Warnings, 1,
			"an authorized link install REPLACES resident bytes at the destination — the crumb must fire")
		assert.Equal(t, authorizedOverwriteWarning(dst), result.Warnings[0])
		require.Len(t, ml.Links, 1)
		assert.Equal(t, "hard", ml.Links[0].Kind)
		assert.Equal(t, dst, ml.Links[0].NewName)
		dstExists, statErr := afero.Exists(fs, dst)
		require.NoError(t, statErr)
		assert.False(t, dstExists,
			"the resident entry was removed so the link install could take the name (MemLinker records the install itself)")
	})

	t.Run("force softlink install replacing a resident file crumbs", func(t *testing.T) {
		strategy, fs, src := forceAuditStrategy(t, &MemLinker{})
		ml := strategy.linker.(*MemLinker)
		require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
		require.NoError(t, afero.WriteFile(fs, dst, []byte("resident-bytes"), 0o644))

		result, err := strategy.Execute(forceAuditLinkPlan(src, dst, LinkModeSoft, true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.Len(t, result.Warnings, 1)
		assert.Equal(t, authorizedOverwriteWarning(dst), result.Warnings[0])
		require.Len(t, ml.Links, 1)
		assert.Equal(t, "soft", ml.Links[0].Kind)
	})

	t.Run("force hardlink install onto a vacant dest stays silent", func(t *testing.T) {
		strategy, _, src := forceAuditStrategy(t, &MemLinker{})
		ml := strategy.linker.(*MemLinker)

		result, err := strategy.Execute(forceAuditLinkPlan(src, dst, LinkModeHard, true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		assert.Empty(t, result.Warnings, "no resident bytes existed — nothing was replaced")
		require.Len(t, ml.Links, 1, "the install still ran")
	})
}

// Same-inode alias lanes: an authorized install recycling a name that ALIASES
// the source's own inode replaces NO foreign bytes — the crumb must stay
// silent even though the destination entry itself is consumed and recreated.
func TestForceOverwriteAudit_SameInodeAliasLanes_Silent(t *testing.T) {
	if testing.Short() {
		t.Skip("os-level hardlink fixture")
	}

	t.Run("hardlink install onto a hardlink alias of the source stays silent", func(t *testing.T) {
		dir := t.TempDir()
		fs := afero.NewOsFs()
		src := filepath.Join(dir, "in", "ABC-123.mkv")
		dst := filepath.Join(dir, "dest", "ABC-123", "ABC-123.mkv")
		require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
		require.NoError(t, os.WriteFile(src, []byte("shared-bytes"), 0o644))
		require.NoError(t, os.Link(src, dst), "destination is a hardlink ALIAS of the source — same inode, zero foreign bytes")

		strategy := newOrganizeStrategy(fs, &Config{
			FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true,
		}, nil, nil) // OSLinker: real link installs against OsFs
		result, err := strategy.Execute(forceAuditLinkPlan(src, dst, LinkModeHard, true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		assert.Empty(t, result.Warnings,
			"removing an alias NAME destroyed no bytes — the inode lives on through the source name")
		content, readErr := os.ReadFile(dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("shared-bytes"), content)
		srcInfo, statErr := os.Stat(src)
		require.NoError(t, statErr)
		dstInfo, statErr := os.Stat(dst)
		require.NoError(t, statErr)
		assert.True(t, os.SameFile(srcInfo, dstInfo), "the recycled name still aliases the source inode")
	})

	t.Run("copy onto a hardlink alias of the source stays silent", func(t *testing.T) {
		dir := t.TempDir()
		fs := afero.NewOsFs()
		src := filepath.Join(dir, "in", "ABC-123.mkv")
		dst := filepath.Join(dir, "dest", "ABC-123", "ABC-123.mkv")
		require.NoError(t, os.MkdirAll(filepath.Dir(src), 0o755))
		require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
		require.NoError(t, os.WriteFile(src, []byte("shared-bytes"), 0o644))
		require.NoError(t, os.Link(src, dst))

		strategy := newOrganizeStrategy(fs, &Config{
			FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true,
		}, nil, nil)
		result, err := strategy.Execute(forceAuditLinkPlan(src, dst, LinkModeNone, true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		assert.Empty(t, result.Warnings,
			"the authorized copy republished identical bytes over an alias — no FOREIGN resident bytes were replaced")
		content, readErr := os.ReadFile(dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("shared-bytes"), content)
		sourceContent, readErr := os.ReadFile(src)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("shared-bytes"), sourceContent, "a copy retains its source")
	})
}

// In-place authorized lanes: the inner FILE rename and the non-rename file
// MOVE both genuinely replace resident bytes under -f (crumb); the pure
// directory rename replaces nothing foreign by construction (silent).
func TestForceOverwriteAudit_InPlaceLanes(t *testing.T) {
	inPlaceCmd := func(src, name string, force bool) OrganizeCmd {
		return OrganizeCmd{
			Match:       models.FileMatchInfo{MovieID: "ABC-100", Path: src, Name: name, Extension: ".mkv"},
			Movie:       &models.Movie{ID: "ABC-100"},
			DestDir:     "/dest",
			MoveFiles:   true,
			ForceUpdate: force,
		}
	}

	t.Run("inner rename replacing a foreign occupant crumbs", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		cfg := &Config{
			FolderFormat: "shared", FileFormat: "<ID> vidfile",
			RenameFile: true, OperationMode: operationmode.OperationModeInPlace,
		}
		require.NoError(t, fs.MkdirAll("/pool/oldA", 0o755))
		require.NoError(t, afero.WriteFile(fs, "/pool/oldA/ABC-100 ownA.mkv", []byte("a-bytes"), 0o644))
		require.NoError(t, afero.WriteFile(fs, "/pool/oldA/ABC-100 vidfile.mkv", []byte("plant-bytes"), 0o644),
			"a foreign file already carries the new name inside the folder about to be renamed")
		org := NewOrganizer(fs, cfg, nil, ipfMatcher(t))

		result, err := org.Organize(context.Background(), inPlaceCmd("/pool/oldA/ABC-100 ownA.mkv", "ABC-100 ownA.mkv", true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.True(t, result.InPlaceRenamed)
		require.Len(t, result.Warnings, 1,
			"the authorized inner rename replaced the foreign occupant's resident bytes")
		assert.Equal(t, authorizedOverwriteWarning(filepath.FromSlash("/pool/shared/ABC-100 vidfile.mkv")),
			filepath.FromSlash(result.Warnings[0]))
		content, readErr := afero.ReadFile(fs, "/pool/shared/ABC-100 vidfile.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("a-bytes"), content, "the plant's bytes were replaced")
	})

	t.Run("inner rename onto a vacant name stays silent — the dir rename replaces no foreign bytes", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		cfg := &Config{
			FolderFormat: "shared", FileFormat: "<ID> vidfile",
			RenameFile: true, OperationMode: operationmode.OperationModeInPlace,
		}
		require.NoError(t, fs.MkdirAll("/pool/oldA", 0o755))
		require.NoError(t, afero.WriteFile(fs, "/pool/oldA/ABC-100 ownA.mkv", []byte("a-bytes"), 0o644))
		org := NewOrganizer(fs, cfg, nil, ipfMatcher(t))

		result, err := org.Organize(context.Background(), inPlaceCmd("/pool/oldA/ABC-100 ownA.mkv", "ABC-100 ownA.mkv", true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.True(t, result.InPlaceRenamed)
		assert.Empty(t, result.Warnings,
			"rename-only legs (directory + file onto vacant names) replace nothing — no crumb")
		content, readErr := afero.ReadFile(fs, "/pool/shared/ABC-100 vidfile.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("a-bytes"), content)
	})

	t.Run("in-place non-dedicated move replacing a foreign occupant crumbs", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		cfg := &Config{
			FolderFormat: "<ID>", FileFormat: "<ID> vidfile",
			RenameFile: true, OperationMode: operationmode.OperationModeInPlace,
		}
		require.NoError(t, fs.MkdirAll("/pool/mixed", 0o755))
		require.NoError(t, afero.WriteFile(fs, "/pool/mixed/ABC-100 ownA.mkv", []byte("a-bytes"), 0o644))
		require.NoError(t, afero.WriteFile(fs, "/pool/mixed/DEF-999 other.mkv", []byte("other-tenant"), 0o644),
			"a second-ID tenant keeps the folder mixed (non-dedicated → non-rename move lane)")
		require.NoError(t, afero.WriteFile(fs, "/pool/mixed/ABC-100 vidfile.mkv", []byte("plant-bytes"), 0o644))
		org := NewOrganizer(fs, cfg, nil, ipfMatcher(t))

		result, err := org.Organize(context.Background(), inPlaceCmd("/pool/mixed/ABC-100 ownA.mkv", "ABC-100 ownA.mkv", true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.Len(t, result.Warnings, 1)
		assert.Equal(t, authorizedOverwriteWarning(filepath.FromSlash("/pool/mixed/ABC-100 vidfile.mkv")),
			filepath.FromSlash(result.Warnings[0]))
		content, readErr := afero.ReadFile(fs, "/pool/mixed/ABC-100 vidfile.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("a-bytes"), content)
		srcExists, statErr := afero.Exists(fs, "/pool/mixed/ABC-100 ownA.mkv")
		require.NoError(t, statErr)
		assert.False(t, srcExists, "the move landed")
	})
}

// In-place-norenamefolder: a same-directory file rename whose authorized leg
// replaces a foreign file carrying the target name (crumb); vacant target
// names stay silent.
func TestForceOverwriteAudit_InPlaceNoRenameFolderLane(t *testing.T) {
	fixture := func(t *testing.T, plant bool) (*Organizer, afero.Fs) {
		t.Helper()
		fs := afero.NewMemMapFs()
		cfg := &Config{
			FolderFormat: "<ID>", FileFormat: "<ID> vidfile",
			RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
		}
		require.NoError(t, fs.MkdirAll("/pool", 0o755))
		require.NoError(t, afero.WriteFile(fs, "/pool/ABC-100 ownA.mkv", []byte("a-bytes"), 0o644))
		if plant {
			require.NoError(t, afero.WriteFile(fs, "/pool/ABC-100 vidfile.mkv", []byte("plant-bytes"), 0o644))
		}
		return NewOrganizer(fs, cfg, nil, nil), fs
	}
	cmd := func(force bool) OrganizeCmd {
		return OrganizeCmd{
			Match:       models.FileMatchInfo{MovieID: "ABC-100", Path: "/pool/ABC-100 ownA.mkv", Name: "ABC-100 ownA.mkv", Extension: ".mkv"},
			Movie:       &models.Movie{ID: "ABC-100"},
			DestDir:     "/dest",
			MoveFiles:   true,
			ForceUpdate: force,
		}
	}

	t.Run("authorized rename replacing a foreign occupant crumbs", func(t *testing.T) {
		org, fs := fixture(t, true)
		result, err := org.Organize(context.Background(), cmd(true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.Len(t, result.Warnings, 1)
		assert.Equal(t, authorizedOverwriteWarning("/pool/ABC-100 vidfile.mkv"), result.Warnings[0])
		content, readErr := afero.ReadFile(fs, "/pool/ABC-100 vidfile.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("a-bytes"), content, "the plant's bytes were replaced")
	})

	t.Run("authorized rename onto a vacant name stays silent", func(t *testing.T) {
		org, fs := fixture(t, false)
		result, err := org.Organize(context.Background(), cmd(true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		assert.Empty(t, result.Warnings)
		content, readErr := afero.ReadFile(fs, "/pool/ABC-100 vidfile.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("a-bytes"), content)
	})
}
