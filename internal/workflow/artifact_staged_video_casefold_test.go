package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type stagedVideoFollowingFS struct{ afero.Fs }

func (fs *stagedVideoFollowingFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	info, err := fs.Stat(path)
	return info, false, err
}

func TestStagedVideoCaseOnlyPublication(t *testing.T) {
	testStagedVideoCaseOnlyPublication(t, false)
}

// Native Windows CI exercises the real organizer, real directory entries, and
// eager NTFS identities. Case-sensitive native filesystems skip only this variant.
func TestStagedVideoCaseOnlyNativePublication(t *testing.T) {
	testStagedVideoCaseOnlyPublication(t, true)
}

func testStagedVideoCaseOnlyPublication(t *testing.T, native bool) {
	for _, part := range []string{"primary", "multipart"} {
		for _, occupied := range []bool{false, true} {
			name := part + "/vacant"
			if occupied {
				name = part + "/late occupant"
			}
			t.Run(name, func(t *testing.T) {
				env := setupCopyIntentE2E(t, "casefold-video")
				incoming := filepath.Dir(env.source)
				source := filepath.Join(incoming, "abc-123.mp4")
				require.NoError(t, os.Rename(env.source, source))
				require.NoError(t, os.Remove(env.subtitle))
				multipart := filepath.Join(incoming, "abc-123-cd2.mp4")
				if part == "multipart" {
					require.NoError(t, os.Rename(env.multipart, multipart))
				} else {
					require.NoError(t, os.Remove(env.multipart))
				}
				if native {
					if _, err := os.Lstat(filepath.Join(incoming, "ABC-123.mp4")); os.IsNotExist(err) {
						t.Skip("native filesystem has case-sensitive names")
					}
				} else {
					env.fs = &stagedCaseFoldFS{Fs: env.fs, root: filepath.Dir(incoming)}
					env.orch.fs = env.fs
				}
				env.movie.ID = "ABC-123"
				env.source, env.match.Path, env.match.Name, env.match.MovieID = source, source, filepath.Base(source), env.movie.ID
				pins := map[string]artifactSourceIdentity{}
				pinPath := func(path string) {
					info, err := lstatArtifactSource(env.fs, path)
					require.NoError(t, err)
					pin := captureArtifactSourceIdentity(env.fs, path, info)
					require.True(t, pin.hasDevIno, "native fixture requires eager strong object identity")
					pins[path] = pin
				}
				pinPath(source)
				if part == "multipart" {
					pinPath(multipart)
				}
				m, err := matcher.NewMatcher(&matcher.Config{})
				require.NoError(t, err)
				org := organizer.NewOrganizer(env.fs, &organizer.Config{FolderFormat: "renamed", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlace}, template.NewEngine(), m)
				primary := filepath.Join(filepath.Dir(incoming), "renamed", "ABC-123.mp4")
				target := primary
				want := "video"
				if part == "multipart" {
					target = filepath.Join(filepath.Dir(primary), "ABC-123-cd2.mp4")
					want = "part two"
				}
				plan, err := org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: env.match, Movie: &env.movie, DestDir: incoming, OperationMode: operationmode.OperationModeInPlace})
				require.NoError(t, err)
				require.Equal(t, primary, plan.TargetPath, "must actually plan the NEW final folder")
				executed := false
				env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: org, preExecute: func(plan *organizer.OrganizePlan) {
					executed = true
					require.True(t, strings.EqualFold(plan.SourcePath, plan.TargetPath), "stage strategy must perform a case-only same-object no-op")
					entries, err := afero.ReadDir(env.fs, filepath.Dir(plan.SourcePath))
					require.NoError(t, err)
					actual := ""
					for _, entry := range entries {
						if strings.EqualFold(entry.Name(), filepath.Base(plan.TargetPath)) {
							actual = entry.Name()
						}
					}
					require.NotEmpty(t, actual)
					require.NotEqual(t, filepath.Base(plan.TargetPath), actual, "real staged entry must retain its original lowercase spelling")
					require.True(t, artifactSameRegularFile(env.fs, plan.TargetPath, filepath.Join(filepath.Dir(plan.SourcePath), actual)))
					if occupied {
						require.NoError(t, env.fs.MkdirAll(filepath.Dir(target), 0755))
						require.NoError(t, afero.WriteFile(env.fs, target, []byte(want), 0644))
						pinPath(target) // Same bytes cannot grant ownership of the foreign winner.
					}
				}}
				cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlace, false, false, organizer.LinkModeNone, false, false)
				cmd.PublicationFence = postPublishFence{movie: &env.movie}
				cmd.Organize.ForceUpdate = false
				_, err = env.orch.Execute(t.Context(), cmd)
				require.True(t, executed)
				if occupied {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				data, err := afero.ReadFile(env.fs, target)
				require.NoError(t, err)
				require.Equal(t, want, string(data))
				checkPins := func() {
					for path, pin := range pins {
						info, err := lstatArtifactSource(env.fs, path)
						require.NoError(t, err)
						require.True(t, pin.matches(env.fs, path, info), "retain exact original object at %s", path)
					}
				}
				checkPins()
				if occupied {
					assertStagedVideoAliasUnowned(t, env, target)
				}
				_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
				require.NoError(t, err)
				checkPins()
			})
		}
	}
}

func TestStagedVideoOutputRoleBoundaries(t *testing.T) {
	for _, kind := range []string{"exact", "unrelated", "alias", "different inode", "missing registered", "symlink", "no lstat capability", "following probe", "weak identity"} {
		t.Run(kind, func(t *testing.T) {
			base := afero.NewOsFs()
			root := t.TempDir()
			registered, actual := filepath.Join(root, "VIDEO.mp4"), filepath.Join(root, "video.mp4")
			require.NoError(t, afero.WriteFile(base, registered, []byte("video"), 0644))
			if _, err := base.Stat(actual); os.IsNotExist(err) {
				require.NoError(t, os.Link(registered, actual))
			}
			s := &artifactStage{fs: base, root: root, finalRoot: filepath.Join(root, "final"), stagedVideoOutputs: map[string]bool{registered: false}}
			wantVideo, wantErr := false, false
			switch kind {
			case "exact":
				actual = registered
				wantVideo = true
			case "unrelated":
				actual = filepath.Join(root, "trailer.mp4")
			case "alias":
				wantVideo = true
			case "different inode", "missing registered", "symlink":
				// These alternate entries can only coexist on a case-sensitive filesystem.
				entries, err := os.ReadDir(root)
				require.NoError(t, err)
				if len(entries) != 2 {
					t.Skip("native filesystem cannot hold distinct case-only entries")
				}
				require.NoError(t, base.Remove(actual))
				if kind == "symlink" {
					if !pr260LinkSupported(t, organizer.LinkModeSoft, registered) {
						return
					}
					require.NoError(t, os.Symlink(registered, actual))
				} else {
					require.NoError(t, afero.WriteFile(base, actual, []byte("video"), 0644))
				}
				if kind == "missing registered" {
					require.NoError(t, base.Remove(registered))
				}
				wantErr = true
			case "no lstat capability":
				s.fs = struct{ afero.Fs }{base}
				wantErr = true
			case "following probe":
				s.fs = &stagedVideoFollowingFS{Fs: base}
				wantErr = true
			case "weak identity":
				s.fs = &artifactAliasProbeFS{Fs: base, probe: func(path string) (os.FileInfo, bool, error) {
					info, err := os.Lstat(path)
					return weakIdentityInfo{info}, true, err
				}}
				wantErr = true
			}
			key, primary, video, err := s.stagedVideoOutput(actual)
			require.Equal(t, wantVideo, video)
			require.False(t, primary, "exact multipart role must survive spelling recovery")
			if wantVideo {
				require.Equal(t, registered, key)
			} else {
				require.Empty(t, key)
			}
			if wantErr {
				require.ErrorIs(t, err, errArtifactSourceChanged)
				_, installErr := s.installPaths([]string{actual}, nil, "", "", nil)
				require.ErrorIs(t, installErr, errArtifactSourceChanged, "ambiguous video role cannot reach generic publication")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestStagedVideoRehomeCaseOnlyBoundaries(t *testing.T) {
	for _, kind := range []string{"native rename", "proven alias", "hidden no-follow", "false no-follow", "distinct target", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			base := afero.NewOsFs()
			source := filepath.Join(root, "movie-cd2.mp4")
			target := filepath.Join(root, "MOVIE-cd2.mp4")
			require.NoError(t, afero.WriteFile(base, source, []byte("part two"), 0644))
			info, err := os.Lstat(source)
			require.NoError(t, err)
			pin := captureArtifactSourceIdentity(base, source, info)
			require.True(t, pin.hasDevIno)
			var fs afero.Fs = base
			wantErr := false
			switch kind {
			case "proven alias", "hidden no-follow", "false no-follow":
				fs = &stagedCaseFoldFS{Fs: base, root: root}
				if kind == "hidden no-follow" {
					fs = struct{ afero.Fs }{fs}
					wantErr = true
				}
				if kind == "false no-follow" {
					fs = &stagedVideoFollowingFS{Fs: fs}
					wantErr = true
				}
			case "distinct target", "symlink":
				if _, err := base.Stat(target); err == nil {
					t.Skip("requires distinct case-sensitive entries")
				}
				if kind == "symlink" {
					if !pr260LinkSupported(t, organizer.LinkModeSoft, source) {
						return
					}
					require.NoError(t, os.Symlink(source, target))
				} else {
					require.NoError(t, afero.WriteFile(base, target, []byte("foreign"), 0644))
				}
				wantErr = true
			}
			s := &artifactStage{fs: fs, stagedSource: filepath.Join(root, "movie.mp4"), siblings: []artifactSibling{{stagedPath: source}}}
			err = s.rehomeRemainingSiblings(filepath.Join(root, "MOVIE.mp4"), nil)
			if wantErr {
				require.ErrorIs(t, err, errArtifactSourceChanged)
				current, err := os.Lstat(source)
				require.NoError(t, err)
				require.True(t, pin.matches(base, source, current), "ambiguous alias never deletes the staged source")
			} else {
				require.NoError(t, err)
				current, err := lstatArtifactSource(fs, target)
				require.NoError(t, err)
				require.True(t, pin.matches(fs, target, current))
			}
		})
	}
}
