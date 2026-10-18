package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdversarialPrimaryModeCrossProduct(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeOrganize, operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, move := range []bool{false, true} {
			for _, link := range []organizer.LinkMode{organizer.LinkModeNone, organizer.LinkModeHard, organizer.LinkModeSoft} {
				for _, resident := range []bool{false, true} {
					for _, outcome := range []string{"success", "pre-reject", "post-reject"} {
						t.Run(fmt.Sprintf("%s/move=%v/link=%s/resident=%v/%s", mode, move, link, resident, outcome), func(t *testing.T) {
							fs := afero.NewOsFs()
							root := t.TempDir()
							incoming := filepath.Join(root, "incoming")
							library := filepath.Join(root, "library")
							dest := library
							folder := "movie"
							leaf := "ABC-123 original.mkv"
							if resident {
								leaf = "ABC-123.mkv"
							}
							if mode == operationmode.OperationModeOrganize {
								if resident {
									incoming = filepath.Join(library, folder)
								}
							} else {
								dest = incoming
								folder = "renamed"
								if resident {
									folder = "incoming"
								}
							}
							require.NoError(t, os.MkdirAll(incoming, 0755))
							source := filepath.Join(incoming, leaf)
							require.NoError(t, os.WriteFile(source, []byte("original primary"), 0644))
							unrelated := filepath.Join(root, "unrelated.txt")
							require.NoError(t, os.WriteFile(unrelated, []byte("foreign"), 0644))
							sourceInfo, e := os.Lstat(source)
							require.NoError(t, e)
							pin := captureArtifactSourceIdentity(fs, source, sourceInfo)
							require.True(t, pin.hasDevIno)
							if mode == operationmode.OperationModeOrganize && !move && link != organizer.LinkModeNone && !resident {
								if !pr260LinkSupported(t, link, source) {
									return
								}
							}
							movie := models.Movie{ID: "ABC-123", ContentID: "mode-cross-product", Title: "Mode matrix", RenderGeneration: 1}
							m, e := matcher.NewMatcher(&matcher.Config{})
							require.NoError(t, e)
							org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: folder, FileFormat: "<ID>", RenameFile: true, OperationMode: mode}, template.NewEngine(), m)
							match := models.FileMatchInfo{Path: source, Name: leaf, Extension: ".mkv", MovieID: movie.ID}
							plan, e := org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: match, Movie: &movie, DestDir: dest, MoveFiles: move, LinkMode: link, OperationMode: mode})
							require.NoError(t, e)
							target := plan.TargetPath
							orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, !resident)
							orch.organizer = org
							cmd := ApplyCmd{Movie: &movie, PublicationFence: postPublishFence{movie: &movie}, Match: match, DestPath: dest, Organize: OrganizeOptions{MoveFiles: move, LinkMode: link}, GenerateNFO: !resident, OperationMode: mode}
							if outcome == "pre-reject" {
								cmd.PublicationFence = &pr260FiniteAuthorityFence{authoritative: &movie, result: os.ErrPermission}
							}
							if outcome == "post-reject" {
								cmd.PublicationFence = postPublishFence{movie: &movie, err: os.ErrPermission}
							}
							result, err := orch.Execute(t.Context(), cmd)
							if outcome == "success" {
								assert.NoError(t, err)
							} else {
								assert.ErrorIs(t, err, os.ErrPermission)
							}
							preserve := outcome != "success" || !move || resident
							if preserve {
								info, e := os.Lstat(source)
								if assert.NoError(t, e, "original primary survives") {
									assert.True(t, pin.matches(fs, source, info), "original source identity retained")
									b, e := os.ReadFile(source)
									assert.NoError(t, e)
									assert.Equal(t, "original primary", string(b))
								}
							} else {
								assert.NoFileExists(t, source)
							}
							if outcome == "success" && err == nil {
								require.NotNil(t, result.OrganizeResult)
								assert.Equal(t, target, result.OrganizeResult.NewPath)
								b, e := os.ReadFile(target)
								assert.NoError(t, e)
								assert.Equal(t, "original primary", string(b))
								info, e := os.Lstat(target)
								require.NoError(t, e)
								if mode == operationmode.OperationModeOrganize && !move && link == organizer.LinkModeSoft && !resident {
									assert.NotZero(t, info.Mode()&os.ModeSymlink)
								} else {
									assert.True(t, info.Mode().IsRegular())
								}
								if mode == operationmode.OperationModeOrganize && !move && link == organizer.LinkModeHard && !resident {
									assert.True(t, pin.matches(fs, target, info), "hardlink shares admitted object")
								}
								if !resident {
									assert.FileExists(t, result.NFOPath)
								}
							} else if source != target {
								assert.NoFileExists(t, target, "refused apply publishes no primary")
							}
							b, e := os.ReadFile(unrelated)
							assert.NoError(t, e)
							assert.Equal(t, "foreign", string(b))
							require.NoError(t, filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
								if err != nil {
									return err
								}
								assert.False(t, strings.HasPrefix(d.Name(), artifactStageDirPrefix), "stage cleanup: %s", path)
								if outcome != "success" && !d.IsDir() && strings.HasSuffix(path, ".nfo") {
									t.Errorf("refused metadata remains: %s", path)
								}
								return nil
							}))
						})
					}
				}
			}
		}
	}
}

func TestAdversarialSidecarModeCrossProduct(t *testing.T) {
	for _, mode := range []operationmode.OperationMode{operationmode.OperationModeOrganize, operationmode.OperationModeInPlace, operationmode.OperationModeInPlaceNoRenameFolder} {
		for _, move := range []bool{false, true} {
			for _, link := range []organizer.LinkMode{organizer.LinkModeNone, organizer.LinkModeHard, organizer.LinkModeSoft} {
				for _, outcome := range []string{"success", "pre-reject", "post-reject"} {
					t.Run(fmt.Sprintf("%s/move=%v/link=%s/%s", mode, move, link, outcome), func(t *testing.T) {
						fs := afero.NewOsFs()
						root := t.TempDir()
						dir := filepath.Join(root, "incoming")
						require.NoError(t, os.Mkdir(dir, 0755))
						source := filepath.Join(dir, "ABC-123 original.mkv")
						sub := filepath.Join(dir, "ABC-123 original.en.srt")
						part := filepath.Join(dir, "ABC-123 original-cd2.mkv")
						before := map[string]string{source: "video", sub: "subtitle", part: "multipart"}
						pins := map[string]artifactSourceIdentity{}
						for p, c := range before {
							require.NoError(t, os.WriteFile(p, []byte(c), 0644))
							info, e := os.Lstat(p)
							require.NoError(t, e)
							pins[p] = captureArtifactSourceIdentity(fs, p, info)
							require.True(t, pins[p].hasDevIno)
						}
						if mode == operationmode.OperationModeOrganize && !move && link != organizer.LinkModeNone {
							if !pr260LinkSupported(t, link, source) {
								return
							}
						}
						movie := models.Movie{ID: "ABC-123", ContentID: "sidecar-mode-matrix", RenderGeneration: 1}
						m, e := matcher.NewMatcher(&matcher.Config{})
						require.NoError(t, e)
						org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "renamed", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}, OperationMode: mode}, template.NewEngine(), m)
						orch := &applyOrchImpl{fs: fs, organizer: org}
						dest := dir
						if mode == operationmode.OperationModeOrganize {
							dest = filepath.Join(root, "library")
						}
						match := models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}
						plan, e := org.PlanOrganize(t.Context(), organizer.OrganizeCmd{Match: match, Movie: &movie, DestDir: dest, MoveFiles: move, LinkMode: link, OperationMode: mode})
						require.NoError(t, e)
						cmd := ApplyCmd{Movie: &movie, PublicationFence: postPublishFence{movie: &movie}, Match: match, DestPath: dest, Organize: OrganizeOptions{MoveFiles: move, LinkMode: link}, OperationMode: mode}
						if outcome == "pre-reject" {
							cmd.PublicationFence = &pr260FiniteAuthorityFence{authoritative: &movie, result: os.ErrPermission}
						}
						if outcome == "post-reject" {
							cmd.PublicationFence = postPublishFence{movie: &movie, err: os.ErrPermission}
						}
						result, e := orch.Execute(t.Context(), cmd)
						if outcome == "success" {
							assert.NoError(t, e)
						} else {
							assert.ErrorIs(t, e, os.ErrPermission)
						}
						for p, c := range before {
							if outcome != "success" || !move {
								info, err := os.Lstat(p)
								if assert.NoError(t, err, "retain source %s", p) {
									assert.True(t, pins[p].matches(fs, p, info))
									body, err := os.ReadFile(p)
									assert.NoError(t, err)
									assert.Equal(t, c, string(body))
								}
							} else {
								assert.NoFileExists(t, p)
							}
						}
						if outcome == "success" && e == nil {
							require.NotNil(t, result.OrganizeResult)
							assert.Equal(t, plan.TargetPath, result.OrganizeResult.NewPath)
							subtitleBody, subtitleErr := os.ReadFile(filepath.Join(plan.TargetDir, "ABC-123.eng.srt"))
							assert.NoError(t, subtitleErr)
							assert.Equal(t, "subtitle", string(subtitleBody))
							if mode != operationmode.OperationModeOrganize || move || link == organizer.LinkModeNone {
								for name, content := range map[string]string{"ABC-123.mkv": "video", "ABC-123.eng.srt": "subtitle", "ABC-123-cd2.mkv": "multipart"} {
									body, e := os.ReadFile(filepath.Join(plan.TargetDir, name))
									assert.NoError(t, e)
									assert.Equal(t, content, string(body))
								}
							}
						} else {
							for _, name := range []string{"ABC-123.mkv", "ABC-123.eng.srt", "ABC-123-cd2.mkv"} {
								assert.NoFileExists(t, filepath.Join(plan.TargetDir, name))
							}
						}
					})
				}
			}
		}
	}
}

func TestAdversarialNonOrganizeModeCrossProduct(t *testing.T) {
	for _, kind := range []string{"metadata", "metadata-noop", "preview"} {
		for _, move := range []bool{false, true} {
			for _, link := range []organizer.LinkMode{organizer.LinkModeNone, organizer.LinkModeHard, organizer.LinkModeSoft} {
				for _, outcome := range []string{"success", "pre-reject", "post-reject"} {
					t.Run(fmt.Sprintf("%s/move=%v/link=%s/%s", kind, move, link, outcome), func(t *testing.T) {
						fs := afero.NewOsFs()
						root := t.TempDir()
						source := filepath.Join(root, "ABC-123 original.mkv")
						require.NoError(t, os.WriteFile(source, []byte("video"), 0644))
						info, e := os.Lstat(source)
						require.NoError(t, e)
						pin := captureArtifactSourceIdentity(fs, source, info)
						require.True(t, pin.hasDevIno)
						movie := models.Movie{ID: "ABC-123", ContentID: "non-organize-matrix", Title: "metadata", RenderGeneration: 1}
						orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, kind == "metadata")
						cmd := ApplyCmd{Movie: &movie, PublicationFence: postPublishFence{movie: &movie}, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mkv", MovieID: movie.ID}, DestPath: root, Organize: OrganizeOptions{Skip: true, MoveFiles: move, LinkMode: link}, OperationMode: operationmode.OperationModeMetadataArtwork, GenerateNFO: kind == "metadata"}
						if kind == "preview" {
							cmd.OperationMode = operationmode.OperationModePreview
							cmd.DryRun = true
						}
						if outcome == "pre-reject" {
							cmd.PublicationFence = &pr260FiniteAuthorityFence{authoritative: &movie, result: os.ErrPermission}
						}
						if outcome == "post-reject" {
							cmd.PublicationFence = postPublishFence{movie: &movie, err: os.ErrPermission}
						}
						result, e := orch.Execute(t.Context(), cmd)
						if kind == "metadata" && outcome != "success" {
							assert.ErrorIs(t, e, os.ErrPermission)
						} else {
							assert.NoError(t, e)
						}
						current, e := os.Lstat(source)
						require.NoError(t, e)
						assert.True(t, pin.matches(fs, source, current))
						body, e := os.ReadFile(source)
						assert.NoError(t, e)
						assert.Equal(t, "video", string(body))
						if kind == "metadata" && outcome == "success" {
							assert.FileExists(t, result.NFOPath)
						} else {
							require.NoError(t, filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
								if e != nil {
									return e
								}
								if !d.IsDir() {
									assert.Equal(t, source, path)
								}
								return nil
							}))
						}
					})
				}
			}
		}
	}
}
