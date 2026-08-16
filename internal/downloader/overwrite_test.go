package downloader

import (
	"context"
	"errors"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type statErrorFS struct {
	afero.Fs
	path string
	err  error
}

func nativePath(path string) string {
	return filepath.FromSlash(path)
}

func (f statErrorFS) Stat(name string) (os.FileInfo, error) {
	if filepath.Clean(name) == filepath.Clean(nativePath(f.path)) {
		return nil, f.err
	}
	return f.Fs.Stat(name)
}

type rejectExistingRenameFS struct {
	afero.Fs
}

func (f rejectExistingRenameFS) Rename(oldname, newname string) error {
	if _, err := f.Fs.Stat(newname); err == nil {
		return errors.New("replace existing destination rejected")
	}
	return f.Fs.Rename(oldname, newname)
}

func TestDownload_OverwriteExistingReplacesAndClassifies(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("new bytes"))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/output/TEST-001-fanart.jpg", []byte("old bytes"), 0644))
	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true}, nil)
	outcome, err := d.Download(context.Background(), DownloadCmd{
		OperationID:            "test-op-" + t.Name(),
		Recorder:               &armedTestLedger{},
		Movie:                  &models.Movie{ID: "TEST-001", Poster: models.PosterState{CoverURL: server.URL + "/cover.jpg"}},
		DestDir:                "/output",
		OverwriteExistingMedia: true,
	})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.Equal(t, int32(1), requests.Load())
	got, readErr := afero.ReadFile(fs, "/output/TEST-001-fanart.jpg")
	require.NoError(t, readErr)
	assert.Equal(t, []byte("new bytes"), got)
	var coverResult *DownloadResult
	for i := range outcome.Results {
		if outcome.Results[i].Type == MediaTypeCover {
			coverResult = &outcome.Results[i]
		}
	}
	require.NotNil(t, coverResult)
	assert.True(t, coverResult.Downloaded)
	assert.True(t, coverResult.Replaced)
	assert.Equal(t, []string{nativePath("/output/TEST-001-fanart.jpg")}, outcome.DownloadedPaths)
	assert.Empty(t, outcome.CreatedPaths)
}

func TestDownload_EmptyBodyDoesNotReplaceExisting(t *testing.T) {
	// P0 regression: a successful HTTP response with an EMPTY body made
	// io.Copy return (0, nil), after which replaceFile overwrote the existing
	// artwork with a zero-byte file and reported success. With
	// overwrite_existing_media enabled, a transient CDN/proxy hiccup could
	// therefore destroy valid media.
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		// Explicitly 200 OK with no body.
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/output/TEST-001-fanart.jpg", []byte("old bytes"), 0644))
	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true}, nil)
	outcome, err := d.Download(context.Background(), DownloadCmd{
		Movie:                  &models.Movie{ID: "TEST-001", Poster: models.PosterState{CoverURL: server.URL + "/cover.jpg"}},
		DestDir:                "/output",
		OverwriteExistingMedia: true,
	})
	require.Error(t, err, "an empty 200 body must fail the download, not silently succeed")

	// The existing media must be preserved byte-for-byte.
	got, readErr := afero.ReadFile(fs, "/output/TEST-001-fanart.jpg")
	require.NoError(t, readErr)
	assert.Equal(t, []byte("old bytes"), got)

	var coverResult *DownloadResult
	for i := range outcome.Results {
		if outcome.Results[i].Type == MediaTypeCover {
			coverResult = &outcome.Results[i]
		}
	}
	require.NotNil(t, coverResult)
	assert.False(t, coverResult.Downloaded)
	assert.False(t, coverResult.Replaced, "nothing may be reported replaced when the body was empty")
	require.Error(t, coverResult.Error)
	assert.Contains(t, coverResult.Error.Error(), "0 bytes")
}

func TestDownload_HTMLChallengePageDoesNotReplaceExisting(t *testing.T) {
	// P0 regression: an auth-challenge/proxy-error HTML page returns 200 OK
	// — before the validation guard it would atomically replace valid
	// artwork with markup and report success.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("<!DOCTYPE html><html><body>Verify you are human</body></html>"))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/output/TEST-001-fanart.jpg", []byte("old bytes"), 0644))
	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true}, nil)
	_, err := d.Download(context.Background(), DownloadCmd{
		Movie:                  &models.Movie{ID: "TEST-001", Poster: models.PosterState{CoverURL: server.URL + "/cover.jpg"}},
		DestDir:                "/output",
		OverwriteExistingMedia: true,
	})
	require.Error(t, err)

	got, readErr := afero.ReadFile(fs, "/output/TEST-001-fanart.jpg")
	require.NoError(t, readErr)
	assert.Equal(t, []byte("old bytes"), got, "existing media must be preserved byte-for-byte")
}

func TestDownload_JSONErrorBodyDoesNotReplaceExisting(t *testing.T) {
	// Same class as the HTML challenge: a JSON error payload (CDN/proxy
	// replies with 200 + {"error": ...}) must not overwrite valid media.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(`{"error": "rate limited"}`))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/output/TEST-001-fanart.jpg", []byte("old bytes"), 0644))
	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true}, nil)
	_, err := d.Download(context.Background(), DownloadCmd{
		Movie:                  &models.Movie{ID: "TEST-001", Poster: models.PosterState{CoverURL: server.URL + "/cover.jpg"}},
		DestDir:                "/output",
		OverwriteExistingMedia: true,
	})
	require.Error(t, err)

	got, readErr := afero.ReadFile(fs, "/output/TEST-001-fanart.jpg")
	require.NoError(t, readErr)
	assert.Equal(t, []byte("old bytes"), got, "existing media must be preserved byte-for-byte")
}

func TestDownload_XMLErrorDocumentDoesNotReplaceExisting(t *testing.T) {
	// P0 regression: an S3-style XML error document returns 200 OK with no
	// DOCTYPE/html marker and no `<?xml` declaration — it slipped past the
	// HTML/JSON checks and would overwrite valid artwork. Both the declared
	// Content-Type and an undeclared <Error> body must be rejected.
	for _, tc := range []struct {
		name        string
		contentType string
		body        string
	}{
		{"declared application/xml", "application/xml", "<Error><Code>AccessDenied</Code></Error>"},
		{"undeclared XML error body", "", "<Error><Code>NoSuchKey</Code></Error>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.contentType != "" {
					w.Header().Set("Content-Type", tc.contentType)
				}
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			fs := afero.NewMemMapFs()
			require.NoError(t, afero.WriteFile(fs, "/output/TEST-001-fanart.jpg", []byte("old bytes"), 0644))
			d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true}, nil)
			_, err := d.Download(context.Background(), DownloadCmd{
				Movie:                  &models.Movie{ID: "TEST-001", Poster: models.PosterState{CoverURL: server.URL + "/cover.jpg"}},
				DestDir:                "/output",
				OverwriteExistingMedia: true,
			})
			require.Error(t, err)

			got, readErr := afero.ReadFile(fs, "/output/TEST-001-fanart.jpg")
			require.NoError(t, readErr)
			assert.Equal(t, []byte("old bytes"), got, "existing media must be preserved byte-for-byte")
		})
	}
}

func TestDownload_DeclaredTextPlainBodyDoesNotReplaceExisting(t *testing.T) {
	// P0 regression: a proxy/upstream answering 200 + "text/plain" with a
	// prose error body ("rate limit exceeded") slipped past the HTML/JSON/XML
	// checks and would replace valid artwork with text.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("rate limit exceeded"))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/output/TEST-001-fanart.jpg", []byte("old bytes"), 0644))
	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true}, nil)
	_, err := d.Download(context.Background(), DownloadCmd{
		Movie:                  &models.Movie{ID: "TEST-001", Poster: models.PosterState{CoverURL: server.URL + "/cover.jpg"}},
		DestDir:                "/output",
		OverwriteExistingMedia: true,
	})
	require.Error(t, err)

	got, readErr := afero.ReadFile(fs, "/output/TEST-001-fanart.jpg")
	require.NoError(t, readErr)
	assert.Equal(t, []byte("old bytes"), got, "existing media must be preserved byte-for-byte")
}

// truncatingTransport fabricates the one truncation shape net/http insists
// wouldn't happen: a response whose DECLARED Content-Length exceeds what the
// body actually yields, with the body ending cleanly (0, io.EOF) instead of
// an unexpected-EOF read error. The stock transport enforces declared length
// itself, so this branch is only reachable through non-enforcing/custom
// transports — e.g. proxy chains that rewrite bodies — and losing media to
// it is exactly what the guard exists to prevent.
type truncatingTransport struct{}

type shortCleanBody struct {
	payload []byte
	done    bool
}

func (b *shortCleanBody) Read(p []byte) (int, error) {
	if b.done {
		return 0, io.EOF
	}
	b.done = true
	return copy(p, b.payload), nil
}

func (b *shortCleanBody) Close() error { return nil }

func (t truncatingTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Header:        make(http.Header),
		ContentLength: 1000, // declared 1000 bytes…
		Body:          &shortCleanBody{payload: []byte("short")},
	}, nil
}

func TestDownload_DeclaredLengthMismatchDoesNotReplaceExisting(t *testing.T) {
	// …but the body yields 5. Under a custom (non-enforcing) transport this
	// must be refused BEFORE replaceFile — otherwise valid artwork is swapped
	// for a truncated payload with a success report.
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/output/TEST-001-fanart.jpg", []byte("old bytes"), 0644))
	d := NewDownloader(&http.Client{Transport: truncatingTransport{}}, fs, &Config{DownloadCover: true}, nil)
	_, err := d.Download(context.Background(), DownloadCmd{
		Movie:                  &models.Movie{ID: "TEST-001", Poster: models.PosterState{CoverURL: "http://unit.test/cover.jpg"}},
		DestDir:                "/output",
		OverwriteExistingMedia: true,
	})
	require.Error(t, err)

	got, readErr := afero.ReadFile(fs, "/output/TEST-001-fanart.jpg")
	require.NoError(t, readErr)
	assert.Equal(t, []byte("old bytes"), got, "existing media must be preserved byte-for-byte")
}

func TestDownload_OverwriteFalseKeepsExisting(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("new bytes"))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/output/TEST-002-fanart.jpg", []byte("old bytes"), 0644))
	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true}, nil)
	outcome, err := d.Download(context.Background(), DownloadCmd{
		Movie:   &models.Movie{ID: "TEST-002", Poster: models.PosterState{CoverURL: server.URL + "/cover.jpg"}},
		DestDir: "/output",
	})
	require.NoError(t, err)
	assert.Equal(t, int32(0), requests.Load())
	assert.False(t, outcome.Results[0].Downloaded)
	assert.False(t, outcome.Results[0].Replaced)
}

func TestDownload_OverwriteReplacesEachEnabledMediaType(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(r.URL.Path))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	cfg := &Config{
		DownloadCover:       true,
		DownloadExtrafanart: true,
		DownloadTrailer:     true,
		DownloadActress:     true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			FanartFormat:     "<ID>-fanart.jpg",
			TrailerFormat:    "<ID>-trailer.mp4",
			ScreenshotFolder: "shots",
			ScreenshotFormat: "shot<INDEX>.jpg",
			ActressFolder:    "actors",
			ActressFormat:    "<ACTORNAME>.jpg",
		},
	}
	d := NewDownloader(server.Client(), fs, cfg, nil)
	movie := &models.Movie{
		ID:          "TEST-012",
		Poster:      models.PosterState{CoverURL: server.URL + "/cover.jpg"},
		TrailerURL:  server.URL + "/trailer.mp4",
		Screenshots: []string{server.URL + "/screenshot.jpg"},
		Actresses:   []models.Actress{{FirstName: "Test", LastName: "Actress", ThumbURL: server.URL + "/actress.jpg"}},
	}
	tmplCtx := d.buildTemplateContext(movie, nil)
	paths := []string{
		d.pathResolver.ResolveFanartPath(movie, nil, true, tmplCtx, "/output"),
		d.pathResolver.ResolveTrailerPath(movie, true, tmplCtx, "/output"),
		filepath.Join("/output", cfg.ScreenshotFolder, d.pathResolver.ResolveScreenshotNames(movie, true, tmplCtx)[0]),
	}
	formattedName := models.FormatActressName(movie.Actresses[0], models.FormatActressNameOptions{
		JapaneseNames:  d.config.ActorJapaneseNames,
		FirstNameOrder: d.config.ActorFirstNameOrder,
	})
	paths = append(paths, filepath.Join("/output", cfg.ActressFolder, d.generateActressFilename(&models.Movie{ID: movie.ID}, formattedName, cfg.ActressFormat)))
	for _, path := range paths {
		require.NoError(t, afero.WriteFile(fs, path, []byte("old"), 0644))
	}

	outcome, err := d.Download(context.Background(), DownloadCmd{
		OperationID: "test-op-" + t.Name(),
		Recorder:    &armedTestLedger{}, Movie: movie, DestDir: "/output", OverwriteExistingMedia: true, Dedup: &sync.Map{}})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	for _, mediaType := range []MediaType{MediaTypeCover, MediaTypeExtrafanart, MediaTypeTrailer, MediaTypeActress} {
		var result *DownloadResult
		for i := range outcome.Results {
			if outcome.Results[i].Type == mediaType {
				result = &outcome.Results[i]
				break
			}
		}
		require.NotNil(t, result, string(mediaType))
		assert.True(t, result.Downloaded, string(mediaType))
		assert.True(t, result.Replaced, string(mediaType))
	}
	assert.Len(t, outcome.DownloadedPaths, 4)
	assert.Empty(t, outcome.CreatedPaths)
	assert.Equal(t, int32(4), requests.Load())

	disabled := NewDownloader(server.Client(), fs, &Config{MediaFormatConfig: cfg.MediaFormatConfig}, nil)
	before := requests.Load()
	disabledOutcome, disabledErr := disabled.Download(context.Background(), DownloadCmd{
		OperationID: "test-op-" + t.Name(),
		Recorder:    &armedTestLedger{}, Movie: movie, DestDir: "/output", OverwriteExistingMedia: true, Dedup: &sync.Map{}})
	require.NoError(t, disabledErr)
	assert.Equal(t, before, requests.Load())
	assert.Empty(t, disabledOutcome.DownloadedPaths)
	assert.Empty(t, disabledOutcome.CreatedPaths)
}
func TestDownload_OverwriteCroppedCreatesAndRecordsCreatedPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 600, 400))
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	d := NewDownloader(server.Client(), fs, &Config{
		DownloadPoster: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "<ID>-poster.jpg",
		},
	}, nil)
	path := nativePath("/output/TEST-008-poster.jpg")
	outcome, err := d.Download(context.Background(), DownloadCmd{
		Movie: &models.Movie{
			ID:     "TEST-008",
			Poster: models.PosterState{CoverURL: server.URL + "/cover.jpg", ShouldCropPoster: true},
		},
		DestDir:                "/output",
		OverwriteExistingMedia: true,
	})
	require.NoError(t, err)
	require.NotNil(t, outcome)
	var posterResult *DownloadResult
	for i := range outcome.Results {
		if outcome.Results[i].Type == MediaTypePoster {
			posterResult = &outcome.Results[i]
		}
	}
	require.NotNil(t, posterResult)
	assert.True(t, posterResult.Downloaded)
	assert.False(t, posterResult.Replaced)
	assert.Contains(t, outcome.DownloadedPaths, path)
	assert.Contains(t, outcome.CreatedPaths, path)
	assertNoUniqueTemps(t, fs, "/output")
}

func TestDownload_OverwriteHTTPFailurePreservesExisting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	path := "/output/TEST-009-fanart.jpg"
	old := []byte("old bytes")
	require.NoError(t, afero.WriteFile(fs, path, old, 0644))
	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true}, nil)
	result, err := d.download(context.Background(), server.URL+"/cover.jpg", path, MediaTypeCover, true, nil)
	require.Error(t, err)
	assert.False(t, result.Downloaded)
	got, readErr := afero.ReadFile(fs, path)
	require.NoError(t, readErr)
	assert.Equal(t, old, got)
	assertNoUniqueTemps(t, fs, "/output")
}

func TestDownload_OverwritePartialOutcomeSeparatesReplacedAndCreated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trailer.mp4", "/screenshot.jpg":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte(r.URL.Path))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	trailerPath := nativePath("/output/TEST-010-trailer.mp4")
	require.NoError(t, afero.WriteFile(fs, trailerPath, []byte("old trailer"), 0644))
	d := NewDownloader(server.Client(), fs, &Config{
		DownloadCover:       true,
		DownloadPoster:      true,
		DownloadTrailer:     true,
		DownloadExtrafanart: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			TrailerFormat:    "<ID>-trailer.mp4",
			ScreenshotFormat: "screenshot.jpg",
			ScreenshotFolder: "extrafanart",
		},
	}, nil)
	outcome, err := d.Download(context.Background(), DownloadCmd{
		OperationID: "test-op-" + t.Name(),
		Recorder:    &armedTestLedger{},
		Movie: &models.Movie{
			ID:          "TEST-010",
			Poster:      models.PosterState{CoverURL: server.URL + "/cover.jpg", PosterURL: server.URL + "/poster.jpg"},
			TrailerURL:  server.URL + "/trailer.mp4",
			Screenshots: []string{server.URL + "/screenshot.jpg"},
		},
		DestDir:                "/output",
		OverwriteExistingMedia: true,
	})
	var partial *DownloadPartialError
	require.ErrorAs(t, err, &partial)
	require.NotNil(t, outcome)
	assert.Contains(t, outcome.DownloadedPaths, trailerPath)
	assert.NotContains(t, outcome.CreatedPaths, trailerPath)
	assert.Contains(t, outcome.CreatedPaths, nativePath("/output/extrafanart/screenshot.jpg"))
	assert.Contains(t, outcome.DownloadedPaths, nativePath("/output/extrafanart/screenshot.jpg"))
	got, readErr := afero.ReadFile(fs, trailerPath)
	require.NoError(t, readErr)
	assert.Equal(t, []byte("/trailer.mp4"), got)
}

func TestDownload_DedupSkippedCriticalMediaIsNotPartialFailure(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("unexpected"))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true, DownloadPoster: true}, nil)
	movie := &models.Movie{
		ID:     "TEST-011",
		Poster: models.PosterState{CoverURL: server.URL + "/cover.jpg", PosterURL: server.URL + "/poster.jpg"},
	}
	ctx := context.Background()
	tmplCtx := d.buildTemplateContext(movie, nil)
	coverPath := d.pathResolver.ResolveFanartPath(movie, nil, true, tmplCtx, "/output")
	posterPath := d.pathResolver.ResolvePosterPath(movie, nil, true, tmplCtx, "/output")
	dedup := &sync.Map{}
	dedup.Store(coverPath, struct{}{})
	dedup.Store(posterPath, struct{}{})

	_, err := d.downloadAllWithExtrafanart(ctx, movie, "/output", nil, false, true, dedup)
	assert.NoError(t, err)
	assert.Equal(t, int32(0), requests.Load())
}
func TestDownload_OverwritePartTwoDownloadsActressWhenItIsOnlyPart(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("new actress"))
	}))
	defer server.Close()

	d := NewDownloader(server.Client(), afero.NewMemMapFs(), &Config{DownloadActress: true}, nil)
	results, err := d.downloadAllWithExtrafanart(context.Background(), &models.Movie{
		ID:        "TEST-013",
		Actresses: []models.Actress{{FirstName: "Test", LastName: "Actress", ThumbURL: server.URL + "/actress.jpg"}},
	}, "/output", &MultipartInfo{IsMultiPart: true, PartNumber: 2}, false, true, &sync.Map{})

	require.NoError(t, err)
	actressResults := make([]DownloadResult, 0, 1)
	for _, result := range results {
		if result.Type == MediaTypeActress {
			actressResults = append(actressResults, result)
		}
	}
	require.Len(t, actressResults, 1)
	assert.True(t, actressResults[0].Downloaded)
	assert.Equal(t, int32(1), requests.Load())
}

func TestDownloadPoster_OverwriteDirectReplacesExisting(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("new poster"))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	path := "/output/TEST-007-poster.jpg"
	require.NoError(t, afero.WriteFile(fs, path, []byte("old poster"), 0644))
	d := NewDownloader(server.Client(), fs, &Config{
		DownloadPoster: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "<ID>-poster.jpg",
		},
	}, nil)
	result, err := d.downloadPoster(context.Background(), &models.Movie{
		ID:     "TEST-007",
		Poster: models.PosterState{PosterURL: server.URL + "/poster.jpg"},
	}, "/output", nil, true, &sync.Map{}, downloadLedger{opID: "test-op-direct", recorder: &armedTestLedger{}})
	require.NoError(t, err)
	assert.Equal(t, int32(1), requests.Load())
	assert.True(t, result.Downloaded)
	assert.True(t, result.Replaced)
	assertNoUniqueTemps(t, fs, "/output")
}

func TestDownloadPoster_OverwriteCroppedCreatesNewFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 600, 400))
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	d := NewDownloader(server.Client(), fs, &Config{
		DownloadPoster: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "<ID>-poster.jpg",
		},
	}, nil)
	path := "/output/TEST-008-poster.jpg"
	result, err := d.downloadPoster(context.Background(), &models.Movie{
		ID:     "TEST-008",
		Poster: models.PosterState{CoverURL: server.URL + "/cover.jpg", ShouldCropPoster: true},
	}, "/output", nil, true, &sync.Map{})
	require.NoError(t, err)
	assert.True(t, result.Downloaded)
	assert.False(t, result.Replaced)
	_, statErr := fs.Stat(path)
	assert.NoError(t, statErr)
	assertNoUniqueTemps(t, fs, "/output")
}
func TestDownloadPoster_OverwriteCroppedReplacesAndCleansTemps(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 600, 400))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	posterPath := nativePath("/output/TEST-003-poster.jpg")
	require.NoError(t, afero.WriteFile(fs, posterPath, []byte("old poster"), 0644))
	d := NewDownloader(server.Client(), fs, &Config{
		DownloadPoster:    true,
		MediaFormatConfig: organizer.MediaFormatConfig{PosterFormat: "<ID>-poster.jpg"},
	}, nil)
	movie := &models.Movie{ID: "TEST-003", Poster: models.PosterState{CoverURL: server.URL + "/cover.jpg", ShouldCropPoster: true}}
	result, err := d.downloadPoster(context.Background(), movie, "/output", nil, true, &sync.Map{}, downloadLedger{opID: "test-op-" + t.Name(), recorder: &armedTestLedger{}})
	require.NoError(t, err)
	require.True(t, result.Downloaded)
	assert.True(t, result.Replaced)
	assert.Equal(t, posterPath, result.LocalPath)
	got, readErr := afero.ReadFile(fs, posterPath)
	require.NoError(t, readErr)
	assert.NotEqual(t, []byte("old poster"), got)
	assertNoUniqueTemps(t, fs, "/output")
}

func TestDownloadPoster_OverwriteCropFailurePreservesExisting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("not an image"))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	posterPath := nativePath("/output/TEST-004-poster.jpg")
	old := []byte("old poster")
	require.NoError(t, afero.WriteFile(fs, posterPath, old, 0644))
	d := NewDownloader(server.Client(), fs, &Config{DownloadPoster: true}, nil)
	movie := &models.Movie{ID: "TEST-004", Poster: models.PosterState{CoverURL: server.URL + "/cover.jpg", ShouldCropPoster: true}}
	result, err := d.downloadPoster(context.Background(), movie, "/output", nil, true, &sync.Map{}, downloadLedger{opID: "test-op-" + t.Name(), recorder: &armedTestLedger{}})
	require.Error(t, err)
	assert.False(t, result.Downloaded)
	got, readErr := afero.ReadFile(fs, posterPath)
	require.NoError(t, readErr)
	assert.Equal(t, old, got)
	assertNoUniqueTemps(t, fs, "/output")
}

func TestDownload_OverwriteStatErrorDoesNotFetch(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("unexpected"))
	}))
	defer server.Close()

	base := afero.NewMemMapFs()
	path := nativePath("/output/TEST-005-fanart.jpg")
	statErr := errors.New("permission denied")
	fs := statErrorFS{Fs: base, path: path, err: statErr}
	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true}, nil)
	result, err := d.download(context.Background(), server.URL+"/cover.jpg", path, MediaTypeCover, true, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, statErr)
	assert.Equal(t, int32(0), requests.Load())
	assert.False(t, result.Downloaded)
}

// P3 rewrite of the replace-failure contract: the install-time wedge now
// rolls the destination back to the PRE-EXISTING bytes via the aside backup
// (previously the destination simply never got swapped). Error surfaces, dest
// intact, no residue.
func TestDownload_OverwriteReplaceFailurePreservesExisting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("new bytes"))
	}))
	defer server.Close()

	base := afero.NewMemMapFs()
	path := "/output/TEST-006-fanart.jpg"
	old := []byte("old bytes")
	require.NoError(t, afero.WriteFile(base, path, old, 0644))
	fs := rejectStagedRenameFS{Fs: base}
	d := NewDownloader(server.Client(), fs, &Config{DownloadCover: true}, nil)
	rec := &armedTestLedger{}
	result, err := d.download(context.Background(), server.URL+"/cover.jpg", path, MediaTypeCover, true, nil, downloadLedger{opID: "test-op-006", recorder: rec})
	require.Error(t, err, "staged install rejection surfaces")
	assert.False(t, result.Downloaded)
	got, readErr := afero.ReadFile(base, path)
	require.NoError(t, readErr)
	assert.Equal(t, old, got, "backup restored over the failed install")
	assertNoUniqueTemps(t, base, "/output")
	entries, _ := afero.ReadDir(base, "/output")
	for _, e := range entries {
		assert.False(t, strings.Contains(e.Name(), ".dlbak."), "backup swept after restore: %s", e.Name())
	}
	assert.Empty(t, rec.get(), "rollback consumed its backup: journal entry retracted via ReleaseReplacement")
	rec.mu.Lock()
	released := append([]replacementRecord(nil), rec.released...)
	rec.mu.Unlock()
	assert.Len(t, released, 1, "the journaled entry was released on rollback, not left dangling")
}

func TestDownload_DedupSharedDestinationClaimsOnce(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("shared bytes"))
	}))
	defer server.Close()

	fs := afero.NewMemMapFs()
	d := NewDownloader(server.Client(), fs, &Config{}, nil)
	path := "/output/shared.jpg"
	dedup := &sync.Map{}
	results := make([]*DownloadResult, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], errs[index] = d.download(context.Background(), server.URL+"/shared.jpg", path, MediaTypeTrailer, true, dedup)
		}(i)
	}
	wg.Wait()

	assert.Equal(t, int32(1), requests.Load())
	assert.NoError(t, errs[0])
	assert.NoError(t, errs[1])
	downloaded := 0
	skipped := 0
	for _, result := range results {
		if result.Downloaded {
			downloaded++
		}
		if result.Skipped {
			skipped++
		}
	}
	assert.Equal(t, 1, downloaded)
	assert.Equal(t, 1, skipped)
	assertNoUniqueTemps(t, fs, "/output")
	// The trailing solo leg carries an armed ledger — the destination now
	// exists, and destructive replaces always journal (P3 rule).
	result, err := d.download(context.Background(), server.URL+"/shared.jpg", path, MediaTypeTrailer, true, &sync.Map{}, downloadLedger{opID: "test-op-solo", recorder: &armedTestLedger{}})
	require.NoError(t, err)
	assert.True(t, result.Downloaded)
	assert.True(t, result.Replaced, "armed overwrite of the shared dest classifies as replace")
	assert.Equal(t, int32(2), requests.Load())
}

type stagedHTTPClient struct {
	requests atomic.Int32
	ready    chan struct{}
	release  <-chan struct{}
}

func (c *stagedHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.requests.Add(1)
	return &http.Response{
		StatusCode:    http.StatusOK,
		Body:          &stagedHTTPBody{ready: c.ready, release: c.release},
		Header:        make(http.Header),
		ContentLength: 2,
		Request:       req,
	}, nil
}

type stagedHTTPBody struct {
	ready     chan<- struct{}
	release   <-chan struct{}
	notified  bool
	remaining int
}

func (b *stagedHTTPBody) Read(p []byte) (int, error) {
	if !b.notified {
		b.notified = true
		b.remaining = 2
		p[0] = 'x'
		b.remaining--
		b.ready <- struct{}{}
		<-b.release
		return 1, nil
	}
	if b.remaining > 0 {
		p[0] = 'y'
		b.remaining--
		return 1, io.EOF
	}
	return 0, io.EOF
}

func (b *stagedHTTPBody) Close() error { return nil }

func TestDownload_UniqueTempsWithoutDedup(t *testing.T) {
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	client := &stagedHTTPClient{ready: ready, release: release}
	defer releaseAll()

	fs := afero.NewMemMapFs()
	d := NewDownloader(client, fs, &Config{}, nil)
	path := "/output/collision.jpg"
	var wg sync.WaitGroup
	results := make([]*DownloadResult, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], errs[index] = d.download(context.Background(), "https://example.test/collision.jpg", path, MediaTypeTrailer, true, nil)
		}(i)
	}
	<-ready
	<-ready
	entries, readErr := afero.ReadDir(fs, "/output")
	require.NoError(t, readErr)
	tempCount := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			tempCount++
		}
	}
	assert.Equal(t, 2, tempCount)
	releaseAll()
	wg.Wait()

	assert.Equal(t, int32(2), client.requests.Load())
	assert.NoError(t, errs[0])
	assert.NoError(t, errs[1])
	// P3 create-vs-create: no dedup map, no armed ledger — the race loser
	// classifies "exists" inside the dest lock at install time and SKIPS
	// instead of clobbering the winner's bytes (conflict over clobber).
	downloaded := 0
	skipped := 0
	for _, r := range results {
		if r.Downloaded {
			downloaded++
		}
		if r.Skipped {
			skipped++
		}
	}
	assert.Equal(t, 1, downloaded, "exactly one winner installs its bytes")
	assert.Equal(t, 1, skipped, "the race loser skips")
	assertNoUniqueTemps(t, fs, "/output")
}

func assertNoUniqueTemps(t *testing.T, fs afero.Fs, dir string) {
	entries, err := afero.ReadDir(fs, dir)
	require.NoError(t, err)
	for _, entry := range entries {
		assert.False(t, strings.HasSuffix(entry.Name(), ".tmp"), "temporary file remains: %s", filepath.Join(dir, entry.Name()))
		assert.False(t, strings.HasSuffix(entry.Name(), ".full.tmp"), "full temporary file remains: %s", filepath.Join(dir, entry.Name()))
		assert.False(t, strings.HasSuffix(entry.Name(), ".crop.tmp"), "crop temporary file remains: %s", filepath.Join(dir, entry.Name()))
	}
}
