package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type stagedSubtitleTestLog struct {
	noOpRevertLog
	recordErr, sealErr error
	entries            []models.DeleteEntry
	sealed             bool
}

func (l *stagedSubtitleTestLog) RecordDeleteIntent(_ context.Context, _ OperationID, entries []models.DeleteEntry) error {
	l.entries = append(l.entries, entries...)
	return l.recordErr
}
func (l *stagedSubtitleTestLog) FinalizeDeleteIntentCopyDigest(_ context.Context, _ OperationID, _ string, digest string) error {
	l.sealed = true
	return l.sealErr
}

type stagedSubtitleFaultFS struct {
	afero.Fs
	lstatPath          string
	lstatErr, mkdirErr error
}

func (f *stagedSubtitleFaultFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	if path == f.lstatPath {
		return nil, true, f.lstatErr
	}
	if fs, ok := f.Fs.(afero.Lstater); ok {
		return fs.LstatIfPossible(path)
	}
	info, err := f.Fs.Stat(path)
	return info, false, err
}
func (f *stagedSubtitleFaultFS) MkdirAll(path string, mode os.FileMode) error {
	if f.mkdirErr != nil {
		return f.mkdirErr
	}
	return f.Fs.MkdirAll(path, mode)
}

func stagedSubtitleFixture(t *testing.T) (*artifactStage, *applyOrchImpl, *organizer.OrganizeResult, *stagedSubtitleTestLog, string, string) {
	t.Helper()
	fs := afero.NewOsFs()
	root := t.TempDir()
	stageDir := filepath.Join(root, "stage")
	targetDir := filepath.Join(root, "out")
	require.NoError(t, fs.MkdirAll(stageDir, 0755))
	source := filepath.Join(stageDir, "movie.eng.srt")
	target := filepath.Join(targetDir, "movie.eng.srt")
	require.NoError(t, afero.WriteFile(fs, source, []byte("subtitle"), 0640))
	batch, err := downloader.NewReplacementBatch(fs, "1", nil)
	require.NoError(t, err)
	stage := &artifactStage{fs: fs, root: stageDir, finalRoot: targetDir, inPlace: true, publishBatch: batch, stagedSubtitleOutputs: map[string]int{source: 0}, stagedSubtitleResults: []organizer.SubtitleResult{{SubtitleMove: models.SubtitleMove{OriginalPath: filepath.Join(root, "original.en.srt"), NewPath: target}, Skipped: true}}}
	log := &stagedSubtitleTestLog{}
	orch := &applyOrchImpl{fs: fs, revertLog: log}
	t.Cleanup(func() { _ = batch.Rollback(context.Background()) })
	return stage, orch, &organizer.OrganizeResult{}, log, source, target
}

func TestStagedSubtitlePublisherBoundaries(t *testing.T) {
	failure := errors.New("injected subtitle failure")
	for _, name := range []string{"success", "no recorder", "occupied", "target stat", "source stat", "source directory", "mkdir", "arm failure", "arm occupied", "partial digest", "source drift", "record intent", "copy collision", "copy error", "copy completed", "copy source proof", "observe failure", "observe successor", "confirm failure", "confirm successor", "confirm absent", "bind absent then successor", "seal failure"} {
		t.Run(name, func(t *testing.T) {
			s, o, result, log, source, target := stagedSubtitleFixture(t)
			oldCopy, oldPartial, oldBefore, oldConfirm, oldObserve := stagedSubtitleCopy, stagedSubtitlePartialDigest, stagedSubtitleBeforePublish, stagedSubtitleConfirmPublish, stagedSubtitleBindPublish
			t.Cleanup(func() {
				stagedSubtitleCopy = oldCopy
				stagedSubtitlePartialDigest = oldPartial
				stagedSubtitleBeforePublish = oldBefore
				stagedSubtitleConfirmPublish = oldConfirm
				stagedSubtitleBindPublish = oldObserve
			})
			wantErr := true
			wantInstalled := false
			switch name {
			case "success":
				wantErr = false
				wantInstalled = true
			case "no recorder":
				o.revertLog = nil
				wantErr = false
				wantInstalled = true
			case "occupied":
				require.NoError(t, s.fs.MkdirAll(filepath.Dir(target), 0755))
				require.NoError(t, afero.WriteFile(s.fs, target, []byte("foreign"), 0644))
				wantErr = false
			case "target stat":
				s.fs = &stagedSubtitleFaultFS{Fs: s.fs, lstatPath: target, lstatErr: failure}
			case "source stat":
				s.fs = &stagedSubtitleFaultFS{Fs: s.fs, lstatPath: source, lstatErr: failure}
			case "source directory":
				require.NoError(t, s.fs.Remove(source))
				require.NoError(t, s.fs.Mkdir(source, 0755))
			case "mkdir":
				s.fs = &stagedSubtitleFaultFS{Fs: s.fs, mkdirErr: failure}
			case "arm failure":
				stagedSubtitleBeforePublish = func(*downloader.ReplacementBatch, context.Context, string, bool) (bool, error) { return false, failure }
			case "arm occupied":
				stagedSubtitleBeforePublish = func(_ *downloader.ReplacementBatch, _ context.Context, path string, replace bool) (bool, error) {
					require.False(t, replace)
					require.NoError(t, afero.WriteFile(s.fs, path, []byte("foreign"), 0644))
					return false, failure
				}
				wantErr = false
			case "partial digest":
				stagedSubtitlePartialDigest = func(afero.Fs, string) (os.FileInfo, string, error) { return nil, "", failure }
			case "source drift":
				stagedSubtitlePartialDigest = func(fs afero.Fs, path string) (os.FileInfo, string, error) {
					require.NoError(t, afero.WriteFile(fs, path, []byte("drifted subtitle content"), 0644))
					return oldPartial(fs, path)
				}
			case "record intent":
				log.recordErr = failure
			case "copy collision":
				stagedSubtitleCopy = func(fs afero.Fs, src, dst string, proof fsutil.VerifiedSourceProof, mode os.FileMode) (string, *fsutil.BoundInstallIdentity, error) {
					require.NoError(t, afero.WriteFile(fs, dst, []byte("foreign"), 0644))
					return oldCopy(fs, src, dst, proof, mode)
				}
				wantErr = false
			case "copy error":
				stagedSubtitleCopy = func(afero.Fs, string, string, fsutil.VerifiedSourceProof, os.FileMode) (string, *fsutil.BoundInstallIdentity, error) {
					return "", nil, failure
				}
			case "copy completed":
				stagedSubtitleCopy = func(fs afero.Fs, src, dst string, proof fsutil.VerifiedSourceProof, mode os.FileMode) (string, *fsutil.BoundInstallIdentity, error) {
					digest, id, err := oldCopy(fs, src, dst, proof, mode)
					require.NoError(t, err)
					return digest, id, fsutil.ErrPublishCompleted
				}
			case "copy source proof":
				stagedSubtitleCopy = func(fs afero.Fs, src, dst string, proof fsutil.VerifiedSourceProof, mode os.FileMode) (string, *fsutil.BoundInstallIdentity, error) {
					require.NoError(t, afero.WriteFile(fs, src, []byte("drifted source"), 0644))
					info, err := fs.Stat(src)
					require.NoError(t, err)
					return "", nil, proof(src, info)
				}
			case "observe failure":
				stagedSubtitleBindPublish = func(*downloader.ReplacementBatch, string, *fsutil.BoundInstallIdentity) error { return failure }
			case "observe successor":
				stagedSubtitleCopy = func(fs afero.Fs, src, dst string, proof fsutil.VerifiedSourceProof, mode os.FileMode) (string, *fsutil.BoundInstallIdentity, error) {
					digest, id, err := oldCopy(fs, src, dst, proof, mode)
					require.NoError(t, err)
					require.NoError(t, fs.Rename(dst, dst+".ours"))
					require.NoError(t, afero.WriteFile(fs, dst, []byte("subtitle"), 0644))
					return digest, id, nil
				}
				wantErr = false
			case "confirm failure":
				stagedSubtitleConfirmPublish = func(*downloader.ReplacementBatch, context.Context, string) error { return failure }
			case "confirm successor":
				stagedSubtitleConfirmPublish = func(batch *downloader.ReplacementBatch, ctx context.Context, path string) error {
					require.NoError(t, oldConfirm(batch, ctx, path))
					require.NoError(t, s.fs.Rename(path, path+".ours"))
					require.NoError(t, afero.WriteFile(s.fs, path, []byte("subtitle"), 0644))
					return nil
				}
			case "confirm absent":
				stagedSubtitleConfirmPublish = func(batch *downloader.ReplacementBatch, ctx context.Context, path string) error {
					require.NoError(t, oldConfirm(batch, ctx, path))
					require.NoError(t, s.fs.Remove(path))
					return nil
				}
			case "bind absent then successor":
				stagedSubtitleBindPublish = func(batch *downloader.ReplacementBatch, path string, installed *fsutil.BoundInstallIdentity) error {
					require.NoError(t, s.fs.Rename(path, path+".ours"))
					bindErr := oldObserve(batch, path, installed)
					require.Error(t, bindErr)
					require.NoError(t, afero.WriteFile(s.fs, path, []byte("subtitle"), 0644))
					return bindErr
				}
			case "seal failure":
				log.sealErr = failure
			}
			err := s.publishStagedSubtitles(context.Background(), o, "1", result)
			if wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, wantInstalled, result.Subtitles[0].Copied)
				require.Equal(t, !wantInstalled, result.Subtitles[0].Skipped)
			}
			require.False(t, result.Subtitles[0].Moved)
			if wantInstalled {
				got, e := afero.ReadFile(s.fs, target)
				require.NoError(t, e)
				require.Equal(t, "subtitle", string(got))
				require.NotNil(t, result.Subtitles[0].InstalledIdentity)
			}
			if name == "copy completed" {
				require.NoError(t, s.publishBatch.Rollback(context.Background()))
				require.NoFileExists(t, target)
				require.FileExists(t, source)
			}
			if name == "confirm successor" || name == "bind absent then successor" {
				require.False(t, log.sealed)
				rollbackErr := s.publishBatch.Rollback(context.Background())
				if name == "confirm successor" {
					require.Error(t, rollbackErr)
				} else {
					require.NoError(t, rollbackErr)
				}
				got, e := afero.ReadFile(s.fs, target)
				require.NoError(t, e)
				require.Equal(t, "subtitle", string(got))
			}
			if name == "observe successor" {
				require.True(t, result.Subtitles[0].SuccessorRefused)
				require.False(t, log.sealed)
				require.NoError(t, s.publishBatch.Rollback(context.Background()))
				got, e := afero.ReadFile(s.fs, target)
				require.NoError(t, e)
				require.Equal(t, "subtitle", string(got))
			}
			if name == "occupied" || name == "arm occupied" || name == "copy collision" {
				got, e := afero.ReadFile(s.fs, target)
				require.NoError(t, e)
				require.Equal(t, "foreign", string(got))
				require.False(t, log.sealed)
			}
			if len(log.entries) > 0 {
				require.Empty(t, log.entries[0].SHA256)
				require.NotEmpty(t, log.entries[0].CopyPartialSHA256)
			}
		})
	}
	s, o, result, _, _, _ := stagedSubtitleFixture(t)
	s.publishBatch = nil
	require.Error(t, s.publishStagedSubtitles(context.Background(), o, "1", result))
	s.stagedSubtitleOutputs = nil
	require.NoError(t, s.publishStagedSubtitles(context.Background(), o, "1", nil))
}

func TestCollectStagedSubtitleOutputs(t *testing.T) {
	s, _, _, _, source, target := stagedSubtitleFixture(t)
	original := filepath.Join(filepath.Dir(s.root), "original.en.srt")
	input := filepath.Join(s.root, "original.en.srt")
	s.siblings = []artifactSibling{{sourcePath: original, stagedPath: input, subtitle: true}}
	result := &organizer.OrganizeResult{Subtitles: []organizer.SubtitleResult{{SubtitleMove: models.SubtitleMove{OriginalPath: input, NewPath: source, Copied: true}}, {Skipped: true}}}
	require.NoError(t, s.collectStagedSubtitleOutputs(result))
	require.Equal(t, original, s.stagedSubtitleResults[0].OriginalPath)
	require.Equal(t, target, s.stagedSubtitleResults[0].NewPath)
	require.True(t, s.isStagedSubtitleOutput(source))
	require.False(t, s.stagedSubtitleResults[0].Copied)
	result.Subtitles[0].NewPath = filepath.Join(filepath.Dir(s.root), "outside.srt")
	require.ErrorContains(t, s.collectStagedSubtitleOutputs(result), "escapes staging")
	result.Subtitles[0].NewPath = source
	result.Subtitles[0].OriginalPath = "unknown"
	require.ErrorContains(t, s.collectStagedSubtitleOutputs(result), "no admitted source")
	s.videoDeferred = true
	require.NoError(t, s.collectStagedSubtitleOutputs(result))
	s.videoDeferred = false
	s.inPlace = false
	require.NoError(t, s.collectStagedSubtitleOutputs(result))
	s.inPlace = true
	require.NoError(t, s.collectStagedSubtitleOutputs(nil))
}

func TestStagedSubtitleCanonicalOccupantPreserved(t *testing.T) {
	for _, kind := range []string{"file", "dangling symlink", "late file", "vacant"} {
		t.Run(kind, func(t *testing.T) {
			env := setupCopyIntentE2E(t, "canonical-"+kind)
			old := env.subtitle
			env.subtitle = old[:len(old)-len(".srt")] + ".en.srt"
			require.NoError(t, os.Rename(old, env.subtitle))
			incoming := filepath.Dir(env.source)
			target := filepath.Join(incoming, env.movie.ID+".eng.srt")
			org := organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "movie", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
			env.orch.organizer = org
			plant := func() { require.NoError(t, os.WriteFile(target, []byte("foreign subtitle"), 0644)) }
			switch kind {
			case "file":
				plant()
			case "dangling symlink":
				if !pr260LinkSupported(t, organizer.LinkModeSoft, env.source) {
					return
				}
				require.NoError(t, os.Symlink(filepath.Join(incoming, "missing"), target))
			case "late file":
				env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: org, preExecute: func(*organizer.OrganizePlan) { plant() }}
			}
			cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
			cmd.Organize.ForceUpdate = false
			result, err := env.orch.Execute(t.Context(), cmd)
			require.NoError(t, err)
			require.Len(t, result.OrganizeResult.Subtitles, 1)
			sr := result.OrganizeResult.Subtitles[0]
			require.Equal(t, env.subtitle, sr.OriginalPath)
			require.Equal(t, target, sr.NewPath)
			require.Equal(t, kind == "vacant", sr.Copied)
			require.Equal(t, kind != "vacant", sr.Skipped)
			require.FileExists(t, env.source)
			require.FileExists(t, env.subtitle)
			if kind != "dangling symlink" {
				got, e := os.ReadFile(target)
				require.NoError(t, e)
				if kind == "vacant" {
					require.Equal(t, "subtitle", string(got))
				} else {
					require.Equal(t, "foreign subtitle", string(got))
				}
			}
			ledger := p3Ledger(t, env.repo, result.OperationID)
			if kind != "vacant" {
				require.NotContains(t, ledger.Delete, target)
				for _, pending := range ledger.PlannedDeletes {
					require.NotEqual(t, target, pending.Path)
				}
			}
			reverter := history.NewReverter(env.fs, env.repo)
			_, err = reverter.RevertBatch(context.Background(), env.jobID)
			require.NoError(t, err)
			if kind == "vacant" {
				require.NoFileExists(t, target)
			} else {
				_, e := os.Lstat(target)
				require.NoError(t, e)
			}
		})
	}
}

type escapingStagedSubtitleOrganizer struct {
	*organizer.Organizer
	outside string
}

func (o *escapingStagedSubtitleOrganizer) Organize(ctx context.Context, cmd organizer.OrganizeCmd) (*organizer.OrganizeResult, error) {
	result, err := o.Organizer.Organize(ctx, cmd)
	if err == nil && len(result.Subtitles) > 0 {
		result.Subtitles[0].NewPath = o.outside
	}
	return result, err
}

func TestStagedSubtitleFailureLeavesOriginals(t *testing.T) {
	for _, escape := range []bool{false, true} {
		name := "publish failure"
		if escape {
			name = "escaped staging output"
		}
		t.Run(name, func(t *testing.T) {
			env := setupCopyIntentE2E(t, "canonical-failure-"+name)
			old := env.subtitle
			env.subtitle = old[:len(old)-len(".srt")] + ".en.srt"
			require.NoError(t, os.Rename(old, env.subtitle))
			incoming := filepath.Dir(env.source)
			target := filepath.Join(incoming, env.movie.ID+".eng.srt")
			org := organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "movie", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
			env.orch.organizer = org
			if escape {
				env.orch.organizer = &escapingStagedSubtitleOrganizer{Organizer: org, outside: filepath.Join(filepath.Dir(incoming), "outside.srt")}
			} else {
				oldCopy := stagedSubtitleCopy
				t.Cleanup(func() { stagedSubtitleCopy = oldCopy })
				stagedSubtitleCopy = func(afero.Fs, string, string, fsutil.VerifiedSourceProof, os.FileMode) (string, *fsutil.BoundInstallIdentity, error) {
					return "", nil, os.ErrPermission
				}
			}
			cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
			cmd.Organize.ForceUpdate = false
			_, err := env.orch.Execute(t.Context(), cmd)
			require.Error(t, err)
			if escape {
				require.ErrorContains(t, err, "escapes staging")
			} else {
				require.ErrorIs(t, err, os.ErrPermission)
			}
			pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
			require.NoFileExists(t, target)
			require.NoFileExists(t, filepath.Join(incoming, env.movie.ID+".mp4"))
			_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
			require.NoError(t, err)
			pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
		})
	}
}

func TestStagedSubtitleIdenticalForeignRaceSurvivesUndo(t *testing.T) {
	for _, afterCopy := range []bool{false, true} {
		name := "after arm"
		if afterCopy {
			name = "after copy"
		}
		t.Run(name, func(t *testing.T) {
			env := setupCopyIntentE2E(t, "canonical-race-"+name)
			old := env.subtitle
			env.subtitle = old[:len(old)-len(".srt")] + ".en.srt"
			require.NoError(t, os.Rename(old, env.subtitle))
			incoming := filepath.Dir(env.source)
			target := filepath.Join(incoming, env.movie.ID+".eng.srt")
			env.orch.organizer = organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "movie", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
			oldCopy := stagedSubtitleCopy
			t.Cleanup(func() { stagedSubtitleCopy = oldCopy })
			stagedSubtitleCopy = func(fs afero.Fs, source, dest string, proof fsutil.VerifiedSourceProof, mode os.FileMode) (string, *fsutil.BoundInstallIdentity, error) {
				if afterCopy {
					digest, id, err := oldCopy(fs, source, dest, proof, mode)
					require.NoError(t, err)
					require.NoError(t, fs.Rename(dest, dest+".previous"))
					require.NoError(t, afero.WriteFile(fs, dest, []byte("subtitle"), 0644))
					return digest, id, nil
				}
				require.NoError(t, afero.WriteFile(fs, dest, []byte("subtitle"), 0644))
				return oldCopy(fs, source, dest, proof, mode)
			}
			cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
			cmd.Organize.ForceUpdate = false
			result, err := env.orch.Execute(t.Context(), cmd)
			require.NoError(t, err)
			require.Len(t, result.OrganizeResult.Subtitles, 1)
			require.True(t, result.OrganizeResult.Subtitles[0].Skipped)
			require.False(t, result.OrganizeResult.Subtitles[0].Copied)
			ledger := p3Ledger(t, env.repo, result.OperationID)
			require.NotContains(t, ledger.Delete, target)
			found := false
			for _, pin := range ledger.PlannedDeletes {
				if pin.Path == target {
					found = true
					require.Empty(t, pin.SHA256)
					require.NotEmpty(t, pin.CopyPartialSHA256)
				}
			}
			require.True(t, found, "the raced copy has only an unsealed, non-deleting intent")
			_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
			require.NoError(t, err)
			got, err := os.ReadFile(target)
			require.NoError(t, err)
			require.Equal(t, "subtitle", string(got), "same-byte foreign inode is never owned by the failed copy")
			pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
		})
	}
}

func TestCollectStagedSubtitleInputsExhaustive(t *testing.T) {
	for _, outcome := range []string{"self skip", "different source skip", "error", "missing result"} {
		t.Run(outcome, func(t *testing.T) {
			s, o, _, _, source, target := stagedSubtitleFixture(t)
			original := filepath.Join(filepath.Dir(s.root), "movie.eng.srt")
			s.siblings = []artifactSibling{{sourcePath: original, stagedPath: source, subtitle: true}, {stagedPath: filepath.Join(s.root, "video.mp4")}, {subtitle: true}}
			sr := organizer.SubtitleResult{SubtitleMove: models.SubtitleMove{OriginalPath: source, NewPath: source}, Skipped: true}
			switch outcome {
			case "different source skip":
				sr.NewPath = filepath.Join(s.root, "other.eng.srt")
			case "error":
				sr.Skipped = false
				sr.Error = os.ErrPermission
			}
			result := &organizer.OrganizeResult{Subtitles: []organizer.SubtitleResult{sr}}
			if outcome == "missing result" {
				result.Subtitles = nil
			}
			require.NoError(t, s.collectStagedSubtitleOutputs(result))
			require.True(t, s.excludedStagedSubtitleInputs[source])
			require.Len(t, s.excludedStagedSubtitleInputs, 1)
			require.Equal(t, outcome == "self skip", s.isStagedSubtitleOutput(source))
			final := &organizer.OrganizeResult{}
			require.NoError(t, s.publishStagedSubtitles(t.Context(), o, "1", final))
			if outcome != "missing result" {
				require.Equal(t, original, final.Subtitles[0].OriginalPath)
				require.Equal(t, outcome == "self skip", final.Subtitles[0].Copied)
			}
			if outcome == "self skip" {
				require.Equal(t, target, final.Subtitles[0].NewPath)
				require.FileExists(t, target)
			} else {
				require.NoFileExists(t, target)
			}
		})
	}
}

func TestStagedSubtitleSelfPathFinalOwnership(t *testing.T) {
	for _, extension := range []string{".srt", ".SRT", ".ttml"} {
		for _, kind := range []string{"vacant", "late occupant", "resident"} {
			t.Run(extension+"/"+kind, func(t *testing.T) {
				env := setupCopyIntentE2E(t, "self-subtitle")
				incoming := filepath.Dir(env.source)
				require.NoError(t, os.Remove(env.multipart))
				require.NoError(t, os.Remove(env.unrelated))
				newSource := filepath.Join(incoming, env.movie.ID+".mp4")
				require.NoError(t, os.Rename(env.source, newSource))
				env.source, env.match.Path, env.match.Name = newSource, newSource, filepath.Base(newSource)
				newSub := filepath.Join(incoming, env.movie.ID+".eng"+extension)
				require.NoError(t, os.Rename(env.subtitle, newSub))
				env.subtitle = newSub
				mode := operationmode.OperationModeInPlace
				finalDir := filepath.Join(filepath.Dir(incoming), "renamed")
				if kind == "resident" {
					mode, finalDir = operationmode.OperationModeInPlaceNoRenameFolder, incoming
				}
				target := filepath.Join(finalDir, env.movie.ID+".eng"+extension)
				m, err := matcher.NewMatcher(&matcher.Config{})
				require.NoError(t, err)
				org := organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "renamed", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{strings.ToLower(extension)}, OperationMode: mode}, template.NewEngine(), m)
				env.orch.organizer = org
				fired := false
				if kind == "late occupant" {
					env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: org, preExecute: func(*organizer.OrganizePlan) {
						fired = true
						require.NoError(t, os.MkdirAll(finalDir, 0755))
						require.NoError(t, os.WriteFile(target, []byte("foreign subtitle"), 0644))
					}}
				}
				cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), mode, false, false, organizer.LinkModeNone, false, false)
				cmd.Organize.ForceUpdate = false
				result, err := env.orch.Execute(t.Context(), cmd)
				require.NoError(t, err)
				require.Equal(t, finalDir, filepath.Dir(result.OrganizeResult.NewPath))
				require.Equal(t, kind == "late occupant", fired)
				require.Len(t, result.OrganizeResult.Subtitles, 1)
				sr := result.OrganizeResult.Subtitles[0]
				require.Equal(t, env.subtitle, sr.OriginalPath)
				require.Equal(t, target, sr.NewPath)
				require.Equal(t, kind == "vacant", sr.Copied)
				require.Equal(t, kind != "vacant", sr.Skipped)
				want := "subtitle"
				if kind == "late occupant" {
					want = "foreign subtitle"
				}
				got, err := os.ReadFile(target)
				require.NoError(t, err)
				require.Equal(t, want, string(got))
				ledger := p3Ledger(t, env.repo, result.OperationID)
				if kind != "vacant" {
					require.NotContains(t, ledger.Delete, target)
					for _, pin := range ledger.PlannedDeletes {
						require.NotEqual(t, target, pin.Path)
					}
				}
				_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
				require.NoError(t, err)
				require.FileExists(t, env.source)
				require.FileExists(t, env.subtitle)
				if kind == "vacant" {
					require.NoFileExists(t, target)
				} else {
					got, err = os.ReadFile(target)
					require.NoError(t, err)
					require.Equal(t, want, string(got))
				}
			})
		}
	}
}

type stagedSubtitleInputOpenFailureFS struct {
	afero.Fs
	fired     bool
	directory bool
}

func (f *stagedSubtitleInputOpenFailureFS) Open(path string) (afero.File, error) {
	if !f.fired && strings.Contains(path, artifactStageDirPrefix) && ((f.directory && strings.HasPrefix(filepath.Base(path), artifactStageDirPrefix)) || (!f.directory && strings.HasSuffix(path, ".en.srt"))) {
		f.fired = true
		return nil, os.ErrPermission
	}
	return f.Fs.Open(path)
}

func TestStagedSubtitleErrorCannotPublishFallback(t *testing.T) {
	env := setupCopyIntentE2E(t, "input-error")
	old := env.subtitle
	env.subtitle = strings.TrimSuffix(old, ".srt") + ".en.srt"
	require.NoError(t, os.Rename(old, env.subtitle))
	incoming := filepath.Dir(env.source)
	fallback := filepath.Join(incoming, env.movie.ID+".en.srt")
	require.NoError(t, os.WriteFile(fallback, []byte("foreign alternate"), 0644))
	fs := &stagedSubtitleInputOpenFailureFS{Fs: env.fs}
	env.orch.fs = fs
	env.orch.organizer = organizer.NewOrganizer(fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
	cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
	cmd.Organize.ForceUpdate = false
	result, err := env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	require.True(t, fs.fired)
	require.Len(t, result.OrganizeResult.Subtitles, 1)
	sr := result.OrganizeResult.Subtitles[0]
	require.ErrorIs(t, sr.Error, os.ErrPermission)
	require.False(t, sr.Copied)
	require.Equal(t, env.subtitle, sr.OriginalPath)
	require.Equal(t, filepath.Join(incoming, env.movie.ID+".eng.srt"), sr.NewPath)
	_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, err)
	got, err := os.ReadFile(fallback)
	require.NoError(t, err)
	require.Equal(t, "foreign alternate", string(got))
	pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
}

func TestStagedSubtitleCannotRetryIntoGenericTree(t *testing.T) {
	for _, directory := range []bool{false, true} {
		name := "file error"
		if directory {
			name = "omitted seats"
		}
		t.Run(name, func(t *testing.T) {
			env := setupCopyIntentE2E(t, "canonical-input-error")
			incoming := filepath.Dir(env.source)
			source := filepath.Join(incoming, env.movie.ID+".mp4")
			require.NoError(t, os.Rename(env.source, source))
			env.source, env.match.Path, env.match.Name = source, source, filepath.Base(source)
			subtitle := filepath.Join(incoming, env.movie.ID+".en.srt")
			require.NoError(t, os.Rename(env.subtitle, subtitle))
			env.subtitle = subtitle
			target := filepath.Join(incoming, env.movie.ID+".eng.srt")
			fs := &stagedSubtitleInputOpenFailureFS{Fs: env.fs, directory: directory}
			env.orch.fs = fs
			org := organizer.NewOrganizer(fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
			fired := false
			env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: org, preExecute: func(*organizer.OrganizePlan) {
				fired = true
				require.NoError(t, os.WriteFile(target, []byte("foreign canonical"), 0644))
			}}
			cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
			cmd.Organize.ForceUpdate = false
			result, err := env.orch.Execute(t.Context(), cmd)
			require.NoError(t, err)
			require.True(t, fs.fired && fired)
			got, err := os.ReadFile(target)
			require.NoError(t, err)
			require.Equal(t, "foreign canonical", string(got))
			if directory {
				require.Empty(t, result.OrganizeResult.Subtitles, "a second probe must not introduce staged-only planned results")
			} else {
				require.Len(t, result.OrganizeResult.Subtitles, 1)
				require.ErrorIs(t, result.OrganizeResult.Subtitles[0].Error, os.ErrPermission)
				require.False(t, result.OrganizeResult.Subtitles[0].Copied)
			}
			_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
			require.NoError(t, err)
			got, err = os.ReadFile(target)
			require.NoError(t, err)
			require.Equal(t, "foreign canonical", string(got))
			pr260AssertRetained(t, env.fs, env.source, env.subtitle, env.multipart, env.unrelated)
		})
	}
}
