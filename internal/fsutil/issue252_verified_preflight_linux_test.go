//go:build linux

package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type issue252VerifiedOperation struct {
	name string
	run  func(afero.Fs, string, string, VerifiedSourceProof) error
}

func issue252VerifiedOperations() []issue252VerifiedOperation {
	return []issue252VerifiedOperation{
		{"copy", CopyFileNoReplaceVerified},
		{"copy-mode", func(fs afero.Fs, src, dst string, p VerifiedSourceProof) error {
			_, err := CopyFileNoReplaceVerifiedMode(fs, src, dst, p, 0o640)
			return err
		}},
		{"copy-install", func(fs afero.Fs, src, dst string, p VerifiedSourceProof) error {
			_, err := CopyFileNoReplaceVerifiedInstall(fs, src, dst, p)
			return err
		}},
		{"copy-digest", func(fs afero.Fs, src, dst string, p VerifiedSourceProof) error {
			_, err := CopyFileNoReplaceVerifiedDigest(fs, src, dst, p)
			return err
		}},
		{"copy-digest-mode", func(fs afero.Fs, src, dst string, p VerifiedSourceProof) error {
			_, _, err := CopyFileNoReplaceVerifiedDigestMode(fs, src, dst, p, 0o640)
			return err
		}},
		{"cross-device-move", MoveFileNoReplaceVerified},
		{"cross-device-move-mode", func(fs afero.Fs, src, dst string, p VerifiedSourceProof) error {
			_, err := MoveFileNoReplaceVerifiedMode(fs, src, dst, p, 0o640)
			return err
		}},
	}
}

// Replay issue #252's exact rename/link refusal on an owned synthetic
// destination. No NAS, media library, network mount, or large payload is used.
func TestIssue252VerifiedStreamsProbeBeforePayload(t *testing.T) {
	for _, tc := range issue252VerifiedOperations() {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			src, dst := filepath.Join(root, "source.mp4"), filepath.Join(root, "nas", "movie.mp4")
			payload := []byte(strings.Repeat("synthetic-media-", 64))
			require.NoError(t, os.WriteFile(src, payload, 0o640))
			admitted, err := os.Stat(src)
			require.NoError(t, err)
			proof := func(_ string, info os.FileInfo) error { require.True(t, os.SameFile(admitted, info)); return nil }
			oldRename, oldLink := renameNoReplaceKernel, publishNoReplaceLink
			t.Cleanup(func() { renameNoReplaceKernel, publishNoReplaceLink = oldRename, oldLink })
			probes, stagedBytes := 0, int64(0)
			renameNoReplaceKernel = func(s, d string) error {
				if filepath.Dir(s) != filepath.Dir(dst) {
					if d == dst {
						return syscall.EXDEV
					}
					return oldRename(s, d)
				}
				if strings.HasPrefix(filepath.Base(s), ".nrprobe.") {
					probes++
				} else if info, err := os.Stat(s); err == nil && info.Mode().IsRegular() {
					if info.Size() > stagedBytes {
						stagedBytes = info.Size()
					}
				}
				return syscall.EINVAL
			}
			publishNoReplaceLink = func(s, d string) error {
				if filepath.Dir(s) == filepath.Dir(dst) {
					return syscall.EPERM
				}
				return oldLink(s, d)
			}
			for attempt := 0; attempt < 2; attempt++ {
				err = tc.run(afero.NewOsFs(), src, dst, proof)
				require.ErrorIs(t, err, ErrPublishNoReplaceUnsupported)
			}
			require.ErrorIs(t, err, ErrPublishNoReplaceUnsupported)
			require.NotErrorIs(t, err, syscall.EINVAL)
			require.False(t, PublishCompleted(err))
			got, readErr := os.ReadFile(src)
			require.NoError(t, readErr)
			require.Equal(t, payload, got)
			require.NoFileExists(t, dst)
			entries, readErr := os.ReadDir(filepath.Dir(dst))
			require.NoError(t, readErr)
			require.Empty(t, entries, "the zero-byte probe is cleaned; no payload staging was created")
			t.Logf("probes=%d staged_payload_bytes=%d error=%v", probes, stagedBytes, err)
			require.Equal(t, int64(0), stagedBytes, "incapability must be discovered before payload streaming")
			require.Equal(t, 1, probes, "verified streams must use the existing destination preflight")
		})
	}
}

func TestIssue252VerifiedPreflightPreservesEarlyCollision(t *testing.T) {
	for _, tc := range issue252VerifiedOperations() {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			src, dst := filepath.Join(root, "source"), filepath.Join(root, "target")
			require.NoError(t, os.WriteFile(src, []byte("source"), 0o600))
			require.NoError(t, os.WriteFile(dst, []byte("foreign"), 0o600))
			stubNoClobberProbe(t, func(afero.Fs, string, string) error {
				t.Fatal("occupied targets must refuse before preflight")
				return nil
			})
			err := tc.run(afero.NewOsFs(), src, dst, func(string, os.FileInfo) error {
				t.Fatal("occupied targets must refuse before source admission")
				return nil
			})
			require.ErrorIs(t, err, ErrPublishCollision)
			got, err := os.ReadFile(dst)
			require.NoError(t, err)
			require.Equal(t, "foreign", string(got))
			got, err = os.ReadFile(src)
			require.NoError(t, err)
			require.Equal(t, "source", string(got))
		})
	}
}

func TestIssue252VerifiedPositiveProbeStillChecksPublication(t *testing.T) {
	for _, tc := range issue252VerifiedOperations() {
		for _, collision := range []bool{false, true} {
			name := "late-io-error"
			if collision {
				name = "late-collision"
			}
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				root := t.TempDir()
				src, dst := filepath.Join(root, "source"), filepath.Join(root, "nas", "target")
				require.NoError(t, os.WriteFile(src, []byte("source"), 0o600))
				oldRename := renameNoReplaceKernel
				t.Cleanup(func() { renameNoReplaceKernel = oldRename })
				probes, publishes := 0, 0
				renameNoReplaceKernel = func(s, d string) error {
					if filepath.Dir(s) != filepath.Dir(dst) {
						if d == dst {
							return syscall.EXDEV
						}
						return oldRename(s, d)
					}
					if strings.HasPrefix(filepath.Base(s), ".nrprobe.") {
						if strings.HasSuffix(d, ".published") {
							probes++
							err := oldRename(s, d)
							require.NoError(t, err)
							if collision {
								require.NoError(t, os.WriteFile(dst, []byte("foreign"), 0o600))
							}
							return err
						}
						return oldRename(s, d)
					}
					if d == dst {
						publishes++
						if collision {
							return oldRename(s, d)
						}
						return syscall.EIO
					}
					return oldRename(s, d)
				}
				err := tc.run(afero.NewOsFs(), src, dst, func(string, os.FileInfo) error { return nil })
				if collision {
					require.ErrorIs(t, err, ErrPublishCollision)
					got, readErr := os.ReadFile(dst)
					require.NoError(t, readErr)
					require.Equal(t, "foreign", string(got))
				} else {
					require.ErrorIs(t, err, syscall.EIO)
					require.NoFileExists(t, dst)
				}
				require.False(t, PublishCompleted(err))
				require.Equal(t, 1, probes)
				require.Equal(t, 1, publishes, "a positive preflight cannot replace the real final publish")
				got, readErr := os.ReadFile(src)
				require.NoError(t, readErr)
				require.Equal(t, "source", string(got))
			})
		}
	}
}

func TestIssue252VerifiedPreflightKeepsForeignProbeSubstitution(t *testing.T) {
	for _, tc := range issue252VerifiedOperations() {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			src, dst := filepath.Join(root, "source"), filepath.Join(root, "nas", "target")
			require.NoError(t, os.WriteFile(src, []byte("source"), 0o600))
			oldRename := renameNoReplaceKernel
			t.Cleanup(func() { renameNoReplaceKernel = oldRename })
			renameNoReplaceKernel = func(s, d string) error {
				if filepath.Dir(s) != filepath.Dir(dst) && d == dst {
					return syscall.EXDEV
				}
				return oldRename(s, d)
			}
			var replaced string
			stubNoClobberProbe(t, func(_ afero.Fs, s, _ string) error {
				replaced = s
				require.NoError(t, os.Rename(s, s+".retained"))
				require.NoError(t, os.WriteFile(s, []byte("foreign probe successor"), 0o600))
				return fmt.Errorf("%w: %w", ErrPublishNoReplaceUnsupported, syscall.EPERM)
			})
			err := tc.run(afero.NewOsFs(), src, dst, func(string, os.FileInfo) error { return nil })
			require.ErrorIs(t, err, ErrPublishNoReplaceUnsupported)
			require.NotEmpty(t, replaced)
			got, readErr := os.ReadFile(replaced)
			require.NoError(t, readErr)
			require.Equal(t, "foreign probe successor", string(got), "cleanup must not delete a substituted probe entry")
			got, readErr = os.ReadFile(src)
			require.NoError(t, readErr)
			require.Equal(t, "source", string(got))
			require.NoFileExists(t, dst)
		})
	}
}

func TestIssue252VerifiedMoveProbeFailureRestorationCollision(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "source"), filepath.Join(root, "nas", "target")
	require.NoError(t, os.WriteFile(src, []byte("admitted source"), 0o600))
	oldRename := renameNoReplaceKernel
	t.Cleanup(func() { renameNoReplaceKernel = oldRename })
	var claim string
	renameNoReplaceKernel = func(s, d string) error {
		if filepath.Dir(s) != filepath.Dir(dst) && d == dst {
			claim = s
			return syscall.EXDEV
		}
		return oldRename(s, d)
	}
	stubNoClobberProbe(t, func(_ afero.Fs, s, d string) error {
		require.NoFileExists(t, src, "verified move has claimed the admitted source before EXDEV detection")
		require.NoError(t, os.WriteFile(src, []byte("foreign source successor"), 0o600))
		return fmt.Errorf("%w: %w", ErrPublishNoReplaceUnsupported, syscall.EPERM)
	})
	err := MoveFileNoReplaceVerified(afero.NewOsFs(), src, dst, func(string, os.FileInfo) error { return nil })
	require.ErrorIs(t, err, ErrPublishNoReplaceUnsupported)
	require.ErrorIs(t, err, ErrTakeAsideRestoreFailed)
	require.False(t, PublishCompleted(err))
	got, readErr := os.ReadFile(src)
	require.NoError(t, readErr)
	require.Equal(t, "foreign source successor", string(got))
	require.NotEmpty(t, claim)
	got, readErr = os.ReadFile(claim)
	require.NoError(t, readErr)
	require.Equal(t, "admitted source", string(got), "the refused restoration keeps the original recoverable")
	require.NoFileExists(t, dst)
}

func TestIssue252VerifiedPreflightRefusalAttributesNoOutput(t *testing.T) {
	for _, name := range []string{"mode", "install", "digest-mode"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			src, dst := filepath.Join(root, "source"), filepath.Join(root, "out", "target")
			require.NoError(t, os.WriteFile(src, []byte("source"), 0o600))
			stubNoClobberProbe(t, func(afero.Fs, string, string) error {
				return fmt.Errorf("%w: %w", ErrPublishNoReplaceUnsupported, syscall.EPERM)
			})
			fs := afero.NewOsFs()
			proof := func(string, os.FileInfo) error { return nil }
			var installed *BoundInstallIdentity
			var digest string
			var err error
			switch name {
			case "mode":
				installed, err = CopyFileNoReplaceVerifiedMode(fs, src, dst, proof, 0o600)
			case "install":
				installed, err = CopyFileNoReplaceVerifiedInstall(fs, src, dst, proof)
			case "digest-mode":
				digest, installed, err = CopyFileNoReplaceVerifiedDigestMode(fs, src, dst, proof, 0o600)
			}
			require.ErrorIs(t, err, ErrPublishNoReplaceUnsupported)
			require.Nil(t, installed, "a preflight refusal attributes no installed identity")
			require.Empty(t, digest, "a preflight refusal attributes no published digest")
			require.FileExists(t, src)
			require.NoFileExists(t, dst)
		})
	}
}

type issue252InterruptedSource struct {
	afero.File
	closed bool
}

func (*issue252InterruptedSource) Read([]byte) (int, error) { return 0, syscall.EINTR }
func (f *issue252InterruptedSource) Close() error           { f.closed = true; return f.File.Close() }

// These APIs have no context parameter. This covers interrupted source I/O
// after a successful preflight, without claiming context cancellation support.
func TestIssue252VerifiedInterruptedStreamPreservesSource(t *testing.T) {
	for _, name := range []string{"mode", "install", "digest-mode"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			src, dst := filepath.Join(root, "source"), filepath.Join(root, "out", "target")
			require.NoError(t, os.WriteFile(src, []byte("source"), 0o600))
			require.NoError(t, os.Mkdir(filepath.Dir(dst), 0o700))
			fs := afero.NewOsFs()
			input, err := fs.Open(src)
			require.NoError(t, err)
			file := &issue252InterruptedSource{File: input}
			proof := func(string, os.FileInfo) error { return nil }
			var installed *BoundInstallIdentity
			var digest string
			switch name {
			case "mode":
				installed, err = streamVerifiedSource(fs, file, src, dst, proof, "copy", "source", false, 0o600)
			case "install":
				installed, err = streamVerifiedSourceInstall(fs, file, src, dst, proof)
			case "digest-mode":
				digest, installed, err = streamVerifiedSourceDigest(fs, file, src, dst, proof, "copy", "source", 0o600)
			}
			require.ErrorIs(t, err, syscall.EINTR)
			require.True(t, file.closed, "every early return closes the admitted source handle")
			require.Nil(t, installed)
			require.Empty(t, digest)
			require.False(t, PublishCompleted(err))
			got, readErr := os.ReadFile(src)
			require.NoError(t, readErr)
			require.Equal(t, "source", string(got))
			entries, readErr := os.ReadDir(filepath.Dir(dst))
			require.NoError(t, readErr)
			require.Empty(t, entries, "an interrupted owned stream cleans its probe and staging")
		})
	}
}

func issue252AllStreamOperations() []issue252VerifiedOperation {
	return append(issue252VerifiedOperations(),
		issue252VerifiedOperation{"legacy-copy", func(fs afero.Fs, src, dst string, _ VerifiedSourceProof) error {
			return CopyFileNoReplace(fs, src, dst)
		}},
		issue252VerifiedOperation{"legacy-copy-digest", func(fs afero.Fs, src, dst string, _ VerifiedSourceProof) error {
			_, err := CopyFileNoReplaceDigest(fs, src, dst)
			return err
		}},
		issue252VerifiedOperation{"legacy-cross-device-move", func(fs afero.Fs, src, dst string, _ VerifiedSourceProof) error {
			return MoveFileNoReplace(fs, src, dst)
		}},
	)
}

func TestIssue252CompletionBelongsOnlyToFinalPayload(t *testing.T) {
	for _, tc := range issue252AllStreamOperations() {
		for _, failProbe := range []bool{false, true} {
			name := "payload-cleanup-failure"
			if failProbe {
				name = "probe-cleanup-failure"
			}
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				root := t.TempDir()
				src, dst := filepath.Join(root, "source"), filepath.Join(root, "out", "target")
				payload := []byte("admitted source")
				require.NoError(t, os.WriteFile(src, payload, 0o600))
				oldRename, oldRemove := renameNoReplaceKernel, publishNoReplaceRemove
				t.Cleanup(func() { renameNoReplaceKernel, publishNoReplaceRemove = oldRename, oldRemove })
				probes := 0
				renameNoReplaceKernel = func(s, d string) error {
					if filepath.Dir(s) != filepath.Dir(dst) && d == dst {
						return syscall.EXDEV
					}
					isProbe := strings.HasPrefix(filepath.Base(s), ".nrprobe.")
					if isProbe && strings.HasSuffix(d, ".published") {
						probes++
					}
					if (failProbe && isProbe) || (!failProbe && strings.Contains(filepath.Base(s), ".nrstg.")) {
						return syscall.EINVAL
					}
					return oldRename(s, d)
				}
				publishNoReplaceRemove = func(path string) error {
					if failProbe && strings.HasPrefix(filepath.Base(path), ".nrprobe.") {
						return syscall.EPERM
					}
					if !failProbe && (strings.Contains(filepath.Base(path), ".nrstg.") || path == dst) {
						return syscall.EPERM
					}
					return oldRemove(path)
				}
				for attempt := 0; attempt < 2; attempt++ {
					err := tc.run(afero.NewOsFs(), src, dst, func(string, os.FileInfo) error { return nil })
					require.Error(t, err)
					if failProbe {
						require.False(t, PublishCompleted(err), "scratch completion conveys no ownership of the requested target")
						require.NoFileExists(t, dst)
						require.ErrorContains(t, err, "no payload published")
						require.ErrorContains(t, err, syscall.EPERM.Error(), "root cleanup diagnostics stay readable")
						require.ErrorContains(t, err, ".published", "residue location stays discoverable")
					} else {
						require.True(t, PublishCompleted(err), "actual installed payload retains its completion marker")
						got, readErr := os.ReadFile(dst)
						require.NoError(t, readErr)
						require.Equal(t, payload, got)
					}
					got, readErr := os.ReadFile(src)
					require.NoError(t, readErr)
					require.Equal(t, payload, got)
					if !failProbe {
						break
					}
				}
				if failProbe {
					require.Equal(t, 2, probes, "incomplete probe cleanup is indeterminate and never cached")
				} else {
					require.Equal(t, 1, probes)
				}
			})
		}
	}
}
