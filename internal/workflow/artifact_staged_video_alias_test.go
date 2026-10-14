package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestStagedVideoPrimaryResidentHardlinkAlias(t *testing.T) {
	env := setupCopyIntentE2E(t, "resident-hardlink-alias")
	incoming := filepath.Dir(env.source)
	target := filepath.Join(incoming, env.movie.ID+".mp4")
	require.NoError(t, os.Link(env.source, target))
	info, e := os.Lstat(env.source)
	require.NoError(t, e)
	pin := captureArtifactSourceIdentity(env.fs, env.source, info)
	require.True(t, pin.hasDevIno)
	env.orch.organizer = organizer.NewOrganizer(env.fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
	cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
	cmd.Organize.ForceUpdate = false
	_, e = env.orch.Execute(t.Context(), cmd)
	require.NoError(t, e)
	assertStagedVideoAliasUnowned(t, env, target)
	for _, path := range []string{env.source, target} {
		current, e := os.Lstat(path)
		require.NoError(t, e)
		require.True(t, pin.matches(env.fs, path, current), "resident hardlink identity preserved at %s", path)
	}
	_, e = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, e)
	for _, path := range []string{env.source, target} {
		current, e := os.Lstat(path)
		require.NoError(t, e)
		require.True(t, pin.matches(env.fs, path, current), "Undo retains resident hardlink at %s", path)
	}
}

func assertStagedVideoAliasUnowned(t *testing.T, env *copyIntentE2EEnv, target string) {
	t.Helper()
	records, err := env.repo.FindByBatchJobID(t.Context(), env.jobID)
	require.NoError(t, err)
	require.Len(t, records, 1)
	ledger, err := models.ParseGeneratedFiles(records[0].GeneratedFiles)
	require.NoError(t, err)
	require.NotContains(t, ledger.Delete, target)
	for _, entry := range ledger.PlannedDeletes {
		require.NotEqual(t, target, entry.Path)
	}
	for _, entry := range ledger.MoveBack {
		require.NotEqual(t, target, entry.NewPath)
	}
	for _, entry := range ledger.Replacements {
		require.NotEqual(t, target, entry.Destination)
	}
}

func TestStagedVideoPrimaryAliasBoundaries(t *testing.T) {
	for _, kind := range []string{"alias", "missing", "lookup failure", "weak identity", "same bytes", "symlink", "changed source", "multipart unmapped", "no lstat capability", "following probe"} {
		t.Run(kind, func(t *testing.T) {
			fs := afero.NewOsFs()
			root := t.TempDir()
			source, target := filepath.Join(root, "source.mp4"), filepath.Join(root, "target.mp4")
			require.NoError(t, os.WriteFile(source, []byte("video"), 0644))
			info, err := os.Lstat(source)
			require.NoError(t, err)
			pin := captureArtifactSourceIdentity(fs, source, info)
			require.True(t, pin.hasDevIno, "native fixture must expose an eager strong identity")
			s := &artifactStage{fs: fs, sourcePath: source, sourceIdentity: pin}
			primary, wantResident, wantErr := true, false, false
			if kind != "missing" && kind != "same bytes" && kind != "symlink" {
				require.NoError(t, os.Link(source, target))
			}
			switch kind {
			case "alias":
				wantResident = true
			case "lookup failure":
				s.fs = &publicationBranchFS{Fs: fs, lstatPath: target}
				wantErr = true
			case "weak identity":
				s.sourceIdentity.hasDevIno = false
			case "same bytes":
				require.NoError(t, os.WriteFile(target, []byte("video"), 0644))
				require.NoError(t, os.Chtimes(target, info.ModTime(), info.ModTime()))
			case "symlink":
				if !pr260LinkSupported(t, organizer.LinkModeSoft, source) {
					return
				}
				require.NoError(t, os.Symlink(source, target))
			case "changed source":
				require.NoError(t, os.Rename(source, source+".old"))
				require.NoError(t, os.WriteFile(source, []byte("video"), 0644))
				require.NoError(t, os.Chtimes(source, info.ModTime(), info.ModTime()))
				wantResident, wantErr = true, true
			case "no lstat capability":
				s.fs = struct{ afero.Fs }{fs}
			case "following probe":
				s.fs = &stagedVideoFollowingFS{Fs: fs}
			case "multipart unmapped":
				primary = false
				s.siblings = []artifactSibling{{sourcePath: source, identity: pin}}
			}
			resident, err := s.stagedVideoResident("unmapped", target, primary)
			require.Equal(t, wantResident, resident)
			if wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestStagedVideoPrimaryAliasPublicationMatrix(t *testing.T) {
	for _, kind := range []string{"late alias", "forced alias", "rejected fence", "same bytes", "symlink", "target substitution", "source substitution"} {
		t.Run(kind, func(t *testing.T) {
			env := setupCopyIntentE2E(t, "primary-alias-matrix")
			incoming := filepath.Dir(env.source)
			target := filepath.Join(incoming, env.movie.ID+".mp4")
			pins := map[string]artifactSourceIdentity{}
			pinPath := func(path string) {
				info, err := os.Lstat(path)
				require.NoError(t, err)
				pin := captureArtifactSourceIdentity(env.fs, path, info)
				require.True(t, pin.hasDevIno)
				pins[path] = pin
			}
			pinPath(env.source)
			if kind == "same bytes" {
				require.NoError(t, os.WriteFile(target, []byte("video"), 0644))
				pinPath(target)
			} else if kind == "symlink" {
				if !pr260LinkSupported(t, organizer.LinkModeSoft, env.source) {
					return
				}
				require.NoError(t, os.Symlink(env.source, target))
			} else if kind != "late alias" {
				require.NoError(t, os.Link(env.source, target))
				pinPath(target)
			}
			org := organizer.NewOrganizer(env.fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
			env.orch.organizer = org
			if kind == "late alias" || kind == "target substitution" || kind == "source substitution" {
				env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: org, preExecute: func(*organizer.OrganizePlan) {
					if kind == "late alias" {
						require.NoError(t, os.Link(env.source, target))
						pinPath(target)
						return
					}
					path := target
					if kind == "source substitution" {
						path = env.source
					}
					old := pins[path]
					require.NoError(t, os.Rename(path, path+".old"))
					require.NoError(t, os.WriteFile(path, []byte("video"), 0644))
					require.NoError(t, os.Chtimes(path, old.modTime, old.modTime))
					pinPath(path)
				}}
			}
			cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
			cmd.Organize.ForceUpdate = kind == "forced alias"
			if kind == "rejected fence" {
				cmd.PublicationFence = postPublishFence{movie: &env.movie, err: os.ErrPermission}
			}
			_, err := env.orch.Execute(t.Context(), cmd)
			if kind == "late alias" || kind == "forced alias" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			assertStagedVideoAliasUnowned(t, env, target)
			checkNames := func() {
				for path, pin := range pins {
					info, err := os.Lstat(path)
					require.NoError(t, err)
					require.True(t, pin.matches(env.fs, path, info), "existing object at %s survives", path)
				}
				if kind == "symlink" {
					link, err := os.Readlink(target)
					require.NoError(t, err)
					require.Equal(t, env.source, link)
				}
			}
			checkNames()
			_, _ = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
			checkNames()
		})
	}
}

func TestStagedVideoPrimaryCaseOnlyAlias(t *testing.T) {
	env := setupCopyIntentE2E(t, "case-only-alias")
	incoming := filepath.Dir(env.source)
	source := filepath.Join(incoming, strings.ToLower(env.movie.ID)+".mp4")
	target := filepath.Join(incoming, env.movie.ID+".mp4")
	require.NotEqual(t, source, target)
	require.NoError(t, os.Rename(env.source, source))
	if _, err := os.Lstat(target); os.IsNotExist(err) {
		t.Skip("native filesystem has case-sensitive names")
	}
	env.source, env.match.Path, env.match.Name = source, source, filepath.Base(source)
	info, err := os.Lstat(source)
	require.NoError(t, err)
	pin := captureArtifactSourceIdentity(env.fs, source, info)
	require.True(t, pin.hasDevIno, "native fixture must expose an eager strong identity")
	s := &artifactStage{fs: env.fs, sourcePath: source, sourceIdentity: pin}
	resident, err := s.stagedVideoResident(source, target, true)
	require.NoError(t, err)
	require.True(t, resident)
	env.orch.organizer = organizer.NewOrganizer(env.fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
	cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
	cmd.Organize.ForceUpdate = false
	_, err = env.orch.Execute(t.Context(), cmd)
	require.NoError(t, err)
	assertStagedVideoAliasUnowned(t, env, target)
	checkNames := func() {
		for _, path := range []string{source, target} {
			info, err := os.Lstat(path)
			require.NoError(t, err)
			require.True(t, pin.matches(env.fs, path, info))
		}
	}
	checkNames()
	_, err = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, err)
	checkNames()
}

func TestStagedVideoMultipartAliasCannotAdoptOtherSource(t *testing.T) {
	env := setupCopyIntentE2E(t, "multipart-wrong-alias")
	incoming := filepath.Dir(env.source)
	ext := filepath.Ext(env.source)
	other := strings.TrimSuffix(env.source, ext) + "-cd3" + ext
	require.NoError(t, os.WriteFile(other, []byte("third part"), 0644))
	target := filepath.Join(incoming, env.movie.ID+"-cd2.mp4")
	require.NoError(t, os.Link(other, target))
	info, e := os.Lstat(other)
	require.NoError(t, e)
	pin := captureArtifactSourceIdentity(env.fs, other, info)
	require.True(t, pin.hasDevIno)
	env.orch.organizer = organizer.NewOrganizer(env.fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
	cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
	cmd.Organize.ForceUpdate = false
	_, e = env.orch.Execute(t.Context(), cmd)
	require.Error(t, e, "part2 cannot silently accept part3's alias as its own resident output")
	for _, p := range []string{env.source, env.multipart, other} {
		require.FileExists(t, p)
	}
	current, e := os.Lstat(target)
	require.NoError(t, e)
	require.True(t, pin.matches(env.fs, target, current))
	assertStagedVideoAliasUnowned(t, env, target)
	_, e = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, e)
	current, e = os.Lstat(target)
	require.NoError(t, e)
	require.True(t, pin.matches(env.fs, target, current))
}

func TestStagedVideoResidentMultipartHardlinkAlias(t *testing.T) {
	env := setupCopyIntentE2E(t, "resident-multipart-alias")
	incoming := filepath.Dir(env.source)
	target := filepath.Join(incoming, env.movie.ID+"-cd2.mp4")
	require.NoError(t, os.Link(env.multipart, target))
	info, e := os.Lstat(env.multipart)
	require.NoError(t, e)
	pin := captureArtifactSourceIdentity(env.fs, env.multipart, info)
	require.True(t, pin.hasDevIno)
	env.orch.organizer = organizer.NewOrganizer(env.fs, &organizer.Config{FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder}, template.NewEngine(), nil)
	cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), operationmode.OperationModeInPlaceNoRenameFolder, false, false, organizer.LinkModeNone, false, false)
	cmd.Organize.ForceUpdate = false
	_, e = env.orch.Execute(t.Context(), cmd)
	require.NoError(t, e)
	for _, path := range []string{env.multipart, target} {
		current, e := os.Lstat(path)
		require.NoError(t, e)
		require.True(t, pin.matches(env.fs, path, current), "resident hardlink identity preserved at %s", path)
	}
	assertStagedVideoAliasUnowned(t, env, target)
	_, e = history.NewReverter(env.fs, env.repo).RevertBatch(t.Context(), env.jobID)
	require.NoError(t, e)
	for _, path := range []string{env.multipart, target} {
		current, e := os.Lstat(path)
		require.NoError(t, e)
		require.True(t, pin.matches(env.fs, path, current), "Undo retains resident hardlink at %s", path)
	}
}
