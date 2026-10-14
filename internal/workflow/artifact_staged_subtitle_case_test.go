package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type stagedCaseFoldFS struct {
	afero.Fs
	root string
}

func (f *stagedCaseFoldFS) p(p string) string {
	rel, e := filepath.Rel(f.root, p)
	if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Join(f.root, strings.ToLower(rel))
	}
	return p
}
func (f *stagedCaseFoldFS) Create(p string) (afero.File, error) { return f.Fs.Create(f.p(p)) }
func (f *stagedCaseFoldFS) Mkdir(p string, m os.FileMode) error { return f.Fs.Mkdir(f.p(p), m) }
func (f *stagedCaseFoldFS) MkdirAll(p string, m os.FileMode) error {
	return f.Fs.MkdirAll(f.p(p), m)
}
func (f *stagedCaseFoldFS) Open(p string) (afero.File, error) { return f.Fs.Open(f.p(p)) }
func (f *stagedCaseFoldFS) OpenFile(p string, flag int, m os.FileMode) (afero.File, error) {
	return f.Fs.OpenFile(f.p(p), flag, m)
}
func (f *stagedCaseFoldFS) Remove(p string) error              { return f.Fs.Remove(f.p(p)) }
func (f *stagedCaseFoldFS) RemoveAll(p string) error           { return f.Fs.RemoveAll(f.p(p)) }
func (f *stagedCaseFoldFS) Rename(a, b string) error           { return f.Fs.Rename(f.p(a), f.p(b)) }
func (f *stagedCaseFoldFS) Stat(p string) (os.FileInfo, error) { return f.Fs.Stat(f.p(p)) }
func (f *stagedCaseFoldFS) LstatIfPossible(p string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(f.p(p))
}
func (f *stagedCaseFoldFS) Chmod(p string, m os.FileMode) error { return f.Fs.Chmod(f.p(p), m) }
func (f *stagedCaseFoldFS) Chown(p string, u, g int) error      { return f.Fs.Chown(f.p(p), u, g) }
func (f *stagedCaseFoldFS) Chtimes(p string, a, m time.Time) error {
	return f.Fs.Chtimes(f.p(p), a, m)
}

// Exercise both a deterministic native-backed case-folding namespace and the
// host filesystem itself. Windows therefore runs the real case-insensitive
// self-skip path without a platform skip; Linux also covers distinct spellings.
func TestStagedSubtitleCaseSpelling(t *testing.T) {
	for _, folded := range []bool{false, true} {
		for _, language := range []string{"eng", "en"} {
			for _, extension := range []string{".srt", ".TTML"} {
				for _, outcome := range []string{"vacant", "late occupant", "rollback", "resident"} {
					name := "native"
					if folded {
						name = "case-folded"
					}
					t.Run(name+"/"+language+extension+"/"+outcome, func(t *testing.T) {
						env := setupCopyIntentE2E(t, "case-subtitle")
						incoming := filepath.Dir(env.source)
						for _, path := range []string{env.source, env.subtitle, env.multipart, env.unrelated} {
							require.NoError(t, env.fs.Remove(path))
						}
						fs := env.fs
						ext := extension
						if folded {
							fs = &stagedCaseFoldFS{Fs: fs, root: filepath.Dir(incoming)}
							ext = strings.ToLower(extension)
						}
						source := filepath.Join(incoming, "abc-123.mp4")
						subtitle := filepath.Join(incoming, "abc-123."+language+ext)
						require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0644))
						require.NoError(t, afero.WriteFile(fs, subtitle, []byte("subtitle"), 0644))
						env.movie.ID = "ABC-123"
						env.match = models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: env.movie.ID}
						env.fs, env.orch.fs = fs, fs
						env.orch.revertLog = NewDBRevertLog(env.repo, NewRevertLogConfig(true, nil), env.jobID, fs, nil, nil, nil)
						m, err := matcher.NewMatcher(&matcher.Config{})
						require.NoError(t, err)
						mode := operationmode.OperationModeInPlace
						finalDir := filepath.Join(filepath.Dir(incoming), "renamed")
						if outcome == "resident" {
							mode = operationmode.OperationModeInPlaceNoRenameFolder
							finalDir = incoming
						}
						target := filepath.Join(finalDir, "ABC-123.eng"+ext)
						org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "renamed", FileFormat: "<ID>", RenameFile: true, MoveSubtitles: true, SubtitleExtensions: []string{strings.ToLower(ext)}, OperationMode: mode}, template.NewEngine(), m)
						env.orch.organizer = org
						foreign := artifactSourceIdentity{}
						if outcome == "late occupant" {
							env.orch.organizer = &pr260PublicationFaultOrganizer{Organizer: org, preExecute: func(*organizer.OrganizePlan) {
								require.NoError(t, fs.MkdirAll(finalDir, 0755))
								require.NoError(t, afero.WriteFile(fs, target, []byte("foreign subtitle"), 0644))
								info, e := lstatArtifactSource(fs, target)
								require.NoError(t, e)
								foreign = captureArtifactSourceIdentity(fs, target, info)
								require.True(t, foreign.hasDevIno)
							}}
						}
						identities := map[string]artifactSourceIdentity{}
						for _, path := range []string{source, subtitle} {
							info, e := lstatArtifactSource(fs, path)
							require.NoError(t, e)
							identities[path] = captureArtifactSourceIdentity(fs, path, info)
							require.True(t, identities[path].hasDevIno)
						}
						resident := false
						if outcome == "resident" {
							if info, e := lstatArtifactSource(fs, target); e == nil {
								resident = identities[subtitle].matches(fs, target, info)
							}
						}
						cmd := pr260FencedCommand(&env.movie, env.match, incoming, pr260FencedCounter(t, env.db), mode, false, false, organizer.LinkModeNone, false, false)
						fence := postPublishFence{movie: &env.movie}
						if outcome == "rollback" {
							fence.err = os.ErrPermission
						}
						cmd.PublicationFence = fence
						cmd.Organize.ForceUpdate = false
						result, err := env.orch.Execute(t.Context(), cmd)
						if outcome == "rollback" {
							require.ErrorIs(t, err, os.ErrPermission)
							exists, e := afero.Exists(fs, target)
							require.NoError(t, e)
							require.False(t, exists)
						} else {
							require.NoError(t, err)
							require.Equal(t, finalDir, filepath.Dir(result.OrganizeResult.NewPath))
							require.Len(t, result.OrganizeResult.Subtitles, 1)
							sr := result.OrganizeResult.Subtitles[0]
							require.Equal(t, subtitle, sr.OriginalPath)
							require.Equal(t, target, sr.NewPath)
							// A resident canonical subtitle aliases the target on case-insensitive
							// hosts; a language-normalized source still needs an independent copy.
							wantCopied := outcome != "late occupant" && !resident
							require.Equal(t, wantCopied, sr.Copied)
							require.Equal(t, !wantCopied, sr.Skipped)
							data, e := afero.ReadFile(fs, target)
							require.NoError(t, e)
							want := "subtitle"
							if outcome == "late occupant" {
								want = "foreign subtitle"
							}
							require.Equal(t, want, string(data))
							ledger := p3Ledger(t, env.repo, result.OperationID)
							if !wantCopied {
								require.NotContains(t, ledger.Delete, target)
								for _, pin := range ledger.PlannedDeletes {
									require.NotEqual(t, target, pin.Path)
								}
							}
							_, err = history.NewReverter(fs, env.repo).RevertBatch(t.Context(), env.jobID)
							require.NoError(t, err)
							if wantCopied {
								exists, e := afero.Exists(fs, target)
								require.NoError(t, e)
								require.False(t, exists)
							}
							if outcome == "late occupant" {
								info, e := lstatArtifactSource(fs, target)
								require.NoError(t, e)
								require.True(t, foreign.matches(fs, target, info))
							}
						}
						for path, id := range identities {
							info, e := lstatArtifactSource(fs, path)
							require.NoError(t, e)
							require.True(t, id.matches(fs, path, info), "source identity changed: %s", path)
						}
						pr260AssertStageGone(t, fs, filepath.Dir(incoming))
						pr260AssertStageGone(t, fs, incoming)
					})
				}
			}
		}
	}
}

func TestStagedSubtitleOutputCaseExclusion(t *testing.T) {
	s := &artifactStage{stagedSubtitleOutputs: map[string]int{filepath.Join("stage", "ABC-123.eng.srt"): 0}}
	require.True(t, s.isStagedSubtitleOutput(filepath.Join("stage", "abc-123.eng.srt")))
	require.False(t, s.isStagedSubtitleOutput(filepath.Join("stage", "different.eng.srt")))
}
