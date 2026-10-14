package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestResidentPrimaryCaseAliasRepeatAndUndo(t *testing.T) {
	for _, native := range []bool{false, true} {
		for _, move := range []bool{false, true} {
			for _, subtitle := range []bool{false, true} {
				t.Run(fmt.Sprintf("native=%v/move=%v/subtitle=%v", native, move, subtitle), func(t *testing.T) {
					if native && runtime.GOOS != "windows" {
						t.Skip("native case-insensitive Windows filesystem counterpart; portable folding runs everywhere")
					}
					root := t.TempDir()
					var fs afero.Fs = afero.NewOsFs()
					if !native {
						fs = &stagedCaseFoldFS{Fs: fs, root: root}
					}
					library := filepath.Join(root, "library")
					dir := filepath.Join(library, "abc-123")
					source := filepath.Join(dir, "abc-123.mp4")
					sub := filepath.Join(dir, "abc-123.en.srt")
					part := filepath.Join(dir, "abc-123-cd2.mp4")
					target := filepath.Join(library, "ABC-123", "ABC-123.mp4")
					subTarget := filepath.Join(library, "ABC-123", "ABC-123.eng.srt")
					require.NoError(t, fs.MkdirAll(dir, 0755))
					require.NoError(t, afero.WriteFile(fs, source, []byte("resident video"), 0644))
					require.NoError(t, afero.WriteFile(fs, part, []byte("resident multipart"), 0644))
					partInfo, err := fs.Stat(part)
					require.NoError(t, err)
					if subtitle {
						require.NoError(t, afero.WriteFile(fs, sub, []byte("resident subtitle"), 0644))
					}
					original, err := fs.Stat(source)
					require.NoError(t, err)
					targetInfo, err := fs.Stat(target)
					require.NoError(t, err)
					require.True(t, os.SameFile(original, targetInfo), "fixture requires actual native file identity shared by both names")
					db, _ := pr260ArtifactDB(t)
					repo := database.NewBatchFileOperationRepository(db)
					movie := models.Movie{ID: "ABC-123", ContentID: "resident-case-alias"}
					engine := template.NewEngine()
					org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: operationmode.OperationModeOrganize}, engine, nil)
					orch := newApplyOrchestrator(fs, org, nil, nil, nil, ApplyConfig{}, engine, NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "resident-alias", fs, nil, nil, nil), nil, nil)
					cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: move}, OperationMode: operationmode.OperationModeOrganize, PublicationFence: postPublishFence{movie: &movie}}
					for attempt := 0; attempt < 2; attempt++ {
						result, err := orch.Execute(context.Background(), cmd)
						require.NoError(t, err)
						require.NotNil(t, result.OrganizeResult)
						require.False(t, result.OrganizeResult.Moved, "resident primary must not be claimed as installed")
						info, e := fs.Stat(source)
						require.NoError(t, e)
						require.True(t, os.SameFile(original, info))
						currentPart, partErr := fs.Stat(part)
						require.NoError(t, partErr)
						require.True(t, os.SameFile(partInfo, currentPart))
						got, e := afero.ReadFile(fs, target)
						require.NoError(t, e)
						require.Equal(t, "resident video", string(got))
						ledger := p3Ledger(t, repo, result.OperationID)
						require.True(t, ledger.PrimaryResident)
						require.Empty(t, ledger.Replacements)
						require.Empty(t, ledger.PlannedDeletes)
						for _, p := range ledger.Delete {
							require.NotEqual(t, filepath.Clean(target), filepath.Clean(p), "resident primary cannot be a delete")
						}
						for _, mv := range ledger.MoveBack {
							require.NotEqual(t, filepath.Clean(target), filepath.Clean(mv.NewPath), "resident primary cannot be an inverse move")
						}
						if subtitle {
							body, e := afero.ReadFile(fs, subTarget)
							require.NoError(t, e)
							require.Equal(t, "resident subtitle", string(body))
						}
						entries, e := afero.ReadDir(fs, dir)
						require.NoError(t, e)
						for _, entry := range entries {
							require.False(t, stagedBusyReservation(entry.Name()), "busy marker leaked")
						}
					}
					reverted, err := history.NewReverter(fs, repo).RevertBatch(context.Background(), "resident-alias")
					require.NoError(t, err)
					require.Zero(t, reverted.Failed)
					require.Equal(t, 2, reverted.Succeeded)
					got, err := afero.ReadFile(fs, source)
					require.NoError(t, err)
					require.Equal(t, "resident video", string(got))
					info, err := fs.Stat(source)
					require.NoError(t, err)
					require.True(t, os.SameFile(original, info))
					currentPart, partErr := fs.Stat(part)
					require.NoError(t, partErr)
					require.True(t, os.SameFile(partInfo, currentPart))
					if subtitle {
						got, err := afero.ReadFile(fs, sub)
						require.NoError(t, err)
						require.Equal(t, "resident subtitle", string(got))
					}
				})
			}
		}
	}
}

type residentAliasLateExecutor struct {
	*organizer.Organizer
	before func()
}

func (o *residentAliasLateExecutor) ExecuteOrganizePlan(plan *organizer.OrganizePlan, move bool, link organizer.LinkMode) (*organizer.OrganizeResult, error) {
	if o.before != nil {
		fn := o.before
		o.before = nil
		fn()
	}
	return o.Organizer.ExecuteOrganizePlan(plan, move, link)
}

func TestResidentPrimaryAliasLateTargetSubstitution(t *testing.T) {
	for _, move := range []bool{false, true} {
		for _, kind := range []string{"removed", "foreign", "symlink"} {
			t.Run(fmt.Sprintf("move=%v/%s", move, kind), func(t *testing.T) {
				fs := afero.NewOsFs()
				root := t.TempDir()
				library := filepath.Join(root, "library")
				source := filepath.Join(root, "source.mp4")
				target := filepath.Join(library, "ABC-123", "ABC-123.mp4")
				require.NoError(t, fs.MkdirAll(filepath.Dir(target), 0755))
				require.NoError(t, afero.WriteFile(fs, source, []byte("resident source"), 0644))
				if err := os.Link(source, target); err != nil {
					t.Skipf("hardlink fixture unsupported: %v", err)
				}
				original, err := fs.Stat(source)
				require.NoError(t, err)
				db, _ := pr260ArtifactDB(t)
				repo := database.NewBatchFileOperationRepository(db)
				movie := models.Movie{ID: "ABC-123", ContentID: "resident-late-alias"}
				engine := template.NewEngine()
				org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, engine, nil)
				foreign := filepath.Join(root, "foreign.mp4")
				require.NoError(t, afero.WriteFile(fs, foreign, []byte("foreign bytes"), 0644))
				var substituted os.FileInfo
				if kind == "symlink" {
					probe := filepath.Join(root, "symlink-probe")
					if err := os.Symlink(foreign, probe); err != nil {
						t.Skipf("symlink fixture unsupported: %v", err)
					}
					require.NoError(t, os.Remove(probe))
				}
				late := &residentAliasLateExecutor{Organizer: org, before: func() {
					require.NoError(t, fs.Remove(target))
					switch kind {
					case "foreign":
						require.NoError(t, afero.WriteFile(fs, target, []byte("foreign target"), 0644))
					case "symlink":
						require.NoError(t, os.Symlink(foreign, target))
					}
					substituted, _ = os.Lstat(target)
				}}
				orch := newApplyOrchestrator(fs, late, nil, nil, nil, ApplyConfig{}, engine, NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "resident-late", fs, nil, nil, nil), nil, nil)
				cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{MoveFiles: move}, OperationMode: operationmode.OperationModeOrganize, PublicationFence: postPublishFence{movie: &movie}}
				result, err := orch.Execute(context.Background(), cmd)
				require.Error(t, err, "a frozen resident may not turn into an unjournaled install")
				require.NotNil(t, result)
				ledger := p3Ledger(t, repo, result.OperationID)
				require.Empty(t, ledger.Replacements)
				require.Empty(t, ledger.MoveBack)
				require.Empty(t, ledger.Delete)
				require.Empty(t, ledger.PlannedDeletes)
				body, e := afero.ReadFile(fs, source)
				require.NoError(t, e)
				require.Equal(t, "resident source", string(body))
				info, e := fs.Stat(source)
				require.NoError(t, e)
				require.True(t, os.SameFile(original, info))
				if kind == "removed" {
					_, e = os.Lstat(target)
					require.True(t, os.IsNotExist(e))
				} else {
					info, e = os.Lstat(target)
					require.NoError(t, e)
					require.True(t, os.SameFile(substituted, info))
					body, e = afero.ReadFile(fs, target)
					require.NoError(t, e)
					want := "foreign target"
					if kind == "symlink" {
						want = "foreign bytes"
					}
					require.Equal(t, want, string(body))
				}
			})
		}
	}
}
