package workflow

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pr260ArtifactBarrierFS struct {
	afero.Fs
	entered chan struct{}
	resume  chan struct{}
	once    sync.Once
	order   *int32
}

func (f *pr260ArtifactBarrierFS) Rename(oldPath, newPath string) error {
	if !strings.Contains(newPath, string(filepath.Separator)+".javinizer-apply-") {
		f.once.Do(func() { close(f.entered) })
		<-f.resume
	}
	err := f.Fs.Rename(oldPath, newPath)
	if err == nil && f.order != nil {
		atomic.AddInt32(f.order, 1)
	}
	return err
}

type pr260ArtifactCreateFailureFS struct {
	afero.Fs
	fail bool
}

func (f *pr260ArtifactCreateFailureFS) Create(name string) (afero.File, error) {
	if f.fail {
		f.fail = false
		return nil, errors.New("pr260 injected artifact create failure")
	}
	return f.Fs.Create(name)
}

func pr260ArtifactDB(t *testing.T) (*database.DB, string) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "pr260-artifact.sqlite")
	db, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })
	return db, dsn
}

func pr260ArtifactSeed(t *testing.T, db *database.DB) (models.Movie, models.CreditCollision, models.Actress, models.Actress) {
	t.Helper()
	source := models.Actress{FirstName: "Source", LastName: "Identity", Verified: true, Origin: "scrape"}
	target := models.Actress{FirstName: "Canonical", LastName: "Identity", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&source).Error)
	require.NoError(t, db.Create(&target).Error)
	movie := models.Movie{ContentID: "pr260-artifact", ID: "PR260-ARTIFACT", Title: "Artifact movie", Poster: models.PosterState{PosterURL: "http://poster.invalid/poster.jpg"}}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: source.ID, CreditedName: "Source Identity", Origin: "scrape"}
	require.NoError(t, db.Create(&credit).Error)
	require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{source}))
	collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: "Source Identity", CanonicalValue: "Canonical Identity", Status: models.CollisionStatusOpen, Occurrences: 1}
	require.NoError(t, db.Create(&collision).Error)
	movie.Actresses = []models.Actress{source}
	movie.Credits = []models.MovieCredit{{MovieContentID: movie.ContentID, ActressID: source.ID, CreditedName: "Source Identity", Actress: &source}}
	return movie, collision, source, target
}

func pr260RealApply(fs afero.Fs, movie *models.Movie, media organizer.MediaFormatConfig, posterClient *http.Client, generateNFO bool) *applyOrchImpl {
	engine := template.NewEngine()
	org := organizer.NewOrganizer(fs, &organizer.Config{MediaFormatConfig: media, FolderFormat: "<ACTRESS>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, engine, nil)
	nameCfg := nfo.NFONameConfig{FilenameTemplate: "<ACTRESS>.nfo", FirstNameOrder: true}
	gen := nfo.NewGenerator(fs, &nfo.Config{FilenameTemplate: nameCfg.FilenameTemplate, FirstNameOrder: true, TemplateEngine: engine})
	var dl downloader.DownloaderInterface
	if posterClient != nil {
		dl = downloader.NewDownloader(posterClient, fs, &downloader.Config{DownloadPoster: true, MediaFormatConfig: media}, engine)
	}
	return newApplyOrchestrator(fs, org, dl, gen, nil, ApplyConfig{NFONameCfg: nameCfg}, engine, noOpRevertLog{}, nil, nil)
}

func TestPR260RealArtifactInterleavingDoesNotPublishStaleOutputs(t *testing.T) {
	db, dsn := pr260ArtifactDB(t)
	mutationDB, err := database.New(&database.Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = mutationDB.Close() })
	movie, collision, source, target := pr260ArtifactSeed(t, db)
	base := afero.NewMemMapFs()
	var publicationOrder, mutationOrder int32
	fs := &pr260ArtifactBarrierFS{Fs: base, entered: make(chan struct{}), resume: make(chan struct{}), order: &publicationOrder}
	root := "/library"
	sourcePath := "/incoming/PR260-ARTIFACT.mp4"
	require.NoError(t, fs.MkdirAll(filepath.Dir(sourcePath), 0o755))
	require.NoError(t, afero.WriteFile(fs, sourcePath, []byte("video"), 0o644))
	match := models.FileMatchInfo{Path: sourcePath, Name: filepath.Base(sourcePath), Extension: ".mp4", MovieID: movie.ID}
	media := organizer.MediaFormatConfig{PosterFormat: "<ACTRESS>-poster.jpg"}
	posterBytes := pr260PosterBytes(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(posterBytes)
	}))
	defer server.Close()
	movie.Poster.PosterURL = server.URL + "/poster.jpg"
	engine := template.NewEngine()
	org := organizer.NewOrganizer(fs, &organizer.Config{MediaFormatConfig: media, FolderFormat: "<ACTRESS>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, engine, nil)
	planOld, err := org.PlanOrganize(context.Background(), organizer.OrganizeCmd{Match: match, Movie: &movie, DestDir: root, MoveFiles: true})
	require.NoError(t, err)
	oldDir := planOld.TargetDir
	oldNFO := filepath.Join(oldDir, nfo.ResolveNFOFilename(engine, &movie, nfo.NFONameConfig{FilenameTemplate: "<ACTRESS>.nfo", FirstNameOrder: true}))
	oldPoster := filepath.Join(oldDir, "Source Identity-poster.jpg")
	movie.Poster.PosterURL = server.URL + "/poster.jpg"
	orch := pr260RealApply(fs, &movie, media, server.Client(), true)
	orch.artifactPrepared = func() {
		fs.once.Do(func() { close(fs.entered) })
		<-fs.resume
	}
	cmd := ApplyCmd{Movie: &movie, PublicationFence: db.Repositories().MovieRepo.(database.ApplyPublicationFencer), Match: match, DestPath: root, Organize: OrganizeOptions{MoveFiles: true}, Download: true, GenerateNFO: true, OperationMode: operationmode.OperationModeOrganize}
	done := make(chan struct{})
	var applyErr error
	go func() { _, applyErr = orch.Execute(context.Background(), cmd); close(done) }()
	select {
	case <-fs.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("real organizer did not reach filesystem publication barrier")
	}
	resolved := make(chan error, 1)
	go func() {
		_, resolveErr := database.NewCollisionService(mutationDB).Resolve(context.Background(), collision.ID, models.CollisionResolutionReassign, target.ID)
		if resolveErr == nil {
			atomic.AddInt32(&mutationOrder, 1)
		}
		resolved <- resolveErr
	}()
	released := false
	release := func() {
		if !released {
			close(fs.resume)
			released = true
		}
	}
	select {
	case resolveErr := <-resolved:
		require.NoError(t, resolveErr)
	case <-time.After(3 * time.Second):
		release()
		require.NoError(t, <-resolved)
	}
	release()
	<-done
	persisted, err := db.Repositories().MovieRepo.FindByID(context.Background(), movie.ID)
	require.NoError(t, err)
	require.True(t, persisted.RenderDirty)
	publication := atomic.LoadInt32(&publicationOrder)
	mutation := atomic.LoadInt32(&mutationOrder)
	t.Logf("orders publication=%d mutation=%d oldDir=%s oldNFO=%s oldPoster=%s", publication, mutation, oldDir, oldNFO, oldPoster)
	require.Positive(t, mutation)
	if errors.Is(applyErr, database.ErrApplyPublicationStale) {
		exists, existsErr := afero.Exists(fs, oldNFO)
		require.NoError(t, existsErr)
		assert.False(t, exists)
		exists, existsErr = afero.Exists(fs, oldPoster)
		require.NoError(t, existsErr)
		assert.False(t, exists)
		_, statErr := fs.Stat(oldDir)
		assert.Error(t, statErr)
	} else {
		require.NoError(t, applyErr)
		require.Positive(t, publication)
		require.Less(t, publication, mutation)
	}
	_ = source
}

func TestPR260RealArtifactFailureStaysDirtyUntilSuccessfulRetry(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	source := models.Actress{FirstName: "Retry", LastName: "Identity", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&source).Error)
	movie := models.Movie{ContentID: "pr260-retry", ID: "PR260-RETRY", Title: "Retry movie", Actresses: []models.Actress{source}, RenderDirty: true, RenderGeneration: 4}
	require.NoError(t, db.Create(&movie).Error)
	base := afero.NewMemMapFs()
	fs := &pr260ArtifactCreateFailureFS{Fs: base, fail: true}
	root := "/library/Retry Identity"
	require.NoError(t, fs.MkdirAll(root, 0o755))
	path := "/incoming/PR260-RETRY.mp4"
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, afero.WriteFile(fs, path, []byte("video"), 0o644))
	fs.fail = true
	movie.Poster.PosterURL = ""
	orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, true)
	cmd := ApplyCmd{Movie: &movie, PublicationFence: db.Repositories().MovieRepo.(database.ApplyPublicationFencer), Match: models.FileMatchInfo{Path: path, Name: filepath.Base(path), Extension: ".mp4", MovieID: movie.ID}, DestPath: root, Organize: OrganizeOptions{Skip: true}, GenerateNFO: true, OperationMode: operationmode.OperationModeMetadataArtwork}
	_, err := orch.Execute(context.Background(), cmd)
	require.Error(t, err)
	persisted, err := db.Repositories().MovieRepo.FindByID(context.Background(), movie.ID)
	require.NoError(t, err)
	require.True(t, persisted.RenderDirty)
	fs.fail = false
	_, err = orch.Execute(context.Background(), cmd)
	require.NoError(t, err)
	name := nfo.ResolveNFOFilename(template.NewEngine(), &movie, nfo.NFONameConfig{FilenameTemplate: "<ACTRESS>.nfo", FirstNameOrder: true})
	exists, existsErr := afero.Exists(fs, filepath.Join(root, name))
	require.NoError(t, existsErr)
	require.True(t, exists)
	persisted, err = db.Repositories().MovieRepo.FindByID(context.Background(), movie.ID)
	require.NoError(t, err)
	require.False(t, persisted.RenderDirty)
}

func pr260PosterBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: 40, B: 60, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))
	return buf.Bytes()
}

func TestPR260ArtifactStagingHelpers(t *testing.T) {
	t.Run("sibling classification", func(t *testing.T) {
		cases := []struct {
			name, source, sibling string
			want                  bool
		}{
			{name: "subtitle", source: "movie.mp4", sibling: "movie.srt", want: true},
			{name: "prefixed subtitle", source: "movie.mp4", sibling: "movie.en.srt", want: true},
			{name: "wrong subtitle stem", source: "movie.mp4", sibling: "moviex.srt"},
			{name: "multipart", source: "movie.mp4", sibling: "movie-cd2.mp4", want: true},
			{name: "multipart source", source: "movie-cd1.mp4", sibling: "movie-cd2.mp4"},
			{name: "unrelated extension", source: "movie.mp4", sibling: "movie.txt"},
		}
		for _, tt := range cases {
			t.Run(tt.name, func(t *testing.T) {
				assert.Equal(t, tt.want, isStagedArtifactSibling(tt.source, tt.sibling))
			})
		}
	})

	t.Run("extensions and multipart roots", func(t *testing.T) {
		for _, ext := range []string{".srt", ".ass", ".ssa", ".sub", ".idx", ".sup", ".vtt", ".smi", ".sami"} {
			assert.True(t, isStagedArtifactSubtitleExtension(ext), ext)
		}
		assert.False(t, isStagedArtifactSubtitleExtension(".txt"))
		for _, ext := range []string{".mp4", ".mkv", ".avi", ".wmv", ".flv", ".mov", ".m4v", ".webm", ".mpg", ".mpeg", ".m2ts", ".ts"} {
			assert.True(t, isStagedArtifactVideoExtension(ext), ext)
		}
		assert.False(t, isStagedArtifactVideoExtension(".txt"))
		for _, tt := range []struct{ stem, want string }{
			{"movie-cd1", "movie"}, {"movie.cd2", "movie"}, {"movie_disc3", "movie"}, {"movie-part4", "movie"}, {"movie_pt5", "movie"},
			{"movie-cd", ""}, {"movie-cdx", ""}, {"movie", ""},
		} {
			assert.Equal(t, tt.want, stagedArtifactMultipartRoot(tt.stem), tt.stem)
		}
	})

	t.Run("sibling names and paths", func(t *testing.T) {
		assert.Equal(t, "target.srt", stagedArtifactSiblingName("movie.mp4", "target.mp4", "movie.srt"))
		assert.Equal(t, "target.en.srt", stagedArtifactSiblingName("movie.mp4", "target.mp4", "movie.en.srt"))
		assert.Equal(t, "other.txt", stagedArtifactSiblingName("movie.mp4", "target.mp4", "other.txt"))
		stageRoot := filepath.Clean("/stage")
		finalRoot := filepath.Clean("/final")
		outsideRoot := filepath.Clean("/outside")
		incomingRoot := filepath.Clean("/incoming")
		assert.True(t, containsPath([]string{filepath.Join(stageRoot, "a"), filepath.Join(stageRoot, "b")}, filepath.Join(stageRoot, "..", "stage", "a")))
		assert.False(t, containsPath([]string{filepath.Join(stageRoot, "a")}, filepath.Join(stageRoot, "b")))

		fs := afero.NewMemMapFs()
		sourcePath := filepath.Join(filepath.Clean("/source"), "movie.mp4")
		stagedPath := filepath.Join(stageRoot, "movie.mp4")
		require.NoError(t, afero.WriteFile(fs, sourcePath, []byte("video"), 0o644))
		require.NoError(t, copyArtifactFile(fs, sourcePath, stagedPath, 0o640))
		assert.Equal(t, []byte("video"), func() []byte { b, _ := afero.ReadFile(fs, stagedPath); return b }())
		require.Error(t, copyArtifactFile(fs, filepath.Join(filepath.Clean("/source"), "missing.mp4"), filepath.Join(stageRoot, "missing.mp4"), 0o644))

		stage := &artifactStage{fs: fs, root: stageRoot, finalRoot: finalRoot}
		path, err := stage.finalPath(stageRoot)
		require.NoError(t, err)
		assert.Equal(t, finalRoot, path)
		path, err = stage.finalPath(filepath.Join(stageRoot, "sub", "movie.mp4"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(finalRoot, "sub", "movie.mp4"), path)
		_, err = stage.finalPath(filepath.Join(outsideRoot, "movie.mp4"))
		require.Error(t, err)
		inPlace := &artifactStage{fs: fs, root: stageRoot, finalRoot: filepath.Join(incomingRoot, "final"), inPlace: true}
		path, err = inPlace.finalPath(filepath.Join(incomingRoot, "other", "movie.srt"))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(incomingRoot, "other", "movie.srt"), path)
		stage.cleanup()
		_, err = fs.Stat(stageRoot)
		assert.Error(t, err)
	})
}

func TestPR260RealStaleArtifactOutcomeFailsWorkerFacingExecuteAndRetries(t *testing.T) {
	db, _ := pr260ArtifactDB(t)
	actress := models.Actress{FirstName: "Fence", LastName: "Retry", Verified: true, Origin: "user"}
	require.NoError(t, db.Create(&actress).Error)
	movie := models.Movie{ContentID: "pr260-stale-outcome", ID: "PR260-STALE-OUTCOME", Title: "Stale outcome", Actresses: []models.Actress{actress}, RenderDirty: true, RenderGeneration: 7}
	require.NoError(t, db.Create(&movie).Error)
	fs := afero.NewMemMapFs()
	source := "/incoming/PR260-STALE-OUTCOME.mp4"
	dest := "/library/Fence Retry"
	require.NoError(t, fs.MkdirAll(filepath.Dir(source), 0o755))
	require.NoError(t, fs.MkdirAll(dest, 0o755))
	require.NoError(t, afero.WriteFile(fs, source, []byte("source"), 0o644))
	orch := pr260RealApply(fs, &movie, organizer.MediaFormatConfig{}, nil, true)
	orch.artifactPrepared = func() {
		require.NoError(t, db.Model(&models.Movie{}).
			Where("content_id = ?", movie.ContentID).
			Updates(map[string]any{"render_generation": movie.RenderGeneration + 1, "render_dirty": true}).Error)
	}
	cmd := ApplyCmd{
		Movie:            &movie,
		PersistedMovie:   true,
		PublicationFence: db.Repositories().MovieRepo.(database.ApplyPublicationFencer),
		Match:            models.FileMatchInfo{Path: source, Name: filepath.Base(source), Extension: ".mp4", MovieID: movie.ID},
		DestPath:         dest,
		Organize:         OrganizeOptions{Skip: true},
		GenerateNFO:      true,
		OperationMode:    operationmode.OperationModeMetadataArtwork,
	}
	result, err := orch.Execute(context.Background(), cmd)
	require.ErrorIs(t, err, database.ErrApplyPublicationStale)
	require.NotNil(t, result)
	require.Equal(t, "artifact_publication", result.FailedStep)
	require.True(t, result.PrePublication)
	require.Empty(t, result.NFOPath)
	require.Empty(t, result.DownloadPaths)
	exists, statErr := afero.Exists(fs, source)
	require.NoError(t, statErr)
	require.True(t, exists)
	entries, readErr := afero.ReadDir(fs, filepath.Dir(dest))
	require.NoError(t, readErr)
	for _, entry := range entries {
		require.NotContains(t, entry.Name(), ".javinizer-apply-")
	}
	persisted, findErr := db.Repositories().MovieRepo.FindByContentID(context.Background(), movie.ContentID)
	require.NoError(t, findErr)
	require.True(t, persisted.RenderDirty)

	orch.artifactPrepared = nil
	cmd.Movie = persisted
	result, err = orch.Execute(context.Background(), cmd)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.NFOPath)
	persisted, findErr = db.Repositories().MovieRepo.FindByContentID(context.Background(), movie.ContentID)
	require.NoError(t, findErr)
	require.False(t, persisted.RenderDirty)
}
