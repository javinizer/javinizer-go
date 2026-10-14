package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type stagedVideoTestLog struct {
	noOpRevertLog
	recordErr, sealErr error
	entries            []models.DeleteEntry
	sealed             string
}

func (l *stagedVideoTestLog) RecordDeleteIntent(_ context.Context, _ OperationID, entries []models.DeleteEntry) error {
	l.entries = append(l.entries, entries...)
	return l.recordErr
}

func (l *stagedVideoTestLog) FinalizeDeleteIntentCopyDigest(_ context.Context, _ OperationID, _, digest string) error {
	l.sealed = digest
	return l.sealErr
}

func stagedVideoFixture(t *testing.T) (*artifactStage, *stagedVideoTestLog, string, string) {
	t.Helper()
	fs := afero.NewOsFs()
	root := t.TempDir()
	stageDir, targetDir := filepath.Join(root, "stage"), filepath.Join(root, "out")
	require.NoError(t, fs.MkdirAll(stageDir, 0755))
	require.NoError(t, fs.MkdirAll(targetDir, 0755))
	source, target := filepath.Join(stageDir, "movie.mp4"), filepath.Join(targetDir, "movie.mp4")
	require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0640))
	log := &stagedVideoTestLog{}
	batch, err := downloader.NewReplacementBatch(fs, "1", log)
	require.NoError(t, err)
	s := &artifactStage{fs: fs, root: stageDir, finalRoot: targetDir, publishBatch: batch, publishCtx: context.Background(), publishVideoLog: log, publishVideoOpID: "1"}
	t.Cleanup(func() { _ = batch.Rollback(context.Background()) })
	return s, log, source, target
}

func TestStagedVideoPublisherBoundaries(t *testing.T) {
	failure := errors.New("injected staged-video failure")
	for _, name := range []string{"success", "no recorder", "no operation", "no batch", "source missing", "source directory", "occupied", "authorized replacement", "arm failure", "partial digest", "pin drift", "intent failure", "digest failure", "move collision", "move failure", "move completed", "move successor", "source proof", "bind failure", "confirm failure", "confirm successor", "confirm vacant", "seal failure"} {
		t.Run(name, func(t *testing.T) {
			s, log, source, target := stagedVideoFixture(t)
			oldMove, oldPartial, oldDigest := stagedVideoMove, stagedVideoPartialDigest, stagedVideoDigest
			oldBefore, oldBind, oldConfirm := stagedVideoBeforePublish, stagedVideoBindPublish, stagedVideoConfirmPublish
			t.Cleanup(func() {
				stagedVideoMove, stagedVideoPartialDigest, stagedVideoDigest = oldMove, oldPartial, oldDigest
				stagedVideoBeforePublish, stagedVideoBindPublish, stagedVideoConfirmPublish = oldBefore, oldBind, oldConfirm
			})
			wantErr, replace := true, false
			switch name {
			case "success":
				wantErr = false
			case "no recorder":
				s.publishVideoLog = nil
				wantErr = false
			case "no operation":
				s.publishVideoOpID = ""
				wantErr = false
			case "no batch":
				s.publishBatch = nil
			case "source missing":
				require.NoError(t, s.fs.Remove(source))
			case "source directory":
				require.NoError(t, s.fs.Remove(source))
				require.NoError(t, s.fs.Mkdir(source, 0755))
			case "occupied", "authorized replacement":
				require.NoError(t, afero.WriteFile(s.fs, target, []byte("foreign"), 0644))
				replace = name == "authorized replacement"
				wantErr = !replace
			case "arm failure":
				stagedVideoBeforePublish = func(*downloader.ReplacementBatch, context.Context, string, bool) (bool, error) { return false, failure }
			case "partial digest":
				stagedVideoPartialDigest = func(afero.Fs, string) (os.FileInfo, string, error) { return nil, "", failure }
			case "pin drift":
				stagedVideoPartialDigest = func(fs afero.Fs, path string) (os.FileInfo, string, error) {
					require.NoError(t, afero.WriteFile(fs, path, []byte("changed staged source"), 0640))
					return oldPartial(fs, path)
				}
			case "intent failure":
				log.recordErr = failure
			case "digest failure":
				stagedVideoDigest = func(afero.Fs, string) (string, error) { return "", failure }
			case "move collision":
				stagedVideoMove = func(fs afero.Fs, src, dst string, proof fsutil.VerifiedSourceProof, mode os.FileMode) (*fsutil.BoundInstallIdentity, error) {
					require.NoError(t, afero.WriteFile(fs, dst, []byte("foreign"), 0644))
					return oldMove(fs, src, dst, proof, mode)
				}
			case "move failure":
				stagedVideoMove = func(afero.Fs, string, string, fsutil.VerifiedSourceProof, os.FileMode) (*fsutil.BoundInstallIdentity, error) {
					return nil, failure
				}
			case "move completed", "move successor":
				stagedVideoMove = func(fs afero.Fs, src, dst string, proof fsutil.VerifiedSourceProof, mode os.FileMode) (*fsutil.BoundInstallIdentity, error) {
					id, err := oldMove(fs, src, dst, proof, mode)
					require.NoError(t, err)
					if name == "move successor" {
						require.NoError(t, fs.Rename(dst, dst+".ours"))
						require.NoError(t, afero.WriteFile(fs, dst, []byte("foreign"), 0644))
						return id, errors.Join(fsutil.ErrPublishCompleted, fsutil.ErrPublishSuccessorUnproven)
					}
					return id, fsutil.ErrPublishCompleted
				}
			case "source proof":
				stagedVideoMove = func(fs afero.Fs, src, dst string, proof fsutil.VerifiedSourceProof, mode os.FileMode) (*fsutil.BoundInstallIdentity, error) {
					require.NoError(t, afero.WriteFile(fs, src, []byte("foreign source"), 0644))
					return oldMove(fs, src, dst, proof, mode)
				}
			case "bind failure":
				stagedVideoBindPublish = func(*downloader.ReplacementBatch, string, string, *fsutil.BoundInstallIdentity) error { return failure }
			case "confirm failure":
				stagedVideoConfirmPublish = func(*downloader.ReplacementBatch, context.Context, string) error { return failure }
			case "confirm successor", "confirm vacant":
				stagedVideoConfirmPublish = func(b *downloader.ReplacementBatch, ctx context.Context, path string) error {
					require.NoError(t, oldConfirm(b, ctx, path))
					require.NoError(t, s.fs.Rename(path, path+".ours"))
					if name == "confirm successor" {
						require.NoError(t, afero.WriteFile(s.fs, path, []byte("video"), 0644))
					}
					return nil
				}
			case "seal failure":
				log.sealErr = failure
			}
			err := s.publishStagedVideo(source, target, replace)
			if wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				body, readErr := afero.ReadFile(s.fs, target)
				require.NoError(t, readErr)
				require.Equal(t, "video", string(body))
				require.NoFileExists(t, source)
			}
			if name == "success" {
				require.Len(t, log.sealed, 64)
				require.NoError(t, s.publishBatch.Rollback(context.Background()))
				require.NoFileExists(t, target)
			}
			if name == "authorized replacement" {
				require.Empty(t, log.entries)
				require.NoError(t, s.publishBatch.Rollback(context.Background()))
				body, readErr := afero.ReadFile(s.fs, target)
				require.NoError(t, readErr)
				require.Equal(t, "foreign", string(body))
			}
			if name == "move completed" || name == "confirm failure" || name == "seal failure" {
				require.NoError(t, s.publishBatch.Rollback(context.Background()))
				require.NoFileExists(t, target)
			}
			if name == "occupied" || name == "move collision" || name == "move successor" || name == "confirm successor" {
				require.Empty(t, log.sealed)
				_ = s.publishBatch.Rollback(context.Background())
				body, readErr := afero.ReadFile(s.fs, target)
				require.NoError(t, readErr)
				want := "foreign"
				if name == "confirm successor" {
					want = "video"
				}
				require.Equal(t, want, string(body))
			}
			if len(log.entries) != 0 {
				require.Empty(t, log.entries[0].SHA256)
				require.NotEmpty(t, log.entries[0].CopyPartialSHA256)
			}
		})
	}
}

func TestRegisterStagedVideoOutputsUsesAdmittedPaths(t *testing.T) {
	root := t.TempDir()
	s := &artifactStage{stagedSource: filepath.Join(root, ".source", "original.mp4"), siblings: []artifactSibling{
		{sourcePath: "/incoming/original-cd2.mp4", stagedPath: filepath.Join(root, ".source", "original-cd2.mp4")},
		{sourcePath: "/incoming/original.en.srt", stagedPath: filepath.Join(root, ".source", "original.en.srt")},
		{sourcePath: "/incoming/original-cd3.mp4", deferred: true},
		{sourcePath: "/incoming/original-cd4.mp4"},
	}}
	primary := filepath.Join(root, "movie", "ID.mp4")
	s.registerStagedVideoOutputs(primary, primary)
	require.Equal(t, map[string]bool{primary: true, filepath.Join(root, "movie", "ID-cd2.mp4"): false}, s.stagedVideoOutputs)
	require.NotContains(t, s.stagedVideoOutputs, filepath.Join(root, "movie", "trailer.mp4"))
	s.original.Organize.LinkMode = organizer.LinkModeHard
	s.registerStagedVideoOutputs(primary, "")
	require.Equal(t, map[string]bool{s.siblings[0].stagedPath: false}, s.stagedVideoOutputs)
	s.original.Organize.LinkMode = organizer.LinkModeNone
	s.registerStagedVideoOutputs("", "")
	require.Equal(t, map[string]bool{s.siblings[0].stagedPath: false}, s.stagedVideoOutputs)
	s.stagedSource = ""
	s.registerStagedVideoOutputs(primary, "")
	require.Equal(t, map[string]bool{s.siblings[0].stagedPath: false}, s.stagedVideoOutputs)
}
