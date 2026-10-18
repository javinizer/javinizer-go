package workflow

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/history"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// Opening the file and statting its handle eagerly captures Windows file identity.
// A pathname-only os.Stat can lazily reopen a successor during os.SameFile.
func preservedArtifactNativeInfo(path string) (os.FileInfo, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil {
		return nil, statErr
	}
	return info, closeErr
}

type preservedArtifactTransport struct {
	body   []byte
	status int
	before func()
}

func (f preservedArtifactTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if f.before != nil {
		f.before()
	}
	return &http.Response{StatusCode: f.status, Header: http.Header{"Content-Type": []string{"image/jpeg"}}, Body: io.NopCloser(bytes.NewReader(f.body)), Request: r}, nil
}

type preservedArtifactFence struct{ movie *models.Movie }

func (f preservedArtifactFence) WithApplyPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	return fn(f.movie)
}
func (f preservedArtifactFence) WithApplyArtifactPublicationFence(_ context.Context, _ string, _ int64, fn func(*models.Movie) error) error {
	return fn(f.movie)
}

// Exercise the real downloader, fenced publication, durable journal and Undo.
// Every download family uses the same publication ownership classification.
func TestPreservedArtifactOwnershipApplyUndo(t *testing.T) {
	for _, metadataOnly := range []bool{false, true} {
		for _, scenario := range []string{"preserve", "identical", "replace", "fresh", "late-substitute", "substitute-before-publication", "download-failed", "cancelled"} {
			t.Run(fmt.Sprintf("metadata=%v/%s", metadataOnly, scenario), func(t *testing.T) {
				fs := afero.NewOsFs()
				root := t.TempDir()
				library := filepath.Join(root, "library")
				dir := filepath.Join(library, "ABC-123")
				require.NoError(t, fs.MkdirAll(dir, 0755))
				video := filepath.Join(dir, "ABC-123.mp4")
				require.NoError(t, afero.WriteFile(fs, video, []byte("resident video"), 0644))
				videoID, err := preservedArtifactNativeInfo(video)
				require.NoError(t, err)
				payload := pr260PosterBytes(t)
				movie := models.Movie{ID: "ABC-123", ContentID: "preserved-artifacts", Poster: models.PosterState{PosterURL: "https://fixture.invalid/poster.jpg", CoverURL: "https://fixture.invalid/fanart.jpg"}, TrailerURL: "https://fixture.invalid/trailer.mp4", Screenshots: []string{"https://fixture.invalid/shot.jpg"}, Actresses: []models.Actress{{FirstName: "Actor", ThumbURL: "https://fixture.invalid/actor.jpg"}}}
				media := organizer.MediaFormatConfig{PosterFormat: "poster.jpg", FanartFormat: "fanart.jpg", TrailerFormat: "trailer.mp4", ScreenshotFolder: "extras", ScreenshotFormat: "shot.jpg", ActressFolder: "actors", ActressFormat: "thumb.jpg"}
				names := []string{"poster.jpg", "fanart.jpg", "trailer.mp4", filepath.Join("extras", "shot.jpg"), filepath.Join("actors", "thumb.jpg")}
				ids := map[string]os.FileInfo{}
				bodies := map[string][]byte{}
				for _, name := range names {
					p := filepath.Join(dir, name)
					if scenario != "fresh" {
						require.NoError(t, fs.MkdirAll(filepath.Dir(p), 0755))
						body := []byte("existing " + name)
						if scenario == "identical" {
							body = payload
						}
						require.NoError(t, afero.WriteFile(fs, p, body, 0644))
						ids[p], err = preservedArtifactNativeInfo(p)
						require.NoError(t, err)
						bodies[p] = body
					}
				}
				db, _ := pr260ArtifactDB(t)
				repo := database.NewBatchFileOperationRepository(db)
				engine := template.NewEngine()
				org := organizer.NewOrganizer(fs, &organizer.Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true, OperationMode: operationmode.OperationModeOrganize}, engine, nil)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				transport := preservedArtifactTransport{body: payload, status: http.StatusOK}
				if scenario == "substitute-before-publication" {
					var once sync.Once
					transport.before = func() {
						once.Do(func() {
							for _, name := range names {
								p := filepath.Join(dir, name)
								replacement := p + ".foreign"
								body := []byte("foreign before publication " + name)
								require.NoError(t, os.WriteFile(replacement, body, 0644))
								require.NoError(t, os.Rename(replacement, p))
								id, statErr := preservedArtifactNativeInfo(p)
								require.NoError(t, statErr)
								require.False(t, os.SameFile(ids[p], id))
								ids[p], bodies[p] = id, body
							}
						})
					}
				}
				if scenario == "download-failed" {
					transport.status = http.StatusNotFound
				}
				if scenario == "cancelled" {
					transport.before = cancel
				}
				dl := downloader.NewDownloader(&http.Client{Transport: transport}, fs, &downloader.Config{DownloadPoster: true, DownloadCover: true, DownloadExtrafanart: true, DownloadTrailer: true, DownloadActress: true, MediaFormatConfig: media}, engine)
				orch := newApplyOrchestrator(fs, org, dl, nil, nil, ApplyConfig{}, engine, NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "preserved-artifacts", fs, nil, nil, nil), nil, nil)
				cmd := ApplyCmd{Movie: &movie, Match: models.FileMatchInfo{Path: video, Name: filepath.Base(video), Extension: ".mp4", MovieID: movie.ID}, DestPath: library, Organize: OrganizeOptions{Skip: metadataOnly}, OperationMode: operationmode.OperationModeOrganize, PublicationFence: preservedArtifactFence{movie: &movie}, Download: true, OverwriteExistingMedia: scenario == "replace"}
				if metadataOnly {
					cmd.DestPath = dir
					cmd.OperationMode = operationmode.OperationModeMetadataArtwork
				}
				result, applyErr := orch.Execute(ctx, cmd)
				if scenario == "cancelled" {
					require.Error(t, applyErr)
				} else {
					require.NoError(t, applyErr)
				}
				if result != nil && result.OperationID != "" {
					ledger := p3Ledger(t, repo, result.OperationID)
					for _, name := range names {
						p := filepath.Join(dir, name)
						if scenario == "fresh" {
							require.Contains(t, result.DownloadPaths, p)
							require.Contains(t, ledger.Delete, p)
						} else {
							require.NotContains(t, result.DownloadPaths, p)
							require.NotContains(t, ledger.Delete, p)
							for _, entry := range ledger.PlannedDeletes {
								require.NotEqual(t, p, entry.Path)
							}
						}
					}
				}
				for _, name := range names {
					p := filepath.Join(dir, name)
					got, e := os.ReadFile(p)
					require.NoError(t, e)
					if scenario == "fresh" || scenario == "replace" {
						require.Equal(t, payload, got)
					} else {
						require.Equal(t, bodies[p], got)
						id, e := preservedArtifactNativeInfo(p)
						require.NoError(t, e)
						require.True(t, os.SameFile(ids[p], id), "preserved native object: %s", p)
					}
				}
				if scenario == "late-substitute" {
					for _, name := range names {
						p := filepath.Join(dir, name)
						replacement := p + ".foreign"
						require.NoError(t, os.WriteFile(replacement, payload, 0644))
						require.NoError(t, os.Rename(replacement, p))
						ids[p], err = preservedArtifactNativeInfo(p)
						require.NoError(t, err)
						bodies[p] = payload
					}
				}
				undo, err := history.NewReverter(fs, repo).RevertBatch(context.Background(), "preserved-artifacts")
				require.NoError(t, err)
				require.Zero(t, undo.Failed)
				for _, name := range names {
					p := filepath.Join(dir, name)
					got, e := os.ReadFile(p)
					if scenario == "fresh" {
						require.True(t, os.IsNotExist(e))
						continue
					}
					require.NoError(t, e)
					require.Equal(t, bodies[p], got)
					if scenario != "replace" {
						id, e := preservedArtifactNativeInfo(p)
						require.NoError(t, e)
						require.True(t, os.SameFile(ids[p], id), "Undo retained foreign native object: %s", p)
					}
				}
				afterVideo, err := preservedArtifactNativeInfo(video)
				require.NoError(t, err)
				require.True(t, os.SameFile(videoID, afterVideo))
			})
		}
	}
}

func TestArtifactSkippedSamePathHasNoPublicationOwnership(t *testing.T) {
	fs := afero.NewOsFs()
	root := t.TempDir()
	source := filepath.Join(root, "poster.jpg")
	require.NoError(t, os.WriteFile(source, []byte("resident"), 0644))
	before, err := preservedArtifactNativeInfo(source)
	require.NoError(t, err)
	stage := &artifactStage{fs: fs, root: filepath.Join(root, "stage"), finalRoot: filepath.Join(root, "movie.mp4"), inPlace: true}
	preserved, err := stage.installPaths([]string{source}, []string{source}, "", "", func([]models.DeleteEntry) error { t.Fatal("skipped path must not journal deletion"); return nil })
	require.NoError(t, err)
	require.True(t, preserved)
	require.True(t, stage.skippedUnowned(source))
	after, err := preservedArtifactNativeInfo(source)
	require.NoError(t, err)
	require.True(t, os.SameFile(before, after))
}
