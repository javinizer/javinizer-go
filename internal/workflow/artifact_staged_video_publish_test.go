package workflow

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestStagedVideoLatePublicationPolicyMatrix(t *testing.T) {
	for _, primary := range []bool{false, true} {
		for _, force := range []bool{false, true} {
			for _, rollback := range []bool{false, true} {
				name := "multipart"
				if primary {
					name = "primary"
				}
				if force {
					name += "/force"
				} else {
					name += "/no-force"
				}
				if rollback {
					name += "/rollback"
				}
				t.Run(name, func(t *testing.T) {
					env := setupCopyIntentE2E(t, "late-video-policy")
					incoming := filepath.Dir(env.source)
					leaf := env.movie.ID + "-cd2.mp4"
					if primary {
						leaf = env.movie.ID + ".mp4"
					}
					target := filepath.Join(incoming, leaf)
					org := organizer.NewOrganizer(env.fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
					fired := false
					env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: org, preExecute: func(*organizer.OrganizePlan) {
						fired = true
						require.NoError(t, os.WriteFile(target, []byte("foreign video"), 0644))
					}}
					cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
					cmd.Organize.ForceUpdate = force
					if rollback {
						cmd.PublicationFence = postPublishFence{movie: &env.movie, err: os.ErrPermission}
					}
					_, err := env.orch.Execute(t.Context(), cmd)
					require.True(t, fired)
					want := "foreign video"
					if primary && force && !rollback {
						require.NoError(t, err)
						want = "video"
					}
					if primary && force && rollback {
						require.ErrorIs(t, err, os.ErrPermission)
					}
					b, e := os.ReadFile(target)
					require.NoError(t, e)
					require.Equal(t, want, string(b))
					for path, content := range map[string]string{env.source: "video", env.multipart: "part two"} {
						b, e := os.ReadFile(path)
						require.NoError(t, e)
						require.Equal(t, content, string(b))
					}
				})
			}
		}
	}
}

func TestStagedVideoResidentSourcesRemainUnowned(t *testing.T) {
	for _, substituted := range []string{"", "primary", "multipart"} {
		t.Run("resident_"+substituted, func(t *testing.T) {
			env := setupCopyIntentE2E(t, "resident-video")
			incoming := filepath.Dir(env.source)
			primary := filepath.Join(incoming, env.movie.ID+".mp4")
			multipart := filepath.Join(incoming, env.movie.ID+"-cd2.mp4")
			require.NoError(t, os.Rename(env.source, primary))
			require.NoError(t, os.Rename(env.multipart, multipart))
			env.source, env.multipart = primary, multipart
			env.match.Path, env.match.Name = primary, filepath.Base(primary)
			org := organizer.NewOrganizer(env.fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
			env.orch.organizer = org
			identities := map[string]artifactSourceIdentity{}
			pin := func(path string) {
				info, err := os.Stat(path)
				require.NoError(t, err)
				identity := captureArtifactSourceIdentity(env.fs, path, info)
				// Capture the kernel key now. Windows os.SameFile can load a
				// stored FileInfo's identity lazily from a replacement pathname.
				require.True(t, identity.hasDevIno, "native fixture requires an eagerly captured strong identity")
				identities[path] = identity
			}
			for _, path := range []string{primary, multipart} {
				pin(path)
			}
			if substituted != "" {
				env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: org, preExecute: func(*organizer.OrganizePlan) {
					path, body := primary, "video"
					if substituted == "multipart" {
						path, body = multipart, "part two"
					}
					require.NoError(t, os.Rename(path, path+".old"))
					require.NoError(t, os.WriteFile(path, []byte(body), 0644))
					pin(path)
				}}
			}
			cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
			cmd.Organize.ForceUpdate = false
			_, err := env.orch.Execute(t.Context(), cmd)
			if substituted == "" {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, errArtifactSourceChanged)
			}
			assertResidentIdentities := func() {
				for path, before := range identities {
					after, err := os.Stat(path)
					require.NoError(t, err)
					require.True(t, before.matches(env.fs, path, after), "resident or substituted source must never become this apply's output")
				}
			}
			assertResidentIdentities()
			_, _ = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
			assertResidentIdentities()
		})
	}
}

type stagedVideoPlantAfterIntentLog struct {
	RevertLog
	fs      afero.Fs
	target  string
	fired   bool
	body    []byte
	planted artifactSourceIdentity
}

func (l *stagedVideoPlantAfterIntentLog) RecordDeleteIntent(ctx context.Context, id OperationID, entries []models.DeleteEntry) error {
	if err := l.RevertLog.RecordDeleteIntent(ctx, id, entries); err != nil {
		return err
	}
	for _, entry := range entries {
		if filepath.Clean(entry.Path) == filepath.Clean(l.target) && !l.fired {
			l.fired = true
			body := l.body
			if body == nil {
				body = []byte("foreign after arm")
			}
			if err := os.WriteFile(l.target, body, 0644); err != nil {
				return err
			}
			info, err := os.Stat(l.target)
			if err != nil {
				return err
			}
			l.planted = captureArtifactSourceIdentity(l.fs, l.target, info)
			return nil
		}
	}
	return nil
}
func TestStagedVideoAfterArmMatrix(t *testing.T) {
	for _, primary := range []bool{false, true} {
		for _, force := range []bool{false, true} {
			name := "multipart"
			if primary {
				name = "primary"
			}
			if force {
				name += "/force"
			} else {
				name += "/no-force"
			}
			t.Run(name, func(t *testing.T) {
				env := setupCopyIntentE2E(t, "after-arm-video")
				incoming := filepath.Dir(env.source)
				leaf := env.movie.ID + "-cd2.mp4"
				if primary {
					leaf = env.movie.ID + ".mp4"
				}
				target := filepath.Join(incoming, leaf)
				env.orch.organizer = organizer.NewOrganizer(env.fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
				l := &stagedVideoPlantAfterIntentLog{RevertLog: env.orch.revertLog, fs: env.fs, target: target}
				env.orch.revertLog = l
				cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
				cmd.Organize.ForceUpdate = force
				_, _ = env.orch.Execute(t.Context(), cmd)
				require.True(t, l.fired, "occupant must be planted after durable create intent/arming")
				require.True(t, l.planted.hasDevIno, "native fixture requires an eagerly captured strong identity")
				after, e := os.Stat(target)
				require.NoError(t, e)
				require.True(t, l.planted.matches(env.fs, target, after), "after-arm foreign object must not be replaced")
				b, e := os.ReadFile(target)
				require.NoError(t, e)
				require.Equal(t, "foreign after arm", string(b))
				require.FileExists(t, env.source)
				require.FileExists(t, env.multipart)
			})
		}
	}
}

func TestStagedVideoSameBytesAfterArmRetainsOwnership(t *testing.T) {
	for _, primary := range []bool{false, true} {
		name := "multipart"
		if primary {
			name = "primary"
		}
		t.Run(name, func(t *testing.T) {
			env := setupCopyIntentE2E(t, "same-bytes-after-arm")
			incoming := filepath.Dir(env.source)
			leaf := env.movie.ID + "-cd2.mp4"
			body := "part two"
			if primary {
				leaf = env.movie.ID + ".mp4"
				body = "video"
			}
			target := filepath.Join(incoming, leaf)
			env.orch.organizer = organizer.NewOrganizer(env.fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
			l := &stagedVideoPlantAfterIntentLog{RevertLog: env.orch.revertLog, fs: env.fs, target: target, body: []byte(body)}
			env.orch.revertLog = l
			cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
			cmd.Organize.ForceUpdate = false
			_, _ = env.orch.Execute(t.Context(), cmd)
			require.True(t, l.fired)
			require.True(t, l.planted.hasDevIno, "native fixture requires an eagerly captured strong identity")
			after, e := os.Stat(target)
			require.NoError(t, e)
			require.True(t, l.planted.matches(env.fs, target, after), "same-content foreign object must not be replaced")
			_, revErr := history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
			t.Logf("revert outcome: %v", revErr)
			after, e = os.Stat(target)
			require.NoError(t, e, "revert must not delete same-content foreign claimant")
			require.True(t, l.planted.matches(env.fs, target, after), "Undo must retain the exact foreign object captured at planting")
			require.FileExists(t, env.source)
		})
	}
}
