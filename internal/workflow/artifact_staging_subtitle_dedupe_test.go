package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

// dupSubFiles mirrors pr260FencedFiles but stages TWO subtitle sources whose
// language suffixes normalize onto ONE organizer destination name
// (.en.srt and .eng.srt both become <target>.eng.srt — codex P2,
// PRRT_kwDORn9KaM6m7CBR).
func dupSubFiles(t *testing.T, slug string) (afero.Fs, string, string, string, string, string, string, models.FileMatchInfo) {
	t.Helper()
	fs := afero.NewOsFs()
	root := t.TempDir()
	sourceDir := filepath.Join(root, "incoming")
	base := "PR260-" + strings.ToUpper(slug)
	source := filepath.Join(sourceDir, base+".mp4")
	subFirst := filepath.Join(sourceDir, base+".en.srt")
	subSecond := filepath.Join(sourceDir, base+".eng.srt")
	multipart := filepath.Join(sourceDir, base+"-cd2.mp4")
	unrelated := filepath.Join(sourceDir, "do-not-touch.txt")
	require.NoError(t, fs.MkdirAll(sourceDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0o644))
	require.NoError(t, afero.WriteFile(fs, subFirst, []byte("english-sub-first"), 0o644))
	require.NoError(t, afero.WriteFile(fs, subSecond, []byte("english-sub-second"), 0o644))
	require.NoError(t, afero.WriteFile(fs, multipart, []byte("part two"), 0o644))
	require.NoError(t, afero.WriteFile(fs, unrelated, []byte("unrelated"), 0o644))
	match := models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4"}
	return fs, root, source, subFirst, subSecond, multipart, unrelated, match
}

// The deferred copy/link arming loop dedupes normalized subtitle endpoints
// BEFORE arming (first-wins, mirroring the organizer's sequential lane): one
// BeforePublish/Yield per endpoint, one durable pin keyed to the FIRST planned
// source's digest, and one keep entry at reconciliation. Before the fix the
// second BeforePublish collided with the first's own live busy claim and
// failed the whole apply.
func TestDeferredCopyDuplicateSubtitleEndpointsSinglePin(t *testing.T) {
	base, root, source, subFirst, subSecond, multipart, unrelated, match := dupSubFiles(t, "copy-dup-single-pin")
	dest := filepath.Join(root, "library")
	real := copyIntentOrganizer(base)
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: real, revertLog: ledger}
	cmd := copyIntentCommand(&models.Movie{ContentID: "copy-dup-single-pin"}, match, dest)
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	stagedPlan, finalPlan := copyIntentPlans(t, real, stage, match, source, dest)
	subMoves := real.PlanSubtitleMoves(finalPlan)
	require.Len(t, subMoves, 2, "both source subtitles plan onto the endpoint set")
	require.Equal(t, filepath.Clean(subMoves[0].NewPath), filepath.Clean(subMoves[1].NewPath), "both normalize onto ONE endpoint")
	require.Equal(t, filepath.Clean(subFirst), filepath.Clean(subMoves[0].OriginalPath), "the alphabetically-first source plans first")
	require.Equal(t, filepath.Clean(subSecond), filepath.Clean(subMoves[1].OriginalPath))
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}

	require.NoError(t, stage.publish(context.Background(), orch, state, nil),
		"duplicate-normalized endpoints must not collide their own busy claim")

	wantFirst, err := artifactDigest(base, subFirst)
	require.NoError(t, err)
	wantVideo, err := artifactDigest(base, source)
	require.NoError(t, err)
	wantVideoInfo, wantVideoPartial, err := fsutil.PartialCopyDigest(base, source)
	require.NoError(t, err)
	wantPart, err := artifactDigest(base, multipart)
	require.NoError(t, err)
	require.Equal(t, []models.DeleteEntry{
		{Path: subMoves[0].NewPath, SHA256: wantFirst},
		{Path: finalPlan.TargetPath, CopySize: wantVideoInfo.Size(), CopyPartialSHA256: wantVideoPartial},
		{Path: filepath.Join(finalPlan.TargetDir, "movie-cd2.mp4"), SHA256: wantPart},
	}, ledger.deletePaths, "exactly one pin per normalized endpoint, digest of the FIRST planned source — never the skipped duplicate's bytes (the tree lane pins its own multipart install); the primary pins the interim partial shape (codex P2, PRRT_kwDORn9KaM6nBUrF)")
	require.Equal(t, []sealedCopyDigest{{path: finalPlan.TargetPath, sha256: wantVideo}}, ledger.sealsCaptured,
		"the publish stream's teed digest seals the interim pin — and it must equal the independently computed source digest, proving the tee counted the admitted bytes")
	pinHits := 0
	for _, pd := range ledger.deletePaths {
		if pd.Path == subMoves[0].NewPath {
			pinHits++
		}
	}
	require.Equal(t, 1, pinHits, "no duplicate pin survives the arming dedupe")
	require.Equal(t, []string{subMoves[0].NewPath}, ledger.deleteKeepCaptured, "the reconciler keep-set carries the single armed endpoint once")

	endpointBytes, err := afero.ReadFile(base, subMoves[0].NewPath)
	require.NoError(t, err)
	assert.Equal(t, "english-sub-first", string(endpointBytes), "the first-planned source's bytes won the endpoint")
	require.Len(t, state.organizeResult.Subtitles, 2)
	assert.True(t, state.organizeResult.Subtitles[0].Copied, "first seat installed")
	assert.Equal(t, filepath.Clean(subFirst), filepath.Clean(state.organizeResult.Subtitles[0].OriginalPath))
	assert.True(t, state.organizeResult.Subtitles[1].Skipped, "second seat skipped over the first's install")
	assert.Equal(t, filepath.Clean(subSecond), filepath.Clean(state.organizeResult.Subtitles[1].OriginalPath))

	busy, err := afero.Exists(base, subMoves[0].NewPath+fsutil.ReplacementBusySuffix)
	require.NoError(t, err)
	assert.False(t, busy, "the single armed leg confirmed and released its claim")
	entries, err := afero.ReadDir(base, finalPlan.TargetDir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	assert.NotContains(t, names, "movie.en.srt", "the installed seat's staged duplicate never double-published through the tree (PRRT_kwDORn9KaM6m7CBi exclusion)")
	assert.Contains(t, names, "movie.eng.srt")
	assert.Contains(t, names, "movie-cd2.mp4", "unplanned siblings still install through the tree")
	secondRetained, err := afero.Exists(base, subSecond)
	require.NoError(t, err)
	assert.True(t, secondRetained, "copy mode retains the skipped duplicate source")
	pr260AssertRetained(t, base, source, subFirst, multipart, unrelated)
}

// The deferred-MOVE intent journaling dedupes the same normalized endpoint
// BEFORE any pending intent lands (codex P2, PRRT_kwDORn9KaM6m_lgt): the
// organizer's sequential lane moves the FIRST planned source and skips the
// duplicate, so exactly one MoveBack intent journals per shared destination
// (pinned to the winner's source), the skipped duplicate keeps no row at all
// for the outcome reconciliation to retract, and the reconciler's keep-set
// carries exactly video + winner.
func TestDeferredMoveDuplicateSubtitleEndpointsSingleIntent(t *testing.T) {
	fs, root, source, subFirst, subSecond, multipart, unrelated, match := dupSubFiles(t, "move-dup-single-intent")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: fs, organizer: real, revertLog: ledger}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "move-dup-single-intent"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false

	stage, state, publishErr := verifiedStagePublish(t, orch, real, fs, root, source, dest, match, cmd)
	defer stage.cleanup()
	require.NoError(t, publishErr)

	require.Len(t, state.organizeResult.Subtitles, 2, "both source subtitles enumerate onto the endpoint set")
	seats := state.organizeResult.Subtitles
	require.Equal(t, filepath.Clean(seats[0].NewPath), filepath.Clean(seats[1].NewPath), "both normalize onto ONE endpoint")
	assert.True(t, seats[0].Moved, "the first-planned source won the endpoint")
	assert.True(t, seats[1].Skipped, "the duplicate source was skipped in place")
	require.Equal(t, filepath.Clean(subFirst), filepath.Clean(seats[0].OriginalPath))
	require.Equal(t, filepath.Clean(subSecond), filepath.Clean(seats[1].OriginalPath))
	endpoint := filepath.Clean(seats[0].NewPath)

	endpointIntents := 0
	for _, mv := range ledger.movesCaptured {
		assert.NotEqual(t, filepath.Clean(subSecond), filepath.Clean(mv.OriginalPath), "the skipped duplicate never journals a move intent — no row for reconciliation to see")
		if filepath.Clean(mv.NewPath) == endpoint {
			endpointIntents++
			assert.Equal(t, filepath.Clean(subFirst), filepath.Clean(mv.OriginalPath), "the shared endpoint's only intent names the winner's source")
		}
	}
	assert.Equal(t, 1, endpointIntents, "single MoveBack intent per normalized destination")
	assert.Len(t, ledger.movesCaptured, 3, "video + winner subtitle + trampoline-published multipart")
	videoTarget := state.organizeResult.NewPath
	require.Len(t, ledger.keepCaptured, 2, "the reconciler keep-set carries exactly the winner video and winner subtitle")
	assert.Contains(t, ledger.keepCaptured, models.FileMove{OriginalPath: source, NewPath: videoTarget})
	assert.Contains(t, ledger.keepCaptured, models.FileMove{OriginalPath: subFirst, NewPath: seats[0].NewPath})

	got, err := afero.ReadFile(fs, seats[0].NewPath)
	require.NoError(t, err)
	assert.Equal(t, "english-sub-first", string(got), "the first-planned source's bytes won the endpoint")
	gone, statErr := afero.Exists(fs, subFirst)
	require.NoError(t, statErr)
	assert.False(t, gone, "the winner's source was consumed by the move")
	got, err = afero.ReadFile(fs, subSecond)
	require.NoError(t, err)
	assert.Equal(t, "english-sub-second", string(got), "the skipped duplicate keeps its source spot, unconsumed")
	got, err = afero.ReadFile(fs, videoTarget)
	require.NoError(t, err)
	assert.Equal(t, "video", string(got))
	for _, gone := range []string{source, multipart} {
		exists, serr := afero.Exists(fs, gone)
		require.NoError(t, serr)
		assert.False(t, exists, gone)
	}
	exists, statErr := afero.Exists(fs, unrelated)
	require.NoError(t, statErr)
	assert.True(t, exists)
}

// Crash-into-window replay of the F2 journal shape (codex P2,
// PRRT_kwDORn9KaM6m_lgt), written through the production journal writers and
// reverted by the production reverter exactly like
// TestPendingSiblingMoveIntentCrashReplayKeepsSourceAndPinnedCopy: the
// process "exits" AFTER execution consumed the video and the WINNER subtitle
// but BEFORE ReconcileMoveIntents. The journal holds one pending intent per
// shared destination (the winner's) and none for the skipped duplicate, so
// nothing suppresses the winner's rename-back — recovery restores the winner
// onto its source, the shared endpoint empties, and the duplicate source sits
// untouched with no row ever naming it. Before the dedupe the loser's
// never-consumed intent suppressed the SHARED destination and stranded the
// winner's bytes in the library.
func TestDuplicateSubtitleMoveIntentCrashReplayRestoresWinnerOnly(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "dup-sub-crash", "")
	fs, root, source, subFirst, subSecond, multipart, unrelated, match := dupSubFiles(t, "dup-sub-crash")
	dest := filepath.Join(root, "library")
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "dup-sub-crash", fs, nil, nil, nil)
	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: &movie, Match: match, DestPath: dest, Organize: OrganizeOptions{MoveFiles: true}})
	require.NoError(t, err)

	finalDir := filepath.Join(dest, "movie")
	videoTarget := filepath.Join(finalDir, "movie.mp4")
	endpoint := filepath.Join(finalDir, "movie.eng.srt")
	require.NoError(t, fs.MkdirAll(finalDir, 0o755))

	// The execution's disk state at the crash instant: video and winner
	// sources consumed into the library; the duplicate-normalized loser was
	// skipped by the organizer lane and retained its source spot untouched.
	require.NoError(t, afero.WriteFile(fs, videoTarget, []byte("video"), 0o644))
	require.NoError(t, afero.WriteFile(fs, endpoint, []byte("english-sub-first"), 0o644))
	require.NoError(t, fs.Remove(source))
	require.NoError(t, fs.Remove(subFirst))

	// The post-dedupe writer shape, through the real recorder: one pending
	// intent for the video and ONE for the shared subtitle endpoint.
	require.NoError(t, log.RecordMoveIntent(context.Background(), opID, source, videoTarget))
	require.NoError(t, log.RecordMoveIntent(context.Background(), opID, subFirst, endpoint))
	require.NoError(t, db.Model(&models.BatchFileOperation{}).Where("id = ?", mustParseOpID(t, opID)).Update("new_path", videoTarget).Error)

	row, rowErr := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, rowErr)
	journal, parseErr := models.ParseGeneratedFiles(row.GeneratedFiles)
	require.NoError(t, parseErr)
	endpointRows := 0
	for _, fm := range journal.MoveBack {
		assert.NotEqual(t, subSecond, fm.OriginalPath, "the skipped duplicate has no row to suppress the shared endpoint")
		if fm.NewPath == endpoint {
			endpointRows++
		}
	}
	require.Equal(t, 1, endpointRows, "single MoveBack per shared destination")

	res, revErr := history.NewReverter(fs, repo).RevertBatch(t.Context(), "dup-sub-crash")
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)

	restored, readErr := afero.ReadFile(fs, subFirst)
	require.NoError(t, readErr, "the winner's rename-back fires — its suppression check finds the consumed source absent")
	assert.Equal(t, "english-sub-first", string(restored))
	gone, statErr := afero.Exists(fs, endpoint)
	require.NoError(t, statErr)
	assert.False(t, gone, "the shared endpoint vacated back onto the winner's source")
	loser, readErr := afero.ReadFile(fs, subSecond)
	require.NoError(t, readErr)
	assert.Equal(t, "english-sub-second", string(loser), "the skipped duplicate retains its spot — source-consumed-only semantics apply")
	videoBack, videoErr := afero.ReadFile(fs, source)
	require.NoError(t, videoErr)
	assert.Equal(t, "video", string(videoBack), "the consumed primary restores alongside")
	persisted, findErr := repo.FindByID(context.Background(), mustParseOpID(t, opID))
	require.NoError(t, findErr)
	assert.Equal(t, models.RevertStatusReverted, persisted.RevertStatus)
	for _, kept := range []string{multipart, unrelated} {
		exists, existsErr := afero.Exists(fs, kept)
		require.NoError(t, existsErr)
		assert.True(t, exists, kept)
	}
}

// End-to-end deferred copy with duplicate-normalized subtitle endpoints: the
// apply succeeds (no all-up failure), exactly one endpoint is published with
// the first source's bytes, the durable ledger carries exactly one entry for
// the endpoint, and a revert reaps that install while retaining every source.
func TestDeferredCopyDuplicateSubtitleEndpointsGraduateOnce(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	slug := "copy-dup-e2e"
	movie := pr260FencedMovie(t, db, slug, "")
	fs, root, source, subFirst, subSecond, multipart, unrelated, match := dupSubFiles(t, slug)
	dest := filepath.Join(root, "library")
	engine := template.NewEngine()
	org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ACTRESS>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, engine, nil)
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), slug, fs, nil, nil, nil)
	nameCfg := nfo.NFONameConfig{FilenameTemplate: "<ACTRESS>.nfo", FirstNameOrder: true}
	orch := newApplyOrchestrator(fs, org, nil, nil, nil, ApplyConfig{NFONameCfg: nameCfg}, engine, log, nil, nil)

	probePlan, err := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: &movie, DestDir: dest, ForceUpdate: true, OperationMode: operationmode.OperationModeOrganize})
	require.NoError(t, err)
	subMoves := org.PlanSubtitleMoves(probePlan)
	require.Len(t, subMoves, 2)
	require.Equal(t, filepath.Clean(subMoves[0].NewPath), filepath.Clean(subMoves[1].NewPath))
	subTarget := subMoves[0].NewPath

	cmd := pr260FencedCommand(&movie, match, dest, pr260FencedCounter(t, db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	result, err := orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result.OrganizeResult)
	video := result.OrganizeResult.NewPath
	require.FileExists(t, video)

	endpointBytes, err := afero.ReadFile(fs, subTarget)
	require.NoError(t, err, "exactly one endpoint published")
	assert.Equal(t, "english-sub-first", string(endpointBytes), "first-planned source's bytes won the endpoint")

	require.Len(t, result.OrganizeResult.Subtitles, 2)
	assert.True(t, result.OrganizeResult.Subtitles[0].Copied)
	assert.True(t, result.OrganizeResult.Subtitles[1].Skipped)

	stem := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	misnamed, err := afero.Exists(fs, filepath.Join(filepath.Dir(video), stem+".en.srt"))
	require.NoError(t, err)
	assert.False(t, misnamed, "the installed seat's staged duplicate never double-published through the tree")
	siblingBytes, err := afero.ReadFile(fs, filepath.Join(filepath.Dir(video), stem+"-cd2.mp4"))
	require.NoError(t, err, "unplanned siblings still install through the tree")
	assert.Equal(t, "part two", string(siblingBytes))
	secondRetained, err := afero.Exists(fs, subSecond)
	require.NoError(t, err)
	assert.True(t, secondRetained, "copy mode retains the skipped duplicate source")
	pr260AssertRetained(t, fs, source, subFirst, multipart, unrelated)
	pr260AssertStageGone(t, fs, filepath.Dir(dest))

	ledger := p3Ledger(t, repo, result.OperationID)
	deleteHits := 0
	for _, del := range ledger.Delete {
		if del == subTarget {
			deleteHits++
		}
	}
	assert.Equal(t, 1, deleteHits, "exactly one ledger entry for the normalized endpoint")
	for _, pd := range ledger.PlannedDeletes {
		assert.NotEqual(t, subTarget, pd.Path, "the endpoint pin graduated out of pending intent")
	}
	for _, rp := range ledger.Replacements {
		assert.NotEqual(t, subTarget, rp.Destination, "the endpoint never registered as a replacement")
	}

	res, revErr := history.NewReverter(fs, repo).RevertBatch(t.Context(), slug)
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)
	gone, statErr := afero.Exists(fs, subTarget)
	require.NoError(t, statErr)
	assert.False(t, gone, "revert reaps the installed endpoint")
	require.FileExists(t, video, "copy-mode revert retains the installed primary")
	pr260AssertRetained(t, fs, source, subFirst, multipart, unrelated)
	secondRetainedAfter, err := afero.Exists(fs, subSecond)
	require.NoError(t, err)
	assert.True(t, secondRetainedAfter, "revert never consumes the skipped duplicate source")
}

// Winner refusal on a shared endpoint, deferred MOVE (codex P2,
// PRRT_kwDORn9KaM6nmSaI): the first-planned duplicate's verified-source proof
// detects a swap AFTER the pre-execution gate and refuses before publishing,
// leaving the endpoint vacant. Before the strict first-wins gate the SECOND
// duplicate then moved into that vacancy with NO journaled inverse — the
// intent dedupe journals the first source only — so a crash before
// reconciliation stranded the second source's bytes at the destination. Now
// the duplicate is blocked (Skipped, never Moved), its seat keeps no journal
// row, and the endpoint's only pending intent still names the first source.
// The apply itself aborts on the swapped winner's republish revalidation
// exactly like the single-subtitle refusal, and rollback restores everything.
func TestDeferredMoveDuplicateWinnerRefusalBlocksSecond(t *testing.T) {
	fs, root, source, subFirst, subSecond, multipart, unrelated, match := dupSubFiles(t, "move-dup-winner-refusal")
	dest := filepath.Join(root, "library")
	real := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	ledger := &completeCallFaultLog{}
	aside := filepath.Join(root, "incoming", "swapped-aside.srt")
	seats := map[string]organizer.SubtitleResult{}
	fault := &pr260PublicationFaultOrganizer{Organizer: real, preExecute: func(*organizer.OrganizePlan) {
		// The winner's bound proof detects this rename-swap at the consume —
		// after the pre-execution gate already passed.
		_ = fs.Rename(subFirst, aside)
		_ = afero.WriteFile(fs, subFirst, []byte("replacement subtitle"), 0o644)
	}, afterExecute: func(_ *organizer.OrganizePlan, result *organizer.OrganizeResult) {
		for _, sr := range result.Subtitles {
			seats[filepath.Clean(sr.OriginalPath)] = sr
		}
	}}
	orch := &applyOrchImpl{fs: fs, organizer: fault, revertLog: ledger}
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "move-dup-winner-refusal"}, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false

	stage, _, publishErr := verifiedStagePublish(t, orch, real, fs, root, source, dest, match, cmd)
	defer stage.cleanup()

	require.ErrorIs(t, publishErr, errArtifactSourceChanged, "the swapped winner aborts the apply exactly like the single-subtitle refusal")
	winner, ok := seats[filepath.Clean(subFirst)]
	require.True(t, ok, "the winner seat exists")
	require.Error(t, winner.Error)
	assert.True(t, errors.Is(winner.Error, fsutil.ErrTakeAsideForeign), "winner seat error class: %v", winner.Error)
	assert.False(t, winner.Moved || winner.Skipped, "a refusal is none of the consumption classes")
	dup, ok := seats[filepath.Clean(subSecond)]
	require.True(t, ok, "the duplicate seat exists")
	assert.True(t, dup.Skipped, "strict first-wins blocks the duplicate after the winner's failed attempt")
	assert.False(t, dup.Moved, "pre-fix the duplicate moved into the vacant endpoint with no journaled inverse")
	endpoint := filepath.Clean(winner.NewPath)
	require.NotEmpty(t, endpoint)
	require.Equal(t, endpoint, filepath.Clean(dup.NewPath), "both seats normalize onto ONE endpoint")

	for _, mv := range ledger.movesCaptured {
		assert.NotEqual(t, filepath.Clean(subSecond), filepath.Clean(mv.OriginalPath), "the blocked duplicate never journals a move intent")
		if filepath.Clean(mv.NewPath) == endpoint {
			assert.Equal(t, filepath.Clean(subFirst), filepath.Clean(mv.OriginalPath), "the endpoint's only intent names the first-planned source")
		}
	}
	exists, err := afero.Exists(fs, endpoint)
	require.NoError(t, err)
	assert.False(t, exists, "rollback leaves the shared endpoint vacant — nothing unjournaled installed")
	got, err := afero.ReadFile(fs, source)
	require.NoError(t, err)
	assert.Equal(t, "video", string(got), "rollback restored the moved video")
	got, err = afero.ReadFile(fs, subFirst)
	require.NoError(t, err)
	assert.Equal(t, "replacement subtitle", string(got), "the foreign replacement rides back onto the winner's name byte-intact")
	got, err = afero.ReadFile(fs, aside)
	require.NoError(t, err)
	assert.Equal(t, "english-sub-first", string(got), "the admitted winner object is never consumed")
	got, err = afero.ReadFile(fs, subSecond)
	require.NoError(t, err)
	assert.Equal(t, "english-sub-second", string(got), "the blocked duplicate keeps its source spot, unconsumed")
	pr260AssertNoFinals(t, fs, dest)
	for _, kept := range []string{multipart, unrelated} {
		exists, serr := afero.Exists(fs, kept)
		require.NoError(t, serr)
		assert.True(t, exists, kept)
	}
}

// Winner refusal on a shared endpoint, deferred COPY (codex P2,
// PRRT_kwDORn9KaM6nmSaI): the armed endpoint pin carries the FIRST-planned
// source's digest. Before the strict first-wins gate the second duplicate
// could still copy into the still-vacant endpoint after the winner's verified
// refusal — bytes the surviving pin can never authenticate, so a later
// revert's verified unlink refused and STRANDED the installed sidecar. Now
// the duplicate is blocked (Skipped, never Copied), the released pin is
// reconciled away, and the endpoint settles only through hash-coherent lanes
// (the staged-twin tree install pins exactly the bytes it lands), so
// recovery reaps precisely what this apply published.
func TestDeferredCopyDuplicateWinnerRefusalBlocksSecond(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	slug := "copy-dup-winner-refusal"
	movie := pr260FencedMovie(t, db, slug, "")
	fs, root, source, subFirst, subSecond, multipart, unrelated, match := dupSubFiles(t, slug)
	dest := filepath.Join(root, "library")
	engine := template.NewEngine()
	org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ACTRESS>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, engine, nil)
	aside := filepath.Join(root, "incoming", "swapped-aside.srt")
	fault := &pr260PublicationFaultOrganizer{Organizer: org, preExecute: func(*organizer.OrganizePlan) {
		// After the pre-execution gate, before the consume: the winner's bound
		// proof refuses this rename-swap; its armed pin keeps the ADMITTED
		// digest recorded at arming time.
		_ = fs.Rename(subFirst, aside)
		_ = afero.WriteFile(fs, subFirst, []byte("replacement subtitle"), 0o644)
	}}
	repo := database.NewBatchFileOperationRepository(db)
	log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), slug, fs, nil, nil, nil)
	nameCfg := nfo.NFONameConfig{FilenameTemplate: "<ACTRESS>.nfo", FirstNameOrder: true}
	orch := newApplyOrchestrator(fs, fault, nil, nil, nil, ApplyConfig{NFONameCfg: nameCfg}, engine, log, nil, nil)

	probePlan, err := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: &movie, DestDir: dest, ForceUpdate: true, OperationMode: operationmode.OperationModeOrganize})
	require.NoError(t, err)
	subMoves := org.PlanSubtitleMoves(probePlan)
	require.Len(t, subMoves, 2)
	require.Equal(t, filepath.Clean(subMoves[0].NewPath), filepath.Clean(subMoves[1].NewPath))
	require.Equal(t, filepath.Clean(subFirst), filepath.Clean(subMoves[0].OriginalPath))
	subTarget := subMoves[0].NewPath

	cmd := pr260FencedCommand(&movie, match, dest, pr260FencedCounter(t, db), operationmode.OperationModeOrganize, false, false, organizer.LinkModeNone, false, false)
	result, err := orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result.OrganizeResult)
	video := result.OrganizeResult.NewPath
	require.FileExists(t, video)

	require.Len(t, result.OrganizeResult.Subtitles, 2)
	seats := map[string]organizer.SubtitleResult{}
	for _, sr := range result.OrganizeResult.Subtitles {
		seats[filepath.Clean(sr.OriginalPath)] = sr
	}
	winner, ok := seats[filepath.Clean(subFirst)]
	require.True(t, ok, "the winner seat exists")
	require.Error(t, winner.Error, "the swapped winner's verified copy refuses")
	assert.True(t, errors.Is(winner.Error, fsutil.ErrTakeAsideForeign), "winner seat: %v", winner.Error)
	assert.False(t, winner.Copied)
	dup, ok := seats[filepath.Clean(subSecond)]
	require.True(t, ok, "the duplicate seat exists")
	assert.True(t, dup.Skipped, "strict first-wins blocks the duplicate after the winner's failed attempt")
	assert.False(t, dup.Copied, "pre-fix the duplicate copied bytes the armed pin could never authenticate")

	// The armed first-source pin released and reconciled away; the endpoint
	// reaches the destination only through the staged-twin tree install, which
	// pins exactly the bytes it lands — so recovery reaps the endpoint instead
	// of stranding it behind a hash it can never match.
	stem := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	winnerTwin := filepath.Join(filepath.Dir(video), stem+".en.srt")
	twinBytes, err := afero.ReadFile(fs, winnerTwin)
	require.NoError(t, err, "the refused winner's admitted staged twin installs through the tree (un-normalized leaf)")
	assert.Equal(t, "english-sub-first", string(twinBytes))
	endpointBytes, err := afero.ReadFile(fs, subTarget)
	require.NoError(t, err, "the shared endpoint is delivered by the hash-pinned tree twin")
	assert.Equal(t, "english-sub-second", string(endpointBytes))
	ledger := p3Ledger(t, repo, result.OperationID)
	// Nothing graduated through the organizer lane (no Copied seat restates the
	// endpoint at completion), so the tree installs stay hash-pinned planned
	// deletes the reverter verifies — exactly the crash-window shape. The
	// endpoint's single surviving pin must authenticate the bytes that actually
	// landed: pre-fix it carried the FIRST source's armed digest against the
	// second source's installed bytes (unlinkable on mismatch in the crash
	// window); post-fix the failed winner's armed pin is retracted and the
	// landed twin's own pin takes over.
	var endpointPins []models.DeleteEntry
	for _, pd := range ledger.PlannedDeletes {
		if pd.Path == subTarget {
			endpointPins = append(endpointPins, pd)
		}
	}
	require.Len(t, endpointPins, 1, "exactly one surviving pin for the normalized endpoint")
	landedDigest, err := artifactDigest(fs, subSecond)
	require.NoError(t, err)
	assert.Equal(t, landedDigest, endpointPins[0].SHA256, "the surviving pin authenticates the bytes that actually landed")
	failedWinnerDigest, err := artifactDigest(fs, aside)
	require.NoError(t, err)
	assert.NotEqual(t, failedWinnerDigest, endpointPins[0].SHA256, "the retracted armed pin carried the failed winner's digest — it must not survive")
	for _, rp := range ledger.Replacements {
		assert.NotEqual(t, subTarget, rp.Destination, "the endpoint never registered as a replacement")
	}

	res, revErr := history.NewReverter(fs, repo).RevertBatch(t.Context(), slug)
	require.NoError(t, revErr)
	require.Equal(t, 1, res.Succeeded)
	for _, reaped := range []string{subTarget, winnerTwin} {
		gone, statErr := afero.Exists(fs, reaped)
		require.NoError(t, statErr)
		assert.False(t, gone, "revert reaps the tree-installed sidecar: %s", reaped)
	}
	require.FileExists(t, video, "copy-mode revert retains the installed primary")
	pr260AssertRetained(t, fs, source, subFirst, multipart, unrelated)
	got, err := afero.ReadFile(fs, subFirst)
	require.NoError(t, err)
	assert.Equal(t, "replacement subtitle", string(got), "the foreign replacement is never consumed")
	got, err = afero.ReadFile(fs, aside)
	require.NoError(t, err)
	assert.Equal(t, "english-sub-first", string(got), "the admitted winner object survives at the swept-aside name")
	got, err = afero.ReadFile(fs, subSecond)
	require.NoError(t, err)
	assert.Equal(t, "english-sub-second", string(got), "copy mode retains the blocked duplicate source")
	pr260AssertStageGone(t, fs, filepath.Dir(dest))
}
