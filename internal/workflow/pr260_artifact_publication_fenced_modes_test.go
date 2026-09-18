package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pr260CountingArtifactFencer struct {
	*database.MovieRepository
	calls     int32
	callbacks int32
}

func (f *pr260CountingArtifactFencer) WithApplyArtifactPublicationFence(ctx context.Context, contentID string, generation int64, fn func(*models.Movie) error) error {
	atomic.AddInt32(&f.calls, 1)
	return f.MovieRepository.WithApplyArtifactPublicationFence(ctx, contentID, generation, func(movie *models.Movie) error {
		atomic.AddInt32(&f.callbacks, 1)
		return fn(movie)
	})
}

func pr260FencedCounter(t *testing.T, db *database.DB) *pr260CountingArtifactFencer {
	real, ok := db.Repositories().MovieRepo.(*database.MovieRepository)
	require.True(t, ok)
	require.NotNil(t, real)
	return &pr260CountingArtifactFencer{MovieRepository: real}
}

func pr260FencedMovie(t *testing.T, db *database.DB, slug, posterURL string) models.Movie {
	t.Helper()
	actress := models.Actress{FirstName: "Luna", LastName: "Fenced", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&actress).Error)
	movie := models.Movie{ContentID: "pr260-fenced-" + slug, ID: "PR260-FENCED-" + strings.ToUpper(slug), Title: "Fenced Matrix Movie", Poster: models.PosterState{PosterURL: posterURL}, RenderDirty: true}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{actress}))
	movie.Actresses = []models.Actress{actress}
	return movie
}

func pr260FencedFiles(t *testing.T, slug string) (afero.Fs, string, string, string, string, string, models.FileMatchInfo) {
	t.Helper()
	fs := afero.NewOsFs()
	root := t.TempDir()
	sourceDir := filepath.Join(root, "incoming")
	base := "PR260-" + strings.ToUpper(slug)
	source := filepath.Join(sourceDir, base+".mp4")
	subtitle := filepath.Join(sourceDir, base+".srt")
	multipart := filepath.Join(sourceDir, base+"-cd2.mp4")
	unrelated := filepath.Join(sourceDir, "do-not-touch.txt")
	require.NoError(t, fs.MkdirAll(sourceDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, source, []byte("video"), 0o644))
	require.NoError(t, afero.WriteFile(fs, subtitle, []byte("subtitle"), 0o644))
	require.NoError(t, afero.WriteFile(fs, multipart, []byte("part two"), 0o644))
	require.NoError(t, afero.WriteFile(fs, unrelated, []byte("unrelated"), 0o644))
	match := models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4"}
	return fs, root, source, subtitle, multipart, unrelated, match
}

func pr260FencedCommand(movie *models.Movie, match models.FileMatchInfo, dest string, fencer *pr260CountingArtifactFencer, mode operationmode.OperationMode, skip, move bool, link organizer.LinkMode, download, generateNFO bool) ApplyCmd {
	return ApplyCmd{Movie: movie, PublicationFence: fencer, Match: match, DestPath: dest, Organize: OrganizeOptions{Skip: skip, MoveFiles: move, LinkMode: link, ForceUpdate: true}, Download: download, GenerateNFO: generateNFO, OperationMode: mode}
}

func pr260AssertStageGone(t *testing.T, fs afero.Fs, parent string) {
	t.Helper()
	entries, err := afero.ReadDir(fs, parent)
	require.NoError(t, err)
	for _, entry := range entries {
		assert.False(t, strings.HasPrefix(entry.Name(), ".javinizer-apply-"))
	}
}

func pr260AssertNoFinals(t *testing.T, fs afero.Fs, dest string) {
	t.Helper()
	entries, err := afero.ReadDir(fs, dest)
	if os.IsNotExist(err) {
		return
	}
	require.NoError(t, err)
	require.Empty(t, entries)
}

func pr260AssertCompleteArtifacts(t *testing.T, fs afero.Fs, result *ApplyResult, finalRoot string, poster []byte, title string) {
	t.Helper()
	require.NotNil(t, result)
	require.NotEmpty(t, result.NFOPath)
	require.NotEmpty(t, result.DownloadPaths)
	for _, path := range append([]string{result.NFOPath}, result.DownloadPaths...) {
		assert.NotContains(t, path, ".javinizer-apply-")
		rel, err := filepath.Rel(finalRoot, path)
		require.NoError(t, err)
		assert.False(t, rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)))
		exists, err := afero.Exists(fs, path)
		require.NoError(t, err)
		require.True(t, exists, path)
	}
	nfoBytes, err := afero.ReadFile(fs, result.NFOPath)
	require.NoError(t, err)
	assert.Contains(t, string(nfoBytes), title)
	posterBytes, err := afero.ReadFile(fs, result.DownloadPaths[0])
	require.NoError(t, err)
	assert.Equal(t, poster, posterBytes)
}

func pr260AssertFinalSidecars(t *testing.T, fs afero.Fs, video string) {
	t.Helper()
	dir := filepath.Dir(video)
	stem := strings.TrimSuffix(filepath.Base(video), filepath.Ext(video))
	for _, item := range []struct{ name, content string }{
		{name: stem + ".srt", content: "subtitle"},
		{name: stem + "-cd2.mp4", content: "part two"},
	} {
		path := filepath.Join(dir, item.name)
		bytes, err := afero.ReadFile(fs, path)
		require.NoError(t, err, path)
		assert.Equal(t, item.content, string(bytes))
	}
}

func pr260AssertRetained(t *testing.T, fs afero.Fs, source, subtitle, multipart, unrelated string) {
	t.Helper()
	for _, path := range []string{source, subtitle, multipart, unrelated} {
		exists, err := afero.Exists(fs, path)
		require.NoError(t, err)
		require.True(t, exists, path)
	}
	bytes, err := afero.ReadFile(fs, unrelated)
	require.NoError(t, err)
	assert.Equal(t, "unrelated", string(bytes))
}

func pr260AssertRemoved(t *testing.T, fs afero.Fs, paths ...string) {
	t.Helper()
	for _, path := range paths {
		exists, err := afero.Exists(fs, path)
		require.NoError(t, err)
		assert.False(t, exists, path)
	}
}

func pr260LinkSupported(t *testing.T, mode organizer.LinkMode, source string) bool {
	t.Helper()
	probe := source + ".pr260-link-probe"
	var err error
	if mode == organizer.LinkModeHard {
		err = os.Link(source, probe)
	} else {
		err = os.Symlink(source, probe)
	}
	if err != nil {
		t.Skipf("PENDING: %s unsupported by OS filesystem: %v", mode, err)
		return false
	}
	require.NoError(t, os.Remove(probe))
	return true
}

func pr260RunStaleGenerationOpenCollision(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	movie, _, _, _ := pr260ArtifactSeed(t, db)
	require.NoError(t, db.Model(&movie).Updates(map[string]interface{}{"render_generation": 1, "render_dirty": true}).Error)
	attempt := movie
	attempt.RenderGeneration = 0
	attempt.RenderDirty = true
	fs, root, source, _, _, unrelated, match := pr260FencedFiles(t, "stale")
	counter := pr260FencedCounter(t, db)
	orch := pr260RealApply(fs, &attempt, organizer.MediaFormatConfig{}, nil, false)
	cmd := pr260FencedCommand(&attempt, match, filepath.Join(root, "library"), counter, operationmode.OperationModeOrganize, false, true, organizer.LinkModeNone, false, false)
	_, err := orch.Execute(context.Background(), cmd)
	if err != nil {
		t.Logf("stale/open collision rejected: %v", err)
	}
	require.Positive(t, atomic.LoadInt32(&counter.calls))
	persisted, findErr := db.Repositories().MovieRepo.FindByID(context.Background(), attempt.ID)
	require.NoError(t, findErr)
	require.True(t, persisted.RenderDirty)
	pr260AssertRetained(t, fs, source, filepath.Join(filepath.Dir(source), "PR260-STALE.srt"), filepath.Join(filepath.Dir(source), "PR260-STALE-cd2.mp4"), unrelated)
	pr260AssertNoFinals(t, fs, filepath.Join(root, "library"))
	pr260AssertStageGone(t, fs, root)
}

func TestPR260RealArtifactFencedModes(t *testing.T) {
	poster := pr260PosterBytes(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(poster)
	}))
	defer server.Close()
	media := organizer.MediaFormatConfig{PosterFormat: "<ACTRESS>-poster.jpg"}
	tests := []struct {
		name string
		kind string
		mode operationmode.OperationMode
		skip bool
		move bool
		link organizer.LinkMode
	}{
		{name: "clean_organize_sidecars", kind: "organize", mode: operationmode.OperationModeOrganize, move: true, link: organizer.LinkModeNone},
		{name: "metadata_artwork_in_place", kind: "metadata", mode: operationmode.OperationModeMetadataArtwork, skip: true, move: true, link: organizer.LinkModeNone},
		{name: "copy_retains_source", kind: "copy", mode: operationmode.OperationModeOrganize, move: false, link: organizer.LinkModeNone},
		{name: "hardlink_retains_source", kind: "link", mode: operationmode.OperationModeOrganize, move: false, link: organizer.LinkModeHard},
		{name: "symlink_retains_source", kind: "link", mode: operationmode.OperationModeOrganize, move: false, link: organizer.LinkModeSoft},
		{name: "failure_cleans_stage", kind: "failure", mode: operationmode.OperationModeOrganize, move: true, link: organizer.LinkModeNone},
		{name: "cancel_cleans_stage", kind: "cancel", mode: operationmode.OperationModeOrganize, move: true, link: organizer.LinkModeNone},
		{name: "stale_generation_open_collision", kind: "stale", mode: operationmode.OperationModeOrganize, move: true, link: organizer.LinkModeNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.kind == "stale" {
				pr260RunStaleGenerationOpenCollision(t)
				return
			}
			db, _ := pr260ArtifactDB(t)
			movie := pr260FencedMovie(t, db, tt.name, server.URL+"/poster.jpg")
			fs, root, source, subtitle, multipart, unrelated, match := pr260FencedFiles(t, tt.name)
			finalRoot := filepath.Join(root, "library")
			if tt.kind == "metadata" {
				finalRoot = filepath.Dir(source)
			}
			counter := pr260FencedCounter(t, db)
			if tt.kind == "link" && !pr260LinkSupported(t, tt.link, source) {
				return
			}
			if tt.kind == "failure" {
				movie.Poster.PosterURL = ""
				failing := &pr260ArtifactCreateFailureFS{Fs: fs, fail: true}
				orch := pr260RealApply(failing, &movie, media, nil, true)
				cmd := pr260FencedCommand(&movie, match, finalRoot, counter, tt.mode, tt.skip, tt.move, tt.link, false, true)
				_, err := orch.Execute(context.Background(), cmd)
				require.Error(t, err)
				require.Positive(t, atomic.LoadInt32(&counter.calls))
				pr260AssertRetained(t, failing, source, subtitle, multipart, unrelated)
				pr260AssertStageGone(t, failing, root)
				return
			}
			orch := pr260RealApply(fs, &movie, media, server.Client(), true)
			cmd := pr260FencedCommand(&movie, match, finalRoot, counter, tt.mode, tt.skip, tt.move, tt.link, true, true)
			if tt.kind == "cancel" {
				ctx, cancel := context.WithCancel(context.Background())
				orch.artifactPrepared = cancel
				_, err := orch.Execute(ctx, cmd)
				cancel()
				require.Error(t, err)
				pr260AssertRetained(t, fs, source, subtitle, multipart, unrelated)
				pr260AssertNoFinals(t, fs, finalRoot)
				pr260AssertStageGone(t, fs, root)
				return
			}
			result, err := orch.Execute(context.Background(), cmd)
			require.NoError(t, err)
			require.Positive(t, atomic.LoadInt32(&counter.calls))
			require.Positive(t, atomic.LoadInt32(&counter.callbacks))
			if tt.kind == "metadata" {
				assert.False(t, result.Steps.Organized)
				assert.Nil(t, result.OrganizeResult)
				pr260AssertRetained(t, fs, source, subtitle, multipart, unrelated)
				pr260AssertCompleteArtifacts(t, fs, result, filepath.Dir(source), poster, movie.Title)
			} else {
				require.True(t, result.Steps.Organized)
				require.NotNil(t, result.OrganizeResult)
				video := result.OrganizeResult.NewPath
				require.NotContains(t, video, ".javinizer-apply-")
				require.Equal(t, movie.ID+".mp4", filepath.Base(video))
				exists, statErr := afero.Exists(fs, video)
				require.NoError(t, statErr)
				require.True(t, exists)
				pr260AssertCompleteArtifacts(t, fs, result, filepath.Dir(video), poster, movie.Title)
				if tt.kind == "organize" {
					pr260AssertRemoved(t, fs, source, subtitle, multipart)
				} else {
					pr260AssertRetained(t, fs, source, subtitle, multipart, unrelated)
				}
				if tt.kind == "organize" || tt.kind == "copy" {
					pr260AssertFinalSidecars(t, fs, video)
				}
				if tt.kind == "link" {
					sourceInfo, statErr := os.Stat(source)
					require.NoError(t, statErr)
					if tt.link == organizer.LinkModeHard {
						finalInfo, finalErr := os.Stat(video)
						require.NoError(t, finalErr)
						require.True(t, os.SameFile(sourceInfo, finalInfo))
					} else {
						linkInfo, linkErr := os.Lstat(video)
						require.NoError(t, linkErr)
						require.NotZero(t, linkInfo.Mode()&os.ModeSymlink)
						target, readErr := os.Readlink(video)
						require.NoError(t, readErr)
						if !filepath.IsAbs(target) {
							target = filepath.Join(filepath.Dir(video), target)
						}
						require.Equal(t, filepath.Clean(source), filepath.Clean(target))
					}
				}
			}
			pr260AssertStageGone(t, fs, root)
		})
	}
}
