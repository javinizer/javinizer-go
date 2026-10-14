//go:build windows

package workflow

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

// TestSMBRegression is opt-in: ordinary Windows tests do not create a share.
// The dedicated harness proves an authenticated SMB3 session before invoking
// this test and requires every Core leaf below to run and pass (never skip).
// These tiny byte fixtures are synthetic, not playable media or real metadata.
func TestSMBRegression(t *testing.T) {
	unc, local := smbRegressionRoots(t)
	t.Logf("real UNC fixture root: %s; local fixture root: %s", unc, local)
	t.Run("Core", func(t *testing.T) {
		for _, source := range []string{"local_to_unc", "unc_to_unc", "unc_inplace"} {
			for _, move := range []bool{false, true} {
				for _, rollback := range []bool{false, true} {
					name := source + "_copy"
					if move {
						name = source + "_move"
					}
					if rollback {
						name += "_rollback"
					}
					t.Run(name, func(t *testing.T) {
						smbRegressionApply(t, unc, local, source, move, rollback, organizer.LinkModeNone)
					})
				}
			}
		}
		// In-place semantics ignore LinkMode: these are required copy cases,
		// not optional link-capability probes.
		for _, mode := range []organizer.LinkMode{organizer.LinkModeHard, organizer.LinkModeSoft} {
			t.Run("unc_inplace_ignores_"+string(mode), func(t *testing.T) {
				smbRegressionApply(t, unc, local, "unc_inplace", false, false, mode)
			})
		}
		for _, occupant := range []string{"primary", "subtitle"} {
			t.Run("foreign_"+occupant+"_no_clobber", func(t *testing.T) {
				smbRegressionForeign(t, unc, local, occupant, false)
			})
			t.Run("same_byte_"+occupant+"_identity", func(t *testing.T) {
				smbRegressionForeign(t, unc, local, occupant, true)
			})
		}
	})
	t.Run("OptionalLinks", func(t *testing.T) {
		for _, mode := range []organizer.LinkMode{organizer.LinkModeHard, organizer.LinkModeSoft} {
			t.Run(string(mode), func(t *testing.T) {
				smbRegressionApply(t, unc, local, "unc_to_unc", false, false, mode)
			})
		}
	})
}

func smbRegressionRoots(t *testing.T) (string, string) {
	t.Helper()
	unc, local, token := os.Getenv("JAVINIZER_TEST_SMB_UNC"), os.Getenv("JAVINIZER_TEST_SMB_LOCAL"), os.Getenv("JAVINIZER_TEST_SMB_TOKEN")
	if unc == "" && local == "" && token == "" && os.Getenv("JAVINIZER_SMB_TEST_REQUIRED") == "" {
		t.Skip("real SMB integration is opt-in; use the reviewed dedicated Windows workflow")
	}
	require.Equal(t, "1", os.Getenv("JAVINIZER_SMB_TEST_REQUIRED"), "partially configured SMB tests must fail closed")
	require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{32}$`), token)
	share := `\\127.0.0.1\javinizer-smb-` + token
	require.Equal(t, share+`\fixtures`, unc, "only the ephemeral loopback share is accepted")
	runnerTemp := os.Getenv("RUNNER_TEMP")
	require.NotEmpty(t, runnerTemp)
	require.Equal(t, filepath.Join(runnerTemp, "javinizer-smb-"+token, "local"), local)
	for _, marker := range []string{filepath.Join(share, ".javinizer-smb-marker"), filepath.Join(filepath.Dir(local), ".javinizer-smb-marker")} {
		body, err := os.ReadFile(marker)
		require.NoError(t, err, "missing ownership marker %s", marker)
		require.Equal(t, token, string(body))
	}
	for _, root := range []string{unc, local} {
		info, err := os.Lstat(root)
		require.NoError(t, err)
		require.True(t, info.IsDir())
		require.Zero(t, info.Mode()&os.ModeSymlink, "fixture roots cannot be links")
	}
	return unc, local
}

func smbRegressionCaseDir(t *testing.T, parent string) string {
	t.Helper()
	dir, err := os.MkdirTemp(parent, "case-")
	require.NoError(t, err, "required SMB directory creation")
	rel, err := filepath.Rel(parent, dir)
	require.NoError(t, err)
	require.False(t, rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)))
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(dir), "remove exactly this synthetic case subtree") })
	return dir
}

type smbRegressionFixture struct {
	root, localRoot, source, targetDir string
	bodies                             map[string]string
	pins                               map[string]artifactSourceIdentity
	movie                              models.Movie
	orch                               *applyOrchImpl
	cmd                                ApplyCmd
}

func smbRegressionFixtureFor(t *testing.T, unc, local, sourceKind string, move bool, link organizer.LinkMode) smbRegressionFixture {
	t.Helper()
	f := smbRegressionFixture{root: smbRegressionCaseDir(t, unc), localRoot: smbRegressionCaseDir(t, local)}
	input := filepath.Join(f.root, "incoming")
	if sourceKind == "local_to_unc" {
		input = filepath.Join(f.localRoot, "incoming")
	}
	require.NoError(t, os.MkdirAll(input, 0o700))
	f.source = filepath.Join(input, "ABC-123 original.mkv")
	f.bodies = map[string]string{
		"ABC-123 original.mkv":     "synthetic primary video\x00\x01",
		"ABC-123 original-cd2.mkv": "synthetic multipart video\x00\x02",
		"ABC-123 original.srt":     "1\n00:00:00,000 --> 00:00:01,000\nSynthetic subtitle\n",
		"ABC-123 original.ass":     "excluded extension must survive",
		"ABC-1234.srt":             "unrelated movie subtitle must survive",
		"keep.txt":                 "unrelated notes must survive",
	}
	f.pins = make(map[string]artifactSourceIdentity)
	for name, body := range f.bodies {
		path := filepath.Join(input, name)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		info, err := os.Lstat(path)
		require.NoError(t, err)
		f.pins[name] = captureArtifactSourceIdentity(afero.NewOsFs(), path, info)
		require.True(t, f.pins[name].hasDevIno, "real SMB must expose an eager native object identity: %s", path)
	}
	mode := operationmode.OperationModeOrganize
	dest := filepath.Join(f.root, "library")
	f.targetDir = filepath.Join(dest, "movie")
	if sourceKind == "unc_inplace" {
		mode, dest, f.targetDir = operationmode.OperationModeInPlaceNoRenameFolder, input, input
	}
	fsys := afero.NewOsFs()
	engine := template.NewEngine()
	org := organizer.NewOrganizer(fsys, &organizer.Config{FolderFormat: "movie", FileFormat: "movie", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: mode}, engine, nil)
	nameCfg := nfo.NFONameConfig{FilenameTemplate: "movie.nfo"}
	gen := nfo.NewGenerator(fsys, &nfo.Config{FilenameTemplate: nameCfg.FilenameTemplate, TemplateEngine: engine})
	f.orch = newApplyOrchestrator(fsys, org, nil, gen, nil, ApplyConfig{NFONameCfg: nameCfg}, engine, noOpRevertLog{}, nil, nil)
	f.movie = models.Movie{ID: "ABC-123", ContentID: "synthetic-smb", Title: "Synthetic SMB regression", RenderGeneration: 1}
	f.cmd = ApplyCmd{Match: models.FileMatchInfo{Path: f.source, Name: filepath.Base(f.source), Extension: ".mkv", MovieID: f.movie.ID}, DestPath: dest, Organize: OrganizeOptions{MoveFiles: move, LinkMode: link}, GenerateNFO: true, OperationMode: mode}
	return f
}

func smbRegressionApply(t *testing.T, unc, local, sourceKind string, move, rollback bool, link organizer.LinkMode) {
	t.Helper()
	f := smbRegressionFixtureFor(t, unc, local, sourceKind, move, link)
	f.cmd.Movie = &f.movie
	if link != organizer.LinkModeNone && sourceKind != "unc_inplace" {
		smbRegressionProbeLink(t, f.source, filepath.Join(f.root, "link-probe"), link)
	}
	fenceErr := errors.New("synthetic post-publication fence rejection")
	fenceObserved := false
	published := map[string]string{"movie.mkv": "ABC-123 original.mkv", "movie.srt": "ABC-123 original.srt"}
	if link == organizer.LinkModeNone || sourceKind == "unc_inplace" {
		published["movie-cd2.mkv"] = "ABC-123 original-cd2.mkv"
	}
	// Organize link mode retains multipart sources; the existing contract
	// does not publish their renamed siblings. Core copy/move cases do.
	fence := postPublishFence{movie: &f.movie, after: func() {
		fenceObserved = true
		require.FileExists(t, filepath.Join(f.targetDir, "movie.nfo"))
		for name := range published {
			require.FileExists(t, filepath.Join(f.targetDir, name), "fence must observe actual published UNC bytes")
		}
	}}
	if rollback {
		fence.err = fenceErr
	}
	f.cmd.PublicationFence = fence
	result, err := f.orch.Execute(t.Context(), f.cmd)
	if rollback {
		require.ErrorIs(t, err, fenceErr)
	} else {
		require.NoError(t, err, "required SMB workflow behavior is never skipped")
		require.NotNil(t, result.OrganizeResult)
		require.Equal(t, filepath.Join(f.targetDir, "movie.mkv"), result.OrganizeResult.NewPath)
		for name, original := range published {
			path := filepath.Join(f.targetDir, name)
			if name == "movie.mkv" && link == organizer.LinkModeSoft && sourceKind != "unc_inplace" {
				body, readErr := os.ReadFile(path)
				require.NoError(t, readErr)
				require.Equal(t, f.bodies[original], string(body))
			} else {
				smbRegressionBytes(t, path, f.bodies[original])
			}
		}
		nfoBytes, readErr := os.ReadFile(result.NFOPath)
		require.NoError(t, readErr)
		require.Contains(t, string(nfoBytes), f.movie.Title)
		require.Equal(t, filepath.Join(f.targetDir, "movie.nfo"), result.NFOPath)
		subInfo, statErr := os.Lstat(filepath.Join(f.targetDir, "movie.srt"))
		require.NoError(t, statErr)
		require.True(t, subInfo.Mode().IsRegular(), "selected subtitles are regular files, including link modes")
		require.Len(t, result.OrganizeResult.Subtitles, 1)
		if link != organizer.LinkModeNone && sourceKind != "unc_inplace" {
			require.True(t, result.OrganizeResult.Subtitles[0].Copied, "link mode copies selected subtitles")
			require.False(t, result.OrganizeResult.Subtitles[0].Moved)
			srcSub, statErr := os.Stat(filepath.Join(filepath.Dir(f.source), "ABC-123 original.srt"))
			require.NoError(t, statErr)
			require.False(t, os.SameFile(srcSub, subInfo), "subtitle copy must not be a hard link")
			smbRegressionAssertLink(t, f.source, result.OrganizeResult.NewPath, link)
		} else {
			info, statErr := os.Lstat(result.OrganizeResult.NewPath)
			require.NoError(t, statErr)
			require.True(t, info.Mode().IsRegular(), "in-place ignores link mode")
		}
	}
	require.True(t, fenceObserved, "the publication fence must actually execute")
	for name, body := range f.bodies {
		selected := strings.HasSuffix(name, ".mkv") || name == "ABC-123 original.srt"
		consumed := move && !rollback && (link == organizer.LinkModeNone || sourceKind == "unc_inplace") && selected
		path := filepath.Join(filepath.Dir(f.source), name)
		if consumed {
			_, statErr := os.Stat(path)
			require.True(t, os.IsNotExist(statErr), "only successfully moved selected files are consumed: %s: %v", path, statErr)
		} else {
			smbRegressionBytes(t, path, body)
			if !move || !selected {
				info, statErr := os.Lstat(path)
				require.NoError(t, statErr)
				require.True(t, f.pins[name].matches(afero.NewOsFs(), path, info), "retained source identity: %s", path)
			}
		}
	}
	for _, name := range []string{"movie.ass", "ABC-1234.srt", "keep.txt"} {
		if sourceKind != "unc_inplace" || name == "movie.ass" {
			_, statErr := os.Stat(filepath.Join(f.targetDir, name))
			require.True(t, os.IsNotExist(statErr), "excluded files must not publish: %s", name)
		}
	}
	if rollback {
		for _, name := range []string{"movie.mkv", "movie-cd2.mkv", "movie.srt", "movie.nfo"} {
			_, statErr := os.Stat(filepath.Join(f.targetDir, name))
			require.True(t, os.IsNotExist(statErr), "rollback must remove this transaction's output: %s", name)
		}
	}
	smbRegressionNoStage(t, f.root, f.localRoot)
}

func smbRegressionForeign(t *testing.T, unc, local, occupant string, sameBytes bool) {
	t.Helper()
	f := smbRegressionFixtureFor(t, unc, local, "unc_to_unc", false, organizer.LinkModeNone)
	f.cmd.Movie, f.cmd.PublicationFence = &f.movie, postPublishFence{movie: &f.movie}
	name := "movie.mkv"
	if occupant == "subtitle" {
		name = "movie.srt"
	}
	foreign := filepath.Join(f.targetDir, name)
	foreignBody := "foreign bytes must survive"
	if sameBytes {
		original := "ABC-123 original.mkv"
		if occupant == "subtitle" {
			original = "ABC-123 original.srt"
		}
		foreignBody = f.bodies[original]
	}
	fired := false
	var foreignIdentity artifactSourceIdentity
	org, ok := f.orch.organizer.(*organizer.Organizer)
	require.True(t, ok)
	// Inject only a competing file after planning. All I/O and publication
	// still use real OsFs UNC operations; no SMB failures are simulated.
	f.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: org, preExecute: func(*organizer.OrganizePlan) {
		fired = true
		require.NoError(t, os.MkdirAll(f.targetDir, 0o700))
		require.NoError(t, os.WriteFile(foreign, []byte(foreignBody), 0o600))
		info, err := os.Lstat(foreign)
		require.NoError(t, err)
		foreignIdentity = captureArtifactSourceIdentity(afero.NewOsFs(), foreign, info)
		require.True(t, foreignIdentity.hasDevIno, "capture the foreign SMB object identity eagerly")
	}}
	result, err := f.orch.Execute(t.Context(), f.cmd)
	require.True(t, fired)
	if sameBytes {
		// A no-op or refusal can both preserve a foreign same-byte occupant.
		// Strong identity below, not this outcome, proves it was not replaced.
		t.Logf("same-byte %s publication outcome: %v", occupant, err)
	} else if occupant == "primary" {
		require.Error(t, err, "late foreign primary must refuse an unforced overwrite")
	} else {
		require.NoError(t, err)
		require.NotNil(t, result.OrganizeResult)
		require.Len(t, result.OrganizeResult.Subtitles, 1)
		require.True(t, result.OrganizeResult.Subtitles[0].Skipped)
		smbRegressionBytes(t, filepath.Join(f.targetDir, "movie.mkv"), f.bodies["ABC-123 original.mkv"])
	}
	smbRegressionBytes(t, foreign, foreignBody)
	foreignInfo, statErr := os.Lstat(foreign)
	require.NoError(t, statErr)
	require.True(t, foreignIdentity.matches(afero.NewOsFs(), foreign, foreignInfo), "retain the exact foreign object, not just equal bytes")
	for name, body := range f.bodies {
		path := filepath.Join(filepath.Dir(f.source), name)
		smbRegressionBytes(t, path, body)
		info, statErr := os.Lstat(path)
		require.NoError(t, statErr)
		require.True(t, f.pins[name].matches(afero.NewOsFs(), path, info), "foreign claimant must not consume or replace source %s", path)
	}
	smbRegressionNoStage(t, f.root, f.localRoot)
}

func smbRegressionBytes(t *testing.T, path, want string) {
	t.Helper()
	info, err := os.Lstat(path)
	require.NoError(t, err, path)
	require.True(t, info.Mode().IsRegular(), "expected regular synthetic file: %s", path)
	got, err := os.ReadFile(path)
	require.NoError(t, err, path)
	require.Equal(t, want, string(got), path)
}

func smbRegressionNoStage(t *testing.T, roots ...string) {
	t.Helper()
	for _, root := range roots {
		require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if strings.HasPrefix(entry.Name(), ".javinizer-") {
				return fmt.Errorf("transaction residue at %s", path)
			}
			return nil
		}))
	}
}

func smbRegressionProbeLink(t *testing.T, source, probe string, mode organizer.LinkMode) {
	t.Helper()
	var err error
	if mode == organizer.LinkModeHard {
		err = os.Link(source, probe)
	} else {
		err = os.Symlink(source, probe)
	}
	if err == nil {
		defer func() { require.NoError(t, os.Remove(probe)) }()
		_, err = os.ReadFile(probe) // Remote-to-remote symlink evaluation may be disabled.
	}
	if err != nil {
		for _, capabilityError := range []error{windows.ERROR_NOT_SUPPORTED, windows.ERROR_INVALID_FUNCTION, windows.ERROR_PRIVILEGE_NOT_HELD, windows.ERROR_SYMLINK_CLASS_DISABLED} {
			if errors.Is(err, capabilityError) {
				t.Skipf("OPTIONAL SMB %s links unavailable under unchanged Windows security defaults: %v", mode, err)
			}
		}
		require.NoError(t, err, "unexpected SMB link-probe error must not become a capability skip")
	}
	smbRegressionAssertLink(t, source, probe, mode)
}

func smbRegressionAssertLink(t *testing.T, source, target string, mode organizer.LinkMode) {
	t.Helper()
	if mode == organizer.LinkModeSoft {
		got, err := os.Readlink(target)
		require.NoError(t, err)
		want, err := organizer.SymlinkLinkTarget(source)
		require.NoError(t, err)
		require.Equal(t, want, got)
	} else {
		sourceInfo, err := os.Stat(source)
		require.NoError(t, err)
		targetInfo, err := os.Stat(target)
		require.NoError(t, err)
		require.True(t, os.SameFile(sourceInfo, targetInfo), "hard-link mode must preserve native object identity")
	}
}
