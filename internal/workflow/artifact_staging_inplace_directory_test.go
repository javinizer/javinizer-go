package workflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
)

func TestInPlaceDedicatedDirectoryHistoryRoundTrip(t *testing.T) {
	ctx := context.Background()
	fs := afero.NewOsFs()
	db, _ := pr260ArtifactDB(t)
	movie := pr260FencedMovie(t, db, "native-directory-history", "")
	input := filepath.Join(t.TempDir(), "incoming")
	require.NoError(t, fs.MkdirAll(filepath.Join(input, "extras"), 0o755))
	source := filepath.Join(input, movie.ID+" - original.mkv")
	subtitle := filepath.Join(input, movie.ID+" - original.en.srt")
	for path, body := range map[string]string{source: "video", subtitle: "subtitle", filepath.Join(input, "cover.jpg"): "existing artwork", filepath.Join(input, "extras", "bonus.mkv"): "existing extra"} {
		require.NoError(t, afero.WriteFile(fs, path, []byte(body), 0o644))
	}
	m, err := matcher.NewMatcher(&matcher.Config{})
	require.NoError(t, err)
	orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, false)
	orch.organizer = organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "renamed-folder", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlace, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), m)
	repo := database.NewBatchFileOperationRepository(db)
	orch.revertLog = NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "directory-history-job", fs, template.NewEngine(), nil, nil)
	match := models.FileMatchInfo{Path: source, Name: filepath.Base(source), MovieID: movie.ID, Extension: ".mkv"}
	cmd := pr260FencedCommand(&movie, match, input, pr260FencedCounter(t, db), operationmode.OperationModeInPlace, false, true, organizer.LinkModeNone, false, true)
	result, err := orch.Execute(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result.OrganizeResult.InPlaceRenamed)
	operations, err := repo.FindByBatchJobID(ctx, "directory-history-job")
	require.NoError(t, err)
	require.Len(t, operations, 1)
	require.True(t, operations[0].InPlaceRenamed)
	reverted, err := history.NewReverter(fs, repo).RevertBatch(ctx, "directory-history-job")
	require.NoError(t, err)
	require.Equal(t, 1, reverted.Succeeded)
	require.FileExists(t, source)
	require.FileExists(t, subtitle)
	require.FileExists(t, filepath.Join(input, "cover.jpg"))
	require.FileExists(t, filepath.Join(input, "extras", "bonus.mkv"))
	require.NoDirExists(t, filepath.Join(filepath.Dir(input), "renamed-folder"))
	require.NoFileExists(t, filepath.Join(input, movie.ID+".nfo"))
}

func TestInPlaceDedicatedDirectoryNativeLifecycle(t *testing.T) {
	for _, canonical := range []bool{false, true} {
		for _, failCommit := range []bool{false, true} {
			name := "renamed"
			if canonical {
				name = "unchanged"
			}
			if failCommit {
				name += "/rollback"
			} else {
				name += "/success"
			}
			t.Run(name, func(t *testing.T) {
				fs := afero.NewOsFs()
				input := filepath.Join(t.TempDir(), "incoming")
				targetDir := filepath.Join(filepath.Dir(input), "renamed-folder")
				require.NoError(t, fs.MkdirAll(filepath.Join(input, "extras"), 0o755))
				movie := models.Movie{ID: "IPX-123", ContentID: "dedicated-directory-lifecycle", RenderGeneration: 1}
				stem := movie.ID
				if !canonical {
					stem += " - original"
				}
				source := filepath.Join(input, stem+".mkv")
				part := filepath.Join(input, stem+"-pt2.mkv")
				subtitle := filepath.Join(input, stem+".en.srt")
				for path, body := range map[string]string{source: "primary", part: "part", subtitle: "subtitle", filepath.Join(input, "cover.jpg"): "existing artwork", filepath.Join(input, "movie.nfo"): "existing NFO", filepath.Join(input, "extras", "bonus.mkv"): "extra"} {
					require.NoError(t, afero.WriteFile(fs, path, []byte(body), 0o644))
				}
				pins := make(map[string]os.FileInfo)
				for key, path := range map[string]string{"directory": input, "primary": source, "part": part, "subtitle": subtitle} {
					info, err := fs.Stat(path)
					require.NoError(t, err)
					require.True(t, os.SameFile(info, info))
					pins[key] = info
				}
				m, err := matcher.NewMatcher(&matcher.Config{})
				require.NoError(t, err)
				org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "renamed-folder", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeInPlace, MoveSubtitles: true, SubtitleExtensions: []string{".srt"}}, template.NewEngine(), m)
				orch := &applyOrchImpl{fs: fs, organizer: org}
				commitErr := errors.New("commit denied")
				fence := postPublishFence{movie: &movie}
				if failCommit {
					fence.err = commitErr
				}
				cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: source, Name: filepath.Base(source), MovieID: movie.ID, Extension: ".mkv"}, DestPath: input, OperationMode: operationmode.OperationModeInPlace, Organize: OrganizeOptions{MoveFiles: true}, PublicationFence: fence}
				stage, _, err := orch.prepareArtifact(context.Background(), cmd)
				require.NoError(t, err)
				defer stage.cleanup()
				require.NoError(t, afero.WriteFile(fs, filepath.Join(stage.root, movie.ID+".nfo"), []byte("new metadata"), 0o644))
				state := &applyPipelineState{organizeResult: &organizer.OrganizeResult{NewPath: stage.stagedSource}}
				err = stage.publish(context.Background(), orch, state, nil)
				expectedDir := targetDir
				expected := map[string]string{"directory": targetDir, "primary": filepath.Join(targetDir, movie.ID+".mkv"), "part": filepath.Join(targetDir, movie.ID+"-pt2.mkv"), "subtitle": filepath.Join(targetDir, movie.ID+".en.srt")}
				if failCommit {
					require.ErrorIs(t, err, commitErr)
					require.Equal(t, commitErr, err, "the entire publication must compensate without an additional rollback error")
					expectedDir = input
					expected = map[string]string{"directory": input, "primary": source, "part": part, "subtitle": subtitle}
					require.NoDirExists(t, targetDir)
					require.NoFileExists(t, filepath.Join(input, movie.ID+".nfo"))
				} else {
					require.NoError(t, err)
					require.NoDirExists(t, input)
					require.FileExists(t, filepath.Join(targetDir, movie.ID+".nfo"))
					require.True(t, state.organizeResult.InPlaceRenamed)
					require.Equal(t, source, state.organizeResult.OriginalPath)
					require.Equal(t, input, state.organizeResult.OldDirectoryPath)
					require.Equal(t, targetDir, state.organizeResult.NewDirectoryPath)
					if !canonical {
						require.Len(t, state.organizeResult.Subtitles, 1)
						sr := state.organizeResult.Subtitles[0]
						require.True(t, sr.Moved)
						require.Equal(t, subtitle, sr.OriginalPath)
						require.Equal(t, targetDir, filepath.Dir(sr.NewPath))
						expected["subtitle"] = sr.NewPath
					}
				}
				for key, path := range expected {
					info, err := fs.Stat(path)
					require.NoError(t, err, key)
					require.True(t, os.SameFile(pins[key], info), key)
				}
				for name, body := range map[string]string{"cover.jpg": "existing artwork", "movie.nfo": "existing NFO", filepath.Join("extras", "bonus.mkv"): "extra"} {
					bytes, err := afero.ReadFile(fs, filepath.Join(expectedDir, name))
					require.NoError(t, err)
					require.Equal(t, body, string(bytes))
				}
			})
		}
	}
}
