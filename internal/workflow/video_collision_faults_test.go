package workflow

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type collisionFaultFS struct {
	afero.Fs
	lstat func(string) (os.FileInfo, bool, error)
	open  func(string) (afero.File, error)
}

func (f *collisionFaultFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	if f.lstat != nil {
		return f.lstat(path)
	}
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}

func (f *collisionFaultFS) Open(path string) (afero.File, error) {
	if f.open != nil {
		return f.open(path)
	}
	return f.Fs.Open(path)
}

type collisionFaultFile struct {
	afero.File
	stat func() (os.FileInfo, error)
	read func([]byte) (int, error)
}

func (f *collisionFaultFile) Stat() (os.FileInfo, error) {
	if f.stat != nil {
		return f.stat()
	}
	return f.File.Stat()
}

func (f *collisionFaultFile) Read(p []byte) (int, error) {
	if f.read != nil {
		return f.read(p)
	}
	return f.File.Read(p)
}

func collisionFixture(t *testing.T) (afero.Fs, string, string) {
	t.Helper()
	fs := afero.NewOsFs()
	dir := t.TempDir()
	source, target := filepath.Join(dir, "ABC-123.mp4"), filepath.Join(dir, "target.mp4")
	require.NoError(t, afero.WriteFile(fs, source, []byte("source video"), 0600))
	require.NoError(t, afero.WriteFile(fs, target, []byte("foreign video"), 0600))
	return fs, source, target
}

func assertCollisionFilesUnchanged(t *testing.T, fs afero.Fs, source, target string) {
	t.Helper()
	for path, want := range map[string]string{source: "source video", target: "foreign video"} {
		body, err := afero.ReadFile(fs, path)
		require.NoError(t, err)
		require.Equal(t, want, string(body))
	}
}

func TestVideoReviewSnapshotFaults(t *testing.T) {
	for _, fault := range []string{"open", "stat", "opened-identity", "read", "final-lstat", "final-identity"} {
		t.Run(fault, func(t *testing.T) {
			base, source, target := collisionFixture(t)
			fs := &collisionFaultFS{Fs: base}
			want := error(os.ErrPermission)
			fs.open = func(path string) (afero.File, error) {
				if fault == "open" {
					return nil, os.ErrPermission
				}
				file, err := base.Open(path)
				if err != nil {
					return nil, err
				}
				wrapped := &collisionFaultFile{File: file}
				switch fault {
				case "stat":
					wrapped.stat = func() (os.FileInfo, error) { return nil, os.ErrPermission }
				case "opened-identity":
					want = errArtifactSourceChanged
					wrapped.stat = func() (os.FileInfo, error) { return base.Stat(target) }
				case "read":
					wrapped.read = func([]byte) (int, error) { return 0, os.ErrPermission }
				}
				return wrapped, nil
			}
			calls := 0
			fs.lstat = func(path string) (os.FileInfo, bool, error) {
				calls++
				if calls == 2 {
					if fault == "final-lstat" {
						return nil, true, os.ErrPermission
					}
					if fault == "final-identity" {
						want = errArtifactSourceChanged
						return base.(afero.Lstater).LstatIfPossible(target)
					}
				}
				return base.(afero.Lstater).LstatIfPossible(path)
			}
			_, err := captureVideoReviewFile(t.Context(), fs, source)
			require.ErrorIs(t, err, want)
			assertCollisionFilesUnchanged(t, base, source, target)
		})
	}
}

func TestVideoReviewTokenRejectsMalformedAndUnavailableKey(t *testing.T) {
	for _, token := range []string{strings.Repeat("x", 32769), "missing-separator", "!.signature", base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".signature", base64.RawURLEncoding.EncodeToString([]byte("{}")) + ".forged"} {
		_, err := decodeVideoReview(token)
		require.Error(t, err)
	}
	old := artifactSweepSecretKey
	t.Cleanup(func() { artifactSweepSecretKey = old })
	artifactSweepSecretKey = func() ([]byte, error) { return nil, os.ErrPermission }
	_, err := signVideoReview(videoReviewReceipt{})
	require.ErrorIs(t, err, os.ErrPermission)
	_, err = decodeVideoReview(base64.RawURLEncoding.EncodeToString([]byte("{}")) + ".signature")
	require.ErrorIs(t, err, os.ErrPermission)
}

func TestInspectVideoCollisionFaults(t *testing.T) {
	for _, fault := range []string{"destination-lstat", "source-open", "destination-open", "weak-after-preflight", "key", "nonregular"} {
		t.Run(fault, func(t *testing.T) {
			base, source, target := collisionFixture(t)
			fs := &collisionFaultFS{Fs: base}
			old := artifactSweepSecretKey
			t.Cleanup(func() { artifactSweepSecretKey = old })
			if fault == "key" {
				artifactSweepSecretKey = func() ([]byte, error) { return nil, os.ErrPermission }
			}
			sourceLookups := 0
			fs.lstat = func(path string) (os.FileInfo, bool, error) {
				if path == target && fault == "destination-lstat" {
					return nil, true, os.ErrPermission
				}
				info, follow, err := base.(afero.Lstater).LstatIfPossible(path)
				if path == source {
					sourceLookups++
					if fault == "weak-after-preflight" && sourceLookups >= 4 && info != nil {
						info = weakIdentityInfo{info}
					}
				}
				if fault == "nonregular" && path == target && info != nil {
					info = symlinkModeInfo{info}
				}
				return info, follow, err
			}
			fs.open = func(path string) (afero.File, error) {
				if fault == "source-open" && path == source || fault == "destination-open" && path == target {
					return nil, os.ErrPermission
				}
				file, err := base.Open(path)
				if err != nil {
					return nil, err
				}
				if fault == "weak-after-preflight" && path == source {
					return &collisionFaultFile{File: file, stat: func() (os.FileInfo, error) { info, err := file.Stat(); return weakIdentityInfo{info}, err }}, nil
				}
				return file, nil
			}
			collision, err := inspectVideoCollision(t.Context(), fs, source, target, "scope")
			require.NotNil(t, collision)
			require.Empty(t, collision.ReviewToken)
			if fault == "weak-after-preflight" || fault == "nonregular" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, os.ErrPermission)
			}
			assertCollisionFilesUnchanged(t, base, source, target)
		})
	}
}

func TestVerifyVideoReviewCancellationAndWeakIdentity(t *testing.T) {
	for _, point := range []string{"before", "source", "destination", "weak"} {
		t.Run(point, func(t *testing.T) {
			base, source, target := collisionFixture(t)
			fs := afero.Fs(base)
			if point == "weak" {
				fs = &weakIdentityProbeFs{Fs: base, path: source}
			}
			src, err := captureVideoReviewFile(t.Context(), fs, source)
			require.NoError(t, err)
			dst, err := captureVideoReviewFile(t.Context(), fs, target)
			require.NoError(t, err)
			token, err := signVideoReview(videoReviewReceipt{Scope: "scope", Source: source, Destination: target, SourceFile: src, DestinationFile: dst})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if point == "before" {
				cancel()
			}
			faultFS := &collisionFaultFS{Fs: fs}
			faultFS.open = func(path string) (afero.File, error) {
				if point == "source" && path == source || point == "destination" && path == target {
					cancel()
					return nil, context.Canceled
				}
				return fs.Open(path)
			}
			decision := videocollision.Decision{Source: source, Destination: target, ReviewToken: token}
			err = verifyVideoReview(ctx, faultFS, decision, source, target, "scope", target)
			if point == "weak" {
				require.ErrorContains(t, err, "provable filesystem identities")
			} else {
				require.ErrorIs(t, err, context.Canceled)
			}
			assertCollisionFilesUnchanged(t, base, source, target)
		})
	}
}

func reviewedCollisionStage(t *testing.T) (*artifactStage, afero.Fs, string, string) {
	t.Helper()
	fs, source, target := collisionFixture(t)
	collision, err := inspectVideoCollision(t.Context(), fs, source, target, "scope")
	require.NoError(t, err)
	require.NotEmpty(t, collision.ReviewToken)
	stage := &artifactStage{fs: fs, sourcePath: source}
	stage.original.Organize.videoReviewTarget = target
	stage.original.Organize.videoReviewScope = "scope"
	stage.original.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{source: {Source: source, Destination: target, Policy: videocollision.Replace, ReviewToken: collision.ReviewToken}}
	return stage, fs, source, target
}

func TestReviewedPayloadFailsClosed(t *testing.T) {
	for _, fault := range []string{"invalid-token", "missing", "denied", "cancelled", "different-info", "different-bytes", "no-context"} {
		t.Run(fault, func(t *testing.T) {
			stage, base, source, target := reviewedCollisionStage(t)
			info, err := base.Stat(source)
			require.NoError(t, err)
			path := source
			switch fault {
			case "invalid-token":
				decision := stage.original.Organize.VideoCollisionDecisions[source]
				decision.ReviewToken = "invalid"
				stage.original.Organize.VideoCollisionDecisions[source] = decision
			case "missing":
				path += ".missing"
			case "denied":
				stage.fs = &collisionFaultFS{Fs: base, open: func(string) (afero.File, error) { return nil, os.ErrPermission }}
			case "cancelled":
				ctx, cancel := context.WithCancel(t.Context())
				stage.publishCtx = ctx
				stage.fs = &collisionFaultFS{Fs: base, open: func(string) (afero.File, error) { cancel(); return nil, context.Canceled }}
			case "different-info":
				info, err = base.Stat(target)
				require.NoError(t, err)
			case "different-bytes":
				path = target
			case "no-context":
			}
			err = stage.verifyReviewedPayload(path, info)
			if fault == "no-context" {
				require.NoError(t, err)
			} else if fault == "cancelled" {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				var conflict *videocollision.ConflictError
				require.ErrorAs(t, err, &conflict)
				require.Equal(t, videoCollisionStaleCode, conflict.Code)
			}
			assertCollisionFilesUnchanged(t, base, source, target)
		})
	}
}

func TestRefreshedVideoCollisionPreservesPublicationFailure(t *testing.T) {
	for _, fault := range []string{"reviewed-inspect-error", "unreviewed-inspect-error", "unchanged", "unreviewed-new", "reviewed-missing", "unreviewed-missing"} {
		t.Run(fault, func(t *testing.T) {
			stage, base, source, target := reviewedCollisionStage(t)
			publicationErr := errors.New("publication refused")
			if strings.HasPrefix(fault, "unreviewed") {
				stage.original.Organize.VideoCollisionDecisions = nil
			}
			if strings.HasSuffix(fault, "inspect-error") {
				stage.fs = &collisionFaultFS{Fs: base, lstat: func(path string) (os.FileInfo, bool, error) {
					if path == target {
						return nil, true, os.ErrPermission
					}
					return base.(afero.Lstater).LstatIfPossible(path)
				}}
			}
			if strings.HasSuffix(fault, "missing") {
				require.NoError(t, base.Remove(target))
			}
			err := stage.refreshedVideoCollision(t.Context(), publicationErr)
			require.ErrorIs(t, err, publicationErr)
			var conflict *videocollision.ConflictError
			switch fault {
			case "reviewed-inspect-error", "reviewed-missing":
				require.ErrorAs(t, err, &conflict)
				require.Equal(t, videoCollisionStaleCode, conflict.Code)
			case "unreviewed-new":
				require.ErrorAs(t, err, &conflict)
				require.Equal(t, "VIDEO_COLLISION_REFRESHED", conflict.Code)
				require.NotEmpty(t, conflict.Collision.ReviewToken)
			default:
				require.False(t, errors.As(err, &conflict))
			}
			body, readErr := afero.ReadFile(base, source)
			require.NoError(t, readErr)
			require.Equal(t, "source video", string(body))
		})
	}
}

func TestReviewedVideoAtOriginalPathCannotBecomeResident(t *testing.T) {
	stage, base, source, target := reviewedCollisionStage(t)
	info, err := base.Stat(source)
	require.NoError(t, err)
	stage.sourceIdentity = captureArtifactSourceIdentity(base, source, info)
	resident, err := stage.stagedVideoResident(source, source, true)
	require.False(t, resident)
	var conflict *videocollision.ConflictError
	require.ErrorAs(t, err, &conflict)
	require.Equal(t, videoCollisionStaleCode, conflict.Code)
	require.Equal(t, videoCollisionKindSameFile, conflict.Collision.Kind)
	after, err := base.Stat(source)
	require.NoError(t, err)
	require.True(t, os.SameFile(info, after))
	assertCollisionFilesUnchanged(t, base, source, target)
}

func TestVideoCollisionSkipDoesNotPrimeDestinationClaim(t *testing.T) {
	env := setupCopyIntentE2E(t, "issue277-skip-priming")
	cmd, plan := issue277Command(t, env, true)
	cmd.Organize.VideoCollisionDecisions = map[string]videocollision.Decision{env.source: {Source: env.source, Destination: plan.TargetPath, Policy: videocollision.Skip}}
	priming, err := env.orch.planDuplicatePriming(t.Context(), cmd)
	require.NoError(t, err)
	require.Empty(t, priming.SourcePath)
	require.Empty(t, priming.TargetPath)
	require.False(t, priming.WillMove)
	require.NoFileExists(t, plan.TargetPath)
	body, err := os.ReadFile(env.source)
	require.NoError(t, err)
	require.Equal(t, "video", string(body))
}

func TestStagedVideoResidentSiblingRequiresOwnAdmission(t *testing.T) {
	base, source, target := collisionFixture(t)
	stage := &artifactStage{fs: base, sourcePath: source}
	resident, err := stage.stagedVideoResident(target, source, false)
	require.NoError(t, err)
	require.False(t, resident)
	assertCollisionFilesUnchanged(t, base, source, target)
}

func TestVideoCollisionCannotReviewDirectoryOrWeakIdentity(t *testing.T) {
	for _, mode := range []string{"directory", "weak-identity"} {
		t.Run(mode, func(t *testing.T) {
			base, source, target := collisionFixture(t)
			fs := afero.Fs(base)
			if mode == "directory" {
				require.NoError(t, base.Remove(target))
				require.NoError(t, base.Mkdir(target, 0700))
			} else {
				fs = &weakIdentityProbeFs{Fs: base, path: source}
			}
			collision, err := inspectVideoCollision(t.Context(), fs, source, target, "scope")
			require.NoError(t, err)
			require.NotNil(t, collision)
			require.Empty(t, collision.ReviewToken)
			if mode == "directory" {
				require.Equal(t, "directory", collision.Kind)
			} else {
				require.Equal(t, videoCollisionKindFile, collision.Kind)
			}
			body, err := afero.ReadFile(base, source)
			require.NoError(t, err)
			require.Equal(t, "source video", string(body))
		})
	}
}
