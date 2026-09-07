package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/assetidentity"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

func identityServer(t *testing.T, source []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	hits := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(source)
	}))
	t.Cleanup(server.Close)
	return server, hits
}

type identityReadFS struct {
	afero.Fs
	openErr   error
	readErr   error
	afterRead func(string) error
}

func (f *identityReadFS) Open(name string) (afero.File, error) {
	if strings.HasSuffix(name, ".full.tmp") {
		if f.openErr != nil {
			return nil, f.openErr
		}
		file, err := f.Fs.Open(name)
		if err != nil {
			return nil, err
		}
		return &identityReadFile{File: file, readErr: f.readErr, afterRead: f.afterRead}, nil
	}
	return f.Fs.Open(name)
}

type identityReadFile struct {
	afero.File
	readErr   error
	afterRead func(string) error
}

func (f *identityReadFile) Read(p []byte) (int, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}
	return f.File.Read(p)
}

func (f *identityReadFile) Close() error {
	err := f.File.Close()
	if err == nil && f.afterRead != nil {
		return f.afterRead(f.Name())
	}
	return err
}

func requireIdentityRefusal(t *testing.T, result *DownloadResult, err error, reason PosterRecropReason, bounds *models.CropBounds) {
	t.Helper()
	require.ErrorIs(t, err, ErrPosterRecropRequired)
	require.ErrorIs(t, result.Error, ErrPosterRecropRequired)
	var refusal *PosterRecropRequiredError
	require.ErrorAs(t, fmt.Errorf("apply poster: %w", err), &refusal)
	require.Equal(t, reason, refusal.Reason)
	require.Equal(t, *bounds, refusal.Bounds)
	require.False(t, result.Downloaded)
	require.False(t, result.Replaced)
	require.Empty(t, result.LocalPath)
	require.Zero(t, result.Size)
}

func TestDownloadPoster_RecropAggregation(t *testing.T) {
	source := twoToneSourceBytes(t)
	server, _ := identityServer(t, source)
	root := t.TempDir()
	fs := afero.NewMemMapFs()
	movie := geometryMovie(server.URL+"/poster.jpg", &models.CropBounds{Width: .4, Height: 1}, true)
	movie.Poster.CoverURL = server.URL + "/cover.jpg"
	d := newGeometryDownloader(fs)
	d.config.DownloadCover = true
	d.config.FanartFormat = "<ID>-fanart.jpg"
	outcome, err := d.Download(context.Background(), DownloadCmd{Movie: movie, DestDir: root})
	require.ErrorIs(t, err, ErrPosterRecropRequired)
	require.NotNil(t, outcome)
	coverPath := filepath.Join(root, "IPX-535-fanart.jpg")
	require.Equal(t, []string{coverPath}, outcome.CreatedPaths)
	require.Equal(t, []string{coverPath}, outcome.DownloadedPaths)
	var cover, poster *DownloadResult
	for _, result := range outcome.Results {
		switch result.Type {
		case MediaTypeCover:
			cover = &result
		case MediaTypePoster:
			poster = &result
		}
	}
	require.NotNil(t, cover)
	require.True(t, cover.Downloaded)
	require.NotNil(t, poster)
	requireIdentityRefusal(t, poster, err, SourceFingerprintMissing, movie.Poster.PosterCropBounds)
	got, err := afero.ReadFile(fs, coverPath)
	require.NoError(t, err)
	require.Equal(t, source, got)
}

func TestDownloadPoster_IdentityUnrelatedPaths(t *testing.T) {
	for _, mode := range []string{"disabled", "no URL", "reservation skip", "existing legacy", "no manual", "non-full legacy", "network", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			fs := afero.NewMemMapFs()
			source := twoToneSourceBytes(t)
			server, hits := identityServer(t, source)
			d := newGeometryDownloader(fs)
			bounds := &models.CropBounds{Width: .4, Height: 1}
			movie := geometryMovie(server.URL, bounds, true)
			ctx := context.Background()
			overwrite := false
			dedup := &sync.Map{}
			switch mode {
			case "disabled":
				d.config.DownloadPoster = false
			case "no URL":
				movie.Poster.PosterURL = ""
			case "reservation skip":
				overwrite = true
				dedup.Store(filepath.Join(root, "IPX-535-poster.jpg"), struct{}{})
			case "existing legacy":
				require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "IPX-535-poster.jpg"), []byte("old"), 0600))
			case "no manual":
				movie.Poster.PosterCropBounds = nil
				movie.Poster.ShouldCropPoster = false
			case "non-full legacy":
				movie.Poster.PosterCropSourceFull = false
				movie.Poster.ShouldCropPoster = false
			case "network":
				bounds.SourceFingerprint = assetidentity.FromBytes(source).Fingerprint
				server.Close()
			case "cancelled":
				bounds.SourceFingerprint = assetidentity.FromBytes(source).Fingerprint
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			result, err := d.downloadPoster(ctx, movie, root, nil, overwrite, dedup)
			require.NotErrorIs(t, err, ErrPosterRecropRequired)
			switch mode {
			case "network", "cancelled":
				require.Error(t, err)
				require.False(t, result.Downloaded)
				if mode == "cancelled" {
					require.ErrorIs(t, err, context.Canceled)
				}
			case "no manual", "non-full legacy":
				require.NoError(t, err)
				require.True(t, result.Downloaded)
				got, err := afero.ReadFile(fs, result.LocalPath)
				require.NoError(t, err)
				require.Equal(t, source, got)
				require.Equal(t, int32(1), hits.Load())
			default:
				require.NoError(t, err)
				require.False(t, result.Downloaded)
				require.Zero(t, hits.Load())
				if mode == "reservation skip" {
					require.True(t, result.Skipped)
				}
			}
		})
	}
}

func TestDownloadPoster_CropConsumesVerifiedSnapshot(t *testing.T) {
	a := p4JPEG(color.RGBA{R: 20, A: 255})
	b := p4JPEG(color.RGBA{R: 220, G: 220, B: 220, A: 255})
	for _, native := range []bool{false, true} {
		for _, auto := range []bool{false, true} {
			t.Run(fmt.Sprintf("native=%t/auto=%t", native, auto), func(t *testing.T) {
				root := t.TempDir()
				var base afero.Fs = afero.NewMemMapFs()
				if native {
					base = afero.NewOsFs()
				}
				swapped := false
				fs := &identityReadFS{Fs: base, afterRead: func(name string) error {
					swapped = true
					return afero.WriteFile(base, name, b, 0600)
				}}
				server, hits := identityServer(t, a)
				bounds := &models.CropBounds{Width: .4, Height: 1, SourceAspect: 1000.0 / 600, SourceFingerprint: assetidentity.FromBytes(a).Fingerprint}
				result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), geometryMovie(server.URL, bounds, auto), root, nil)
				require.NoError(t, err)
				require.True(t, swapped)
				require.True(t, result.Downloaded)
				img, width, height := decodeResultPoster(t, base, result.LocalPath)
				require.Equal(t, 400, width)
				require.Equal(t, 600, height)
				require.Less(t, sampleLuma(img, .5, .5), 40.0, "saved rectangle must consume A, not the substituted white B")
				require.Equal(t, int32(1), hits.Load())
			})
		}
	}
}

func TestDownloadPoster_ByteIdentityDefinition(t *testing.T) {
	a := p4JPEG(color.RGBA{R: 20, A: 255})
	b := append([]byte{}, a[:2]...)
	b = append(b, 0xff, 0xfe, 0, 6, 'n', 'o', 't', 'e')
	b = append(b, a[2:]...)
	imageA, _, err := image.Decode(bytes.NewReader(a))
	require.NoError(t, err)
	imageB, _, err := image.Decode(bytes.NewReader(b))
	require.NoError(t, err)
	require.Equal(t, imageA, imageB)
	require.NotEqual(t, assetidentity.FromBytes(a).Fingerprint, assetidentity.FromBytes(b).Fingerprint)
	for _, metadataChanged := range []bool{false, true} {
		t.Run(fmt.Sprint(metadataChanged), func(t *testing.T) {
			source := a
			if metadataChanged {
				source = b
			}
			server, hits := identityServer(t, source)
			root := t.TempDir()
			fs := afero.NewMemMapFs()
			reviewPath := filepath.Join(root, "review", "original.jpg")
			require.NoError(t, afero.WriteFile(fs, reviewPath, a, 0600))
			measured, err := assetidentity.Measure(fs, reviewPath)
			require.NoError(t, err)
			bounds := &models.CropBounds{Width: .4, Height: 1, SourceAspect: 1000.0 / 600, SourceFingerprint: strings.ToUpper(measured.Fingerprint)}
			result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), geometryMovie(server.URL+"/different/location.jpg", bounds, false), filepath.Join(root, "apply"), nil)
			if metadataChanged {
				requireIdentityRefusal(t, result, err, SourceFingerprintMismatch, bounds)
			} else {
				require.NoError(t, err)
				require.True(t, result.Downloaded)
			}
			require.Equal(t, int32(1), hits.Load())
		})
	}
}

func TestDownloadPoster_LegacyGeometryCannotAuthorize(t *testing.T) {
	source := p4JPEG(color.RGBA{R: 20, A: 255})
	identity := assetidentity.FromBytes(source)
	for _, auto := range []bool{false, true} {
		for _, fingerprint := range []string{"", "malformed"} {
			for _, width := range []float64{.4, 1.5} {
				t.Run(fmt.Sprintf("auto=%t/fingerprint=%s/width=%g", auto, fingerprint, width), func(t *testing.T) {
					server, hits := identityServer(t, source)
					wire, err := json.Marshal(map[string]any{"x": 0, "y": 0, "width": width, "height": 1, "source_aspect": 1000.0 / 600, "source_revision": identity.Revision, "source_fingerprint": fingerprint})
					require.NoError(t, err)
					var bounds models.CropBounds
					require.NoError(t, json.Unmarshal(wire, &bounds))
					movie := geometryMovie(server.URL, &bounds, auto)
					result, err := newGeometryDownloader(afero.NewMemMapFs()).downloadPoster(context.Background(), movie, t.TempDir(), nil)
					reason := SourceFingerprintMissing
					if fingerprint != "" {
						reason = SourceFingerprintInvalid
					}
					requireIdentityRefusal(t, result, err, reason, &bounds)
					require.Equal(t, fingerprint, movie.Poster.PosterCropBounds.SourceFingerprint)
					require.Zero(t, hits.Load())
				})
			}
		}
	}
}

func TestDownloadPoster_SourceReplacement(t *testing.T) {
	a := p4JPEG(color.RGBA{R: 20, A: 255})
	b := p4JPEG(color.RGBA{G: 220, A: 255})
	for _, native := range []bool{false, true} {
		for _, auto := range []bool{false, true} {
			for _, overwrite := range []bool{false, true} {
				for _, existing := range []bool{false, true} {
					t.Run(fmt.Sprintf("native=%t/auto=%t/overwrite=%t/existing=%t", native, auto, overwrite, existing), func(t *testing.T) {
						root := t.TempDir()
						var fs afero.Fs = afero.NewMemMapFs()
						if native {
							fs = afero.NewOsFs()
						}
						dest := filepath.Join(root, "IPX-535-poster.jpg")
						old := []byte("existing poster must survive")
						if existing {
							require.NoError(t, afero.WriteFile(fs, dest, old, 0600))
						}
						var replaced atomic.Bool
						var hits atomic.Int32
						server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
							hits.Add(1)
							w.Header().Set("Content-Type", "image/jpeg")
							if replaced.Load() {
								_, _ = w.Write(b)
							} else {
								_, _ = w.Write(a)
							}
						}))
						defer server.Close()
						response, err := server.Client().Get(server.URL + "/single-use.jpg")
						require.NoError(t, err)
						reviewed, err := io.ReadAll(response.Body)
						require.NoError(t, err)
						require.NoError(t, response.Body.Close())
						bounds := &models.CropBounds{Width: .4, Height: 1, SourceAspect: 1000.0 / 600, SourceFingerprint: assetidentity.FromBytes(reviewed).Fingerprint}
						replaced.Store(true)
						hits.Store(0)
						movie := geometryMovie(server.URL+"/single-use.jpg", bounds, auto)
						result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), movie, root, nil, overwrite)
						if existing && !overwrite {
							require.NoError(t, err)
							require.False(t, result.Downloaded)
							require.False(t, result.Replaced)
							require.Equal(t, dest, result.LocalPath)
							require.Zero(t, hits.Load())
						} else {
							requireIdentityRefusal(t, result, err, SourceFingerprintMismatch, bounds)
							require.Equal(t, int32(1), hits.Load())
						}
						if existing {
							got, err := afero.ReadFile(fs, dest)
							require.NoError(t, err)
							require.Equal(t, old, got)
						} else {
							_, err := fs.Stat(dest)
							require.ErrorIs(t, err, os.ErrNotExist)
						}
						entries, _ := afero.ReadDir(fs, root)
						if existing {
							require.Len(t, entries, 1)
						} else {
							require.Empty(t, entries)
						}
					})
				}
			}
		}
	}
}

func TestDownloadPoster_ManualCropIdentity(t *testing.T) {
	a := p4JPEG(color.RGBA{R: 20, A: 255})
	b := p4JPEG(color.RGBA{G: 220, A: 255})
	fingerprint := assetidentity.FromBytes(a).Fingerprint
	cause := errors.New("source measurement unavailable")
	for _, tc := range []struct {
		name, fingerprint string
		source            []byte
		reason            PosterRecropReason
		openErr, readErr  error
	}{
		{name: "missing", source: a, reason: SourceFingerprintMissing},
		{name: "malformed length", fingerprint: "abc", source: a, reason: SourceFingerprintInvalid},
		{name: "malformed hex", fingerprint: strings.Repeat("g", 64), source: a, reason: SourceFingerprintInvalid},
		{name: "matching", fingerprint: fingerprint, source: a},
		{name: "uppercase", fingerprint: strings.ToUpper(fingerprint), source: a},
		{name: "mismatch", fingerprint: fingerprint, source: b, reason: SourceFingerprintMismatch},
		{name: "unmeasurable open", fingerprint: fingerprint, source: a, reason: SourceIdentityUnavailable, openErr: cause},
		{name: "unmeasurable read", fingerprint: fingerprint, source: a, reason: SourceIdentityUnavailable, readErr: cause},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			base := afero.NewMemMapFs()
			fs := &identityReadFS{Fs: base, openErr: tc.openErr, readErr: tc.readErr}
			server, hits := identityServer(t, tc.source)
			bounds := &models.CropBounds{Width: .4, Height: 1, SourceAspect: 1000.0 / 600, SourceFingerprint: tc.fingerprint}
			movie := geometryMovie(server.URL+"/source.jpg", bounds, true)
			before := movie.Clone()
			var logs bytes.Buffer
			restore := logging.SetOutput(&logs)
			defer restore()
			result, err := newGeometryDownloader(fs).downloadPoster(context.Background(), movie, root, nil)
			require.Equal(t, before, movie, "downloader must never mutate review intent")
			if tc.reason == "" {
				require.NoError(t, err)
				require.True(t, result.Downloaded)
				img, width, height := decodeResultPoster(t, base, result.LocalPath)
				require.Equal(t, 400, width)
				require.Equal(t, 600, height)
				require.Less(t, sampleLuma(img, .5, .5), 40.0)
			} else {
				requireIdentityRefusal(t, result, err, tc.reason, bounds)
				require.Contains(t, logs.String(), "code="+PosterRecropRequiredCode)
				require.Contains(t, logs.String(), "reason="+string(tc.reason))
				_, statErr := base.Stat(filepath.Join(root, "IPX-535-poster.jpg"))
				require.ErrorIs(t, statErr, os.ErrNotExist)
				entries, _ := afero.ReadDir(base, root)
				require.Empty(t, entries)
				if tc.reason == SourceIdentityUnavailable {
					require.ErrorIs(t, fmt.Errorf("workflow: %w", err), cause)
					require.Contains(t, err.Error(), cause.Error())
				}
			}
			if tc.reason == SourceFingerprintMissing || tc.reason == SourceFingerprintInvalid {
				require.Zero(t, hits.Load())
			} else {
				require.Equal(t, int32(1), hits.Load())
			}
		})
	}
}
