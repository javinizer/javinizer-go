package workflow

import (
	"context"
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
	wantPart, err := artifactDigest(base, multipart)
	require.NoError(t, err)
	require.Equal(t, []models.DeleteEntry{
		{Path: subMoves[0].NewPath, SHA256: wantFirst},
		{Path: finalPlan.TargetPath, SHA256: wantVideo},
		{Path: filepath.Join(finalPlan.TargetDir, "movie-cd2.mp4"), SHA256: wantPart},
	}, ledger.deletePaths, "exactly one pin per normalized endpoint, digest of the FIRST planned source — never the skipped duplicate's bytes (the tree lane pins its own multipart install)")
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
