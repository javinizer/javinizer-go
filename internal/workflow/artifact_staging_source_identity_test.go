package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// swapReplace installs replacement bytes at path with the admitted size
// and mtime restored, as a true rename swap: the replacement is written
// under a temporary name while the original still occupies its inode, then
// renamed onto the vacated path, so POSIX allocators provably hand it a
// different inode. An unlink+create pair is not a valid swap fixture —
// ext4/tmpfs recycle a freshly freed inode number for the next create in
// the same directory, which makes before/after compare equal.
func swapReplace(t *testing.T, fs afero.Fs, path string, replacement []byte, admitted os.FileInfo) {
	t.Helper()
	aside := path + ".swap-aside"
	repl := path + ".swap-repl"
	require.NoError(t, fs.Rename(path, aside))
	require.NoError(t, afero.WriteFile(fs, repl, replacement, 0o644))
	require.NoError(t, fs.Chtimes(repl, admitted.ModTime(), admitted.ModTime()))
	require.NoError(t, fs.Rename(repl, path))
	require.NoError(t, fs.Remove(aside))
}

func TestCaptureArtifactSourceIdentityRejectsNonRegular(t *testing.T) {
	assert.False(t, captureArtifactSourceIdentity(nil, "", nil).known, "nil info captures nothing")
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/dir", 0o755))
	info, err := base.Stat("/dir")
	require.NoError(t, err)
	assert.False(t, captureArtifactSourceIdentity(base, "/dir", info).known, "a directory is never an admitted source")
}

func TestArtifactSourceIdentityMemfsShapeAndDrift(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/m.mp4", []byte("video"), 0o644))
	info, err := base.Stat("/m.mp4")
	require.NoError(t, err)
	id := captureArtifactSourceIdentity(base, "/m.mp4", info)
	require.True(t, id.known)
	assert.False(t, id.hasDevIno, "in-memory afero keeps the size+modtime legs only")
	assert.True(t, id.matches(base, "/m.mp4", info), "an untouched source still matches")
	assert.False(t, id.matches(base, "/m.mp4", nil))

	require.NoError(t, afero.WriteFile(base, "/m.mp4", []byte("video payload"), 0o644))
	grown, err := base.Stat("/m.mp4")
	require.NoError(t, err)
	assert.False(t, id.matches(base, "/m.mp4", grown), "a size change proves a rewrite")

	require.NoError(t, afero.WriteFile(base, "/m.mp4", []byte("VIDEO"), 0o644))
	moved := info.ModTime().Add(2 * time.Hour)
	require.NoError(t, base.Chtimes("/m.mp4", moved, moved))
	shifted, err := base.Stat("/m.mp4")
	require.NoError(t, err)
	assert.False(t, id.matches(base, "/m.mp4", shifted), "a modtime change proves a rewrite at equal size")

	dirInfo, err := base.Stat("/")
	require.NoError(t, err)
	assert.False(t, id.matches(base, "/m.mp4", dirInfo), "a non-regular replacement never matches")

	// A captured dev/inode leg degrades to size+modtime when the current
	// lookup exposes none (mixed real/wrapper filesystem postures).
	fabricated := artifactSourceIdentity{known: true, hasDevIno: true, dev: 1, ino: 2, size: grown.Size(), modTime: grown.ModTime()}
	assert.True(t, fabricated.matches(base, "/m.mp4", grown))
	assert.False(t, artifactSourceIdentity{}.matches(base, "/m.mp4", grown), "unknown identity matches nothing")
}

func TestArtifactSourceIdentityOsFsRenameSwapChangesInode(t *testing.T) {
	base := afero.NewOsFs()
	dir := t.TempDir()
	path := filepath.Join(dir, "m.mp4")
	require.NoError(t, afero.WriteFile(base, path, []byte("video"), 0o644))
	info, err := base.Stat(path)
	require.NoError(t, err)
	id := captureArtifactSourceIdentity(base, path, info)
	require.True(t, id.known)
	if !id.hasDevIno {
		t.Skip("platform exposes no dev/inode identity")
	}
	restat, err := base.Stat(path)
	require.NoError(t, err)
	assert.True(t, id.matches(base, path, restat), "a quiet file keeps its identity")

	// Replace-then-restore: same size, mtime forced back to the admitted
	// value — the surviving difference must be the inode.
	swapReplace(t, base, path, []byte("VIDEO"), info)
	swapped, err := base.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, id.size, swapped.Size())
	assert.True(t, info.ModTime().Equal(swapped.ModTime()), "size and mtime restored")
	assert.False(t, id.matches(base, path, swapped), "inode leg alone still pins the swap")
}

func TestRevalidateAdmittedSourceSkipsUnknownIdentity(t *testing.T) {
	stage := &artifactStage{fs: &pr260StatFailureFs{Fs: afero.NewMemMapFs(), path: "never"}}
	assert.NoError(t, stage.revalidateAdmittedSource("/anything", artifactSourceIdentity{}),
		"a path admission never pinned keeps existing plan semantics")
}

func TestRevalidateAdmittedSourceStatFaultRefuses(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/m.mp4", []byte("video"), 0o644))
	info, err := base.Stat("/m.mp4")
	require.NoError(t, err)
	id := captureArtifactSourceIdentity(base, "/m.mp4", info)
	stage := &artifactStage{fs: &pr260StatFailureFs{Fs: base, path: "/m.mp4"}}
	err = stage.revalidateAdmittedSource("/m.mp4", id)
	require.ErrorIs(t, err, errArtifactSourceChanged)
	require.ErrorContains(t, err, "stat denied")
}

type identityProbeExecutor struct {
	moves []models.SubtitleMove
}

func (identityProbeExecutor) PlanOrganize(context.Context, organizer.OrganizeCmd) (*organizer.OrganizePlan, error) {
	return nil, errors.New("unused")
}
func (identityProbeExecutor) PlanSourceExists(*organizer.OrganizePlan) bool { return true }
func (identityProbeExecutor) ExecuteOrganizePlan(*organizer.OrganizePlan, bool, organizer.LinkMode) (*organizer.OrganizeResult, error) {
	return nil, errors.New("unused")
}
func (e identityProbeExecutor) PlanSubtitleMoves(*organizer.OrganizePlan) []models.SubtitleMove {
	return e.moves
}

func TestRevalidateDirectSourcesDispatch(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/real/m.mp4", []byte("video"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/real/m.srt", []byte("subtitle"), 0o644))
	require.NoError(t, afero.WriteFile(base, "/real/late.srt", []byte("late"), 0o644))
	videoInfo, err := base.Stat("/real/m.mp4")
	require.NoError(t, err)
	subInfo, err := base.Stat("/real/m.srt")
	require.NoError(t, err)
	stage := &artifactStage{
		fs:             base,
		sourcePath:     "/real/m.mp4",
		sourceIdentity: captureArtifactSourceIdentity(base, "/real/m.mp4", videoInfo),
		siblings: []artifactSibling{
			{sourcePath: "/real/m.srt", stagedPath: "/stage/.source/m.srt", identity: captureArtifactSourceIdentity(base, "/real/m.srt", subInfo)},
		},
	}
	moves := []models.SubtitleMove{{OriginalPath: "/real/m.srt", NewPath: "/out/m.srt"}}

	// Staged-video plans never touch the real source: the video leg skips and
	// subtitle endpoints unknown at admission pass through untouched.
	stagedPlan := &organizer.OrganizePlan{SourcePath: "/stage/.source/m.mp4"}
	probe := identityProbeExecutor{moves: append([]models.SubtitleMove{}, moves...)}
	probe.moves = append(probe.moves, models.SubtitleMove{OriginalPath: "/real/late.srt", NewPath: "/out/late.srt"})
	require.NoError(t, stage.revalidateDirectSources(probe, stagedPlan))

	// The deferred plan addresses the real source: drift there refuses.
	realPlan := &organizer.OrganizePlan{SourcePath: "/real/m.mp4"}
	require.NoError(t, afero.WriteFile(base, "/real/m.mp4", []byte("video!"), 0o644))
	err = stage.revalidateDirectSources(identityProbeExecutor{moves: moves}, realPlan)
	require.ErrorIs(t, err, errArtifactSourceChanged)

	// Video intact, admitted subtitle drifted: refused through the sibling leg.
	require.NoError(t, afero.WriteFile(base, "/real/m.mp4", []byte("video"), 0o644))
	videoRestat, err := base.Stat("/real/m.mp4")
	require.NoError(t, err)
	stage.sourceIdentity = captureArtifactSourceIdentity(base, "/real/m.mp4", videoRestat)
	require.NoError(t, afero.WriteFile(base, "/real/m.srt", []byte("subtitle-drifted"), 0o644))
	err = stage.revalidateDirectSources(identityProbeExecutor{moves: moves}, realPlan)
	require.ErrorIs(t, err, errArtifactSourceChanged)
	require.ErrorContains(t, err, "/real/m.srt")

	// Everything as admitted: the dispatch passes.
	require.NoError(t, afero.WriteFile(base, "/real/m.srt", []byte("subtitle"), 0o644))
	restat, err := base.Stat("/real/m.mp4")
	require.NoError(t, err)
	stage.sourceIdentity = captureArtifactSourceIdentity(base, "/real/m.mp4", restat)
	subRestat, err := base.Stat("/real/m.srt")
	require.NoError(t, err)
	stage.siblings[0].identity = captureArtifactSourceIdentity(base, "/real/m.srt", subRestat)
	require.NoError(t, stage.revalidateDirectSources(identityProbeExecutor{moves: moves}, realPlan))
}

func journalCounts(l *completeCallFaultLog) (completes, reconciles int32) {
	return atomic.LoadInt32(&l.calls), atomic.LoadInt32(&l.deleteReconciles)
}

// A source replaced between preparation and the deferred publication must
// abort the fenced publish: the foreign bytes stay untouched, no completion
// journal lands, and the rollback markers read exactly like any other
// pre-consumption publication failure.
func TestDeferredMovePublishAbortsWhenSourceReplaced(t *testing.T) {
	type mutation struct {
		name           string
		requiresDevIno bool
		mutate         func(t *testing.T, fs afero.Fs, source string, admitted os.FileInfo)
		foreign        string
	}
	for _, m := range []mutation{
		{
			name:           "rename swap with restored size and mtime",
			requiresDevIno: true,
			mutate: func(t *testing.T, fs afero.Fs, source string, admitted os.FileInfo) {
				swapReplace(t, fs, source, []byte("VIDEO"), admitted)
			},
			foreign: "VIDEO",
		},
		{
			name: "in-place rewrite with different size",
			mutate: func(t *testing.T, fs afero.Fs, source string, _ os.FileInfo) {
				require.NoError(t, afero.WriteFile(fs, source, []byte("a much longer replacement payload"), 0o644))
			},
			foreign: "a much longer replacement payload",
		},
		{
			name: "in-place rewrite with shifted mtime",
			mutate: func(t *testing.T, fs afero.Fs, source string, admitted os.FileInfo) {
				require.NoError(t, afero.WriteFile(fs, source, []byte("VIDEO"), 0o644))
				shifted := admitted.ModTime().Add(2 * time.Hour)
				require.NoError(t, fs.Chtimes(source, shifted, shifted))
			},
			foreign: "VIDEO",
		},
	} {
		t.Run(m.name, func(t *testing.T) {
			db, _ := pr260ArtifactDB(t)
			movie := pr260FencedMovie(t, db, "deferred-source-replaced", "")
			base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-source-replaced")
			dest := filepath.Join(root, "library")
			admitted, statErr := base.Stat(source)
			require.NoError(t, statErr)
			if m.requiresDevIno && !captureArtifactSourceIdentity(base, source, admitted).hasDevIno {
				t.Skip("platform exposes no dev/inode identity: a same-size, same-mtime rename swap is indistinguishable from the admitted file there")
			}
			org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
			ledger := &completeCallFaultLog{}
			orch := &applyOrchImpl{fs: base, organizer: org, revertLog: ledger}
			cmd := pr260ArtifactFailureCommand(&movie, match, dest)
			cmd.Organize.Skip = false
			cmd.Organize.MoveFiles = true
			cmd.Download = false
			stage, _, err := orch.prepareArtifact(context.Background(), cmd)
			require.NoError(t, err)
			defer stage.cleanup()

			m.mutate(t, base, source, admitted)

			stagedPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
			require.NoError(t, planErr)
			state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
			publishErr := stage.publish(context.Background(), orch, state, nil)
			require.ErrorIs(t, publishErr, errArtifactSourceChanged)
			require.ErrorContains(t, publishErr, filepath.Base(source))
			assert.False(t, stage.sourceCleanupArmed, "marker state matches any pre-consumption failure")
			assert.False(t, stage.directOriginArmed)
			completes, reconciles := journalCounts(ledger)
			assert.Zero(t, completes, "the completion journal path never ran")
			assert.Zero(t, reconciles)
			pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
			pr260AssertNoFinals(t, base, dest)
			got, readErr := afero.ReadFile(base, source)
			require.NoError(t, readErr)
			assert.Equal(t, m.foreign, string(got), "foreign replacement bytes are never touched")
		})
	}
}

// Deferred copy mode consumes the same real source: the same refusal pin
// applies before any copy/link leg reads it.
func TestDeferredCopyPublishAbortsWhenSourceReplaced(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "deferred-copy-source-replaced", "")
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-copy-source-replaced")
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: org, revertLog: ledger}
	cmd := pr260ArtifactFailureCommand(&movie, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = false
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	require.NoError(t, afero.WriteFile(base, source, []byte("replacement video"), 0o644))

	stagedPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: false, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorIs(t, publishErr, errArtifactSourceChanged)
	completes, reconciles := journalCounts(ledger)
	assert.Zero(t, completes)
	assert.Zero(t, reconciles, "intent reconciliation waits for a confirmed publish")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	pr260AssertNoFinals(t, base, dest)
}

// An admitted subtitle replaced inside the window aborts the publication even
// though the video itself is untouched: its bytes were never admitted.
func TestDeferredMovePublishAbortsWhenSubtitleReplaced(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "deferred-subtitle-replaced", "")
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-subtitle-replaced")
	dest := filepath.Join(root, "library")
	admitted, statErr := base.Stat(subtitle)
	require.NoError(t, statErr)
	if !captureArtifactSourceIdentity(base, subtitle, admitted).hasDevIno {
		t.Skip("platform exposes no dev/inode identity: a same-size, same-mtime rename swap is indistinguishable from the admitted file there")
	}
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: org, revertLog: ledger}
	cmd := pr260ArtifactFailureCommand(&movie, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	swapReplace(t, base, subtitle, []byte("SUBTITLE"), admitted)

	stagedPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorIs(t, publishErr, errArtifactSourceChanged)
	require.ErrorContains(t, publishErr, filepath.Base(subtitle))
	completes, _ := journalCounts(ledger)
	assert.Zero(t, completes)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	pr260AssertNoFinals(t, base, dest)
}

// Identity preserved across the window: admission pinned dev/inode + size +
// mtime for the video and every sibling, and the deferred publication is the
// unchanged success path.
func TestDeferredMovePublishSucceedsWhenSourcesUntouched(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "deferred-sources-untouched", "")
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-sources-untouched")
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: org, revertLog: ledger}
	cmd := pr260ArtifactFailureCommand(&movie, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	require.True(t, stage.sourceIdentity.known, "admission pinned the video identity")
	switch runtime.GOOS {
	case "darwin", "dragonfly", "freebsd", "linux", "netbsd", "openbsd", "solaris":
		require.True(t, stage.sourceIdentity.hasDevIno, "OsFs admission carries dev/inode on POSIX Stat_t targets")
	case "windows":
		require.True(t, stage.sourceIdentity.hasDevIno, "OsFs admission carries the volume-serial+file-index handle identity on Windows")
	default:
		require.False(t, stage.sourceIdentity.hasDevIno, "no POSIX Stat_t or Windows handle identity on this target — admission keeps the size+mtime legs")
	}
	require.Len(t, stage.siblings, 2)
	for _, sibling := range stage.siblings {
		assert.True(t, sibling.identity.known, "admission pinned sibling %s", sibling.sourcePath)
	}

	stagedPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	require.NoError(t, stage.publish(context.Background(), orch, state, nil))
	for _, consumed := range []string{source, subtitle, multipart} {
		exists, existsErr := afero.Exists(base, consumed)
		require.NoError(t, existsErr)
		assert.False(t, exists, "unchanged sources moved out: %s", consumed)
	}
	exists, existsErr := afero.Exists(base, unrelated)
	require.NoError(t, existsErr)
	assert.True(t, exists)
	assert.NotZero(t, atomic.LoadInt32(&ledger.calls), "the success path still completes")
}

// The post-publish original-removal legs are direct source consumers too: a
// sibling video replaced inside the window (same size, mtime restored — only
// the inode differs) must refuse its removal and roll the publication back.
func TestDeferredMoveAbortsRemovingReplacedSiblingOriginal(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "deferred-sibling-replaced", "")
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-sibling-replaced")
	dest := filepath.Join(root, "library")
	admitted, statErr := base.Stat(multipart)
	require.NoError(t, statErr)
	if !captureArtifactSourceIdentity(base, multipart, admitted).hasDevIno {
		t.Skip("platform exposes no dev/inode identity: a same-size, same-mtime rename swap is indistinguishable from the admitted file there")
	}
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: org, revertLog: ledger}
	cmd := pr260ArtifactFailureCommand(&movie, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	swapReplace(t, base, multipart, []byte("PART-TWO"), admitted)

	stagedPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorIs(t, publishErr, errArtifactSourceChanged)
	assert.False(t, stage.sourceCleanupArmed, "rollback restored the video: markers reset")
	assert.False(t, stage.directOriginArmed)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	pr260AssertNoFinals(t, base, dest)
	got, readErr := afero.ReadFile(base, multipart)
	require.NoError(t, readErr)
	assert.Equal(t, "PART-TWO", string(got), "the foreign sibling replacement is never removed")
}

// In-place move mode consumes the original only after the staged copy is
// published: a source rewritten inside the window refuses that removal.
func TestInPlaceMoveAbortsRemovingReplacedOriginal(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "inplace-original-replaced", "")
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "inplace-original-replaced")
	isolatedDir := filepath.Join(root, "only-video")
	require.NoError(t, base.MkdirAll(isolatedDir, 0o755))
	standalone := filepath.Join(isolatedDir, movie.ID+".mp4")
	require.NoError(t, afero.WriteFile(base, standalone, []byte("isolated media"), 0o644))
	match.Path = standalone
	match.Name = filepath.Base(standalone)
	match.MovieID = movie.ID
	orch := pr260RealApply(base, &movie, organizer.MediaFormatConfig{}, nil, false)
	m, matchErr := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, matchErr)
	orch.organizer = organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "renamed-folder", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlace}, template.NewEngine(), m)
	cmd := pr260FencedCommand(&movie, match, isolatedDir, pr260FencedCounter(t, db), operationmode.OperationModeInPlace, false, true, organizer.LinkModeNone, false, false)
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.True(t, stage.inPlace)
	defer stage.cleanup()

	require.True(t, stage.sourceIdentity.known)
	require.NoError(t, afero.WriteFile(base, standalone, []byte("rewritten foreign bytes"), 0o644))

	state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource, InPlaceRenamed: true}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorIs(t, publishErr, errArtifactSourceChanged)
	got, readErr := afero.ReadFile(base, standalone)
	require.NoError(t, readErr)
	assert.Equal(t, "rewritten foreign bytes", string(got), "the replaced original survives untouched")
	regularFiles := []string{}
	walkErr := afero.Walk(base, root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && !strings.Contains(path, ".javinizer-apply-") {
			regularFiles = append(regularFiles, path)
		}
		return nil
	})
	require.NoError(t, walkErr)
	assert.ElementsMatch(t, []string{source, sub, part, other, standalone}, regularFiles,
		"rollback removed every published artifact; only the inputs remain")
}

// symlinkModeInfo reports the shape a no-follow lookup of a symlink returns:
// a non-regular entry carrying ModeSymlink. MemMapFs has no symlink model, so
// the memfs rejection legs model the entry mode directly.
type symlinkModeInfo struct{ os.FileInfo }

func (symlinkModeInfo) Mode() os.FileMode { return os.ModeSymlink | 0o777 }

// symlinkEntryLstatFs reports linkPath as a symlink directory entry through
// the no-follow lookup while the following Stat still resolves the underlying
// regular file — the rename-aside-then-symlink plant, on any platform.
type symlinkEntryLstatFs struct {
	afero.Fs
	linkPath string
}

func (f *symlinkEntryLstatFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if filepath.Clean(name) == filepath.Clean(f.linkPath) {
		info, err := f.Fs.Stat(name)
		if err != nil {
			return nil, false, err
		}
		return symlinkModeInfo{info}, true, nil
	}
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

// statOnlyArtifactFs hides the Lstater capability so the revalidation lookup
// exercises its Stat fallback leg (a filesystem without a symlink view).
type statOnlyArtifactFs struct{ afero.Fs }

// symlinkSwapAside renames the admitted file aside and plants a symlink at its
// pathname pointing back at the same inode — the codex shape: a following
// Stat still resolves the admitted bytes, only a no-follow lookup sees the
// foreign link object. Returns the aside path. Requires an OsFs-backed tree.
func symlinkSwapAside(t *testing.T, fs afero.Fs, path string) string {
	t.Helper()
	aside := path + ".symlink-aside"
	require.NoError(t, fs.Rename(path, aside))
	require.NoError(t, os.Symlink(aside, path))
	return aside
}

func pr260AssertSymlinkPreserved(t *testing.T, link, aside, asideContent string) {
	t.Helper()
	linkInfo, lerr := os.Lstat(link)
	require.NoError(t, lerr)
	assert.NotZero(t, linkInfo.Mode()&os.ModeSymlink, "the foreign symlink object is never followed, moved, or removed")
	got, readErr := os.ReadFile(aside)
	require.NoError(t, readErr)
	assert.Equal(t, asideContent, string(got), "the admitted bytes survive untouched under the aside name")
}

// The revalidation lookup is no-follow: a symlink planted at the admitted
// pathname is rejected even when its target still names the admitted file.
func TestRevalidateAdmittedSourceRejectsSymlinkDirectoryEntry(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/m.mp4", []byte("video"), 0o644))
	info, err := base.Stat("/m.mp4")
	require.NoError(t, err)
	id := captureArtifactSourceIdentity(base, "/m.mp4", info)
	stage := &artifactStage{fs: &symlinkEntryLstatFs{Fs: base, linkPath: "/m.mp4"}}
	err = stage.revalidateAdmittedSource("/m.mp4", id)
	require.ErrorIs(t, err, errArtifactSourceChanged,
		"a symlink directory entry at the admitted pathname never matches — its link object, not the target, would be moved")
	assert.False(t, captureArtifactSourceIdentity(base, "/m.mp4", symlinkModeInfo{info}).known,
		"admission never pins a symlink entry")
	assert.False(t, captureArtifactSourceIdentity(base, "/m.mp4", symlinkModeInfo{info}).matches(base, "/m.mp4", info),
		"a symlink-shaped pin matches nothing")
	got, readErr := afero.ReadFile(base, "/m.mp4")
	require.NoError(t, readErr)
	assert.Equal(t, "video", string(got), "the entry under the foreign alias is untouched")
}

// A filesystem without any symlink distinction (or one that hides the
// Lstater) falls back to Stat: MemMapFs itself has no symlink model, so the
// answer stays regular-or-absent and the happy path remains provable there.
func TestRevalidateAdmittedSourceStatFallbackWithoutLstater(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(base, "/m.mp4", []byte("video"), 0o644))
	info, err := base.Stat("/m.mp4")
	require.NoError(t, err)
	id := captureArtifactSourceIdentity(base, "/m.mp4", info)
	stage := &artifactStage{fs: statOnlyArtifactFs{base}}
	require.NoError(t, stage.revalidateAdmittedSource("/m.mp4", id),
		"no Lstater: Stat answers the lookup; memfs has no symlinks to hide")
	blocked := &artifactStage{fs: &pr260StatFailureFs{Fs: statOnlyArtifactFs{base}, path: "/m.mp4"}}
	err = blocked.revalidateAdmittedSource("/m.mp4", id)
	require.ErrorIs(t, err, errArtifactSourceChanged, "a lookup failure still fails closed on the fallback leg")
}

// The pre-execution gate: the admitted video renamed aside and replaced by a
// symlink to that same inode must abort the deferred move publication — a
// following Stat would still resolve the original bytes here.
func TestDeferredMovePublishAbortsWhenSourceSwappedForSymlink(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "deferred-source-symlinked", "")
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-source-symlinked")
	if !pr260LinkSupported(t, organizer.LinkModeSoft, source) {
		return
	}
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: org, revertLog: ledger}
	cmd := pr260ArtifactFailureCommand(&movie, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	aside := symlinkSwapAside(t, base, source)

	stagedPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorIs(t, publishErr, errArtifactSourceChanged)
	require.ErrorContains(t, publishErr, filepath.Base(source))
	assert.False(t, stage.sourceCleanupArmed, "marker state matches any pre-consumption failure")
	assert.False(t, stage.directOriginArmed)
	completes, reconciles := journalCounts(ledger)
	assert.Zero(t, completes, "the completion journal path never ran")
	assert.Zero(t, reconciles)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	pr260AssertNoFinals(t, base, dest)
	pr260AssertSymlinkPreserved(t, source, aside, "video")
}

// The post-publish original-removal gate: in-place move mode consumes the
// original only after the staged copy lands; a symlink swapped in admits no
// removal, and rollback restores the untouched state.
func TestInPlaceMoveAbortsRemovingSymlinkedOriginal(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "inplace-original-symlinked", "")
	base, root, source, sub, part, other, match := pr260FencedFiles(t, "inplace-original-symlinked")
	isolatedDir := filepath.Join(root, "only-video")
	require.NoError(t, base.MkdirAll(isolatedDir, 0o755))
	standalone := filepath.Join(isolatedDir, movie.ID+".mp4")
	require.NoError(t, afero.WriteFile(base, standalone, []byte("isolated media"), 0o644))
	if !pr260LinkSupported(t, organizer.LinkModeSoft, standalone) {
		return
	}
	match.Path = standalone
	match.Name = filepath.Base(standalone)
	match.MovieID = movie.ID
	orch := pr260RealApply(base, &movie, organizer.MediaFormatConfig{}, nil, false)
	m, matchErr := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, matchErr)
	orch.organizer = organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "renamed-folder", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlace}, template.NewEngine(), m)
	cmd := pr260FencedCommand(&movie, match, isolatedDir, pr260FencedCounter(t, db), operationmode.OperationModeInPlace, false, true, organizer.LinkModeNone, false, false)
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	require.True(t, stage.inPlace)
	defer stage.cleanup()

	require.True(t, stage.sourceIdentity.known)
	aside := symlinkSwapAside(t, base, standalone)

	state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource, InPlaceRenamed: true}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorIs(t, publishErr, errArtifactSourceChanged)
	pr260AssertSymlinkPreserved(t, standalone, aside, "isolated media")
	regularFiles := []string{}
	walkErr := afero.Walk(base, root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() && !strings.Contains(path, ".javinizer-apply-") {
			regularFiles = append(regularFiles, path)
		}
		return nil
	})
	require.NoError(t, walkErr)
	assert.ElementsMatch(t, []string{source, sub, part, other, aside}, regularFiles,
		"rollback removed every published artifact; only the inputs remain, and the symlink never counted as a payload")
}

// The sibling-original removal gate: a multipart sibling video renamed aside
// and replaced by a symlink to itself refuses its removal after publication.
func TestDeferredMoveAbortsRemovingSymlinkedSiblingOriginal(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "deferred-sibling-symlinked", "")
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "deferred-sibling-symlinked")
	if !pr260LinkSupported(t, organizer.LinkModeSoft, multipart) {
		return
	}
	dest := filepath.Join(root, "library")
	org := organizer.NewOrganizer(base, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, OperationMode: operationmode.OperationModeOrganize, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), nil)
	ledger := &completeCallFaultLog{}
	orch := &applyOrchImpl{fs: base, organizer: org, revertLog: ledger}
	cmd := pr260ArtifactFailureCommand(&movie, match, dest)
	cmd.Organize.Skip = false
	cmd.Organize.MoveFiles = true
	cmd.Download = false
	stage, _, err := orch.prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()

	aside := symlinkSwapAside(t, base, multipart)

	stagedPlan, planErr := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: models.FileMatchInfo{Path: stage.stagedSource, Name: filepath.Base(source)}, Movie: stage.original.Movie, DestDir: stage.root, MoveFiles: true, OperationMode: stage.original.OperationMode})
	require.NoError(t, planErr)
	state := &applyPipelineState{operationID: "op", organizeResult: &organizer.OrganizeResult{NewPath: stagedPlan.TargetPath, FolderPath: stagedPlan.TargetDir}}
	publishErr := stage.publish(context.Background(), orch, state, nil)
	require.ErrorIs(t, publishErr, errArtifactSourceChanged)
	assert.False(t, stage.sourceCleanupArmed, "rollback restored the video: markers reset")
	assert.False(t, stage.directOriginArmed)
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
	pr260AssertNoFinals(t, base, dest)
	pr260AssertSymlinkPreserved(t, multipart, aside, "part two")
}

func TestPrepareArtifactNeverAdmitsSymlinkSource(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "admit-symlink-source")
	if !pr260LinkSupported(t, organizer.LinkModeSoft, source) {
		return
	}
	aside := symlinkSwapAside(t, base, source)
	dest := filepath.Join(root, "published")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "admit-symlink-source"}, match, dest)
	cmd.Organize.Skip = false
	stage, _, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.ErrorContains(t, err, "non-regular source",
		"a symlinked video source is never admitted as a direct-publish source")
	assert.Nil(t, stage)
	pr260AssertSymlinkPreserved(t, source, aside, "video")
	pr260AssertRetained(t, base, aside, subtitle, multipart, unrelated)
	pr260AssertStageGone(t, base, root)
	pr260AssertNoFinals(t, base, dest)
}

// A symlinked sibling — even one pointing at a real regular file — is never
// pinned for direct publication; the regular siblings still admit.
func TestPrepareArtifactSkipsSymlinkedSibling(t *testing.T) {
	base, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, "admit-symlink-sibling")
	if !pr260LinkSupported(t, organizer.LinkModeSoft, subtitle) {
		return
	}
	aside := symlinkSwapAside(t, base, subtitle)
	dest := filepath.Join(root, "published")
	cmd := pr260ArtifactFailureCommand(&models.Movie{ContentID: "admit-symlink-sibling"}, match, dest)
	cmd.Organize.Skip = false
	stage, _, err := (&applyOrchImpl{fs: base}).prepareArtifact(context.Background(), cmd)
	require.NoError(t, err)
	defer stage.cleanup()
	require.True(t, stage.sourceIdentity.known, "the regular video still admits")
	for _, s := range stage.siblings {
		assert.NotEqual(t, subtitle, s.sourcePath, "a symlinked sibling is never pinned for direct publication")
	}
	require.Len(t, stage.siblings, 1, "only the regular multipart sibling admitted")
	pr260AssertSymlinkPreserved(t, subtitle, aside, "subtitle")
	pr260AssertRetained(t, base, source, subtitle, multipart, unrelated)
}
