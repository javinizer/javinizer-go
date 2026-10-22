package update_test

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	updatecmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/update"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/stretchr/testify/require"
)

// A recursive scan is not an output directory: each movie owns its sibling
// metadata and artwork, including when every movie uses the same artwork names.
func TestRun_InPlaceOutputRouting(t *testing.T) {
	for _, leaf := range []bool{false, true} {
		for _, existingMedia := range []bool{false, true} {
			for _, overwrite := range []bool{false, true} {
				t.Run(fmt.Sprintf("leaf=%t/existing_media=%t/overwrite=%t", leaf, existingMedia, overwrite), func(t *testing.T) {
					root := t.TempDir()
					ids := []string{"AAA-101", "BBB-202"}
					images := map[string][]byte{}
					originals := map[string][]byte{}
					dirs := map[string]string{}
					makeImage := func(c color.RGBA) []byte {
						var b bytes.Buffer
						img := image.NewRGBA(image.Rect(0, 0, 32, 32))
						for x := 0; x < 32; x++ {
							for y := 0; y < 32; y++ {
								img.Set(x, y, c)
							}
						}
						require.NoError(t, jpeg.Encode(&b, img, nil))
						return b.Bytes()
					}
					mediaNames := []string{"fanart.jpg", "poster.jpg", filepath.Join("extrafanart", "fanart1.jpg")}
					for i, id := range ids {
						images[id] = makeImage(color.RGBA{R: uint8(100 + i*100), G: 20, A: 255})
						originals[id] = makeImage(color.RGBA{G: uint8(100 + i*100), B: 20, A: 255})
						// Exercise native paths, spaces/non-ASCII names and unequal scan depths.
						dir := filepath.Join(root, "映画 collection", id)
						if i == 1 {
							dir = filepath.Join(root, "another level", "映画 collection", id)
						}
						dirs[id] = dir
						require.NoError(t, os.MkdirAll(dir, 0700))
						require.NoError(t, os.WriteFile(filepath.Join(dir, id+".mp4"), []byte("synthetic video "+id), 0600))
						require.NoError(t, os.WriteFile(filepath.Join(dir, id+".nfo"), []byte("<movie><title>Original "+id+"</title></movie>"), 0600))
						if existingMedia {
							for _, name := range mediaNames {
								path := filepath.Join(dir, name)
								require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
								require.NoError(t, os.WriteFile(path, originals[id], 0600))
							}
						}
					}
					var srv *httptest.Server
					srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path == "/search" {
							id := r.URL.Query().Get("q")
							if _, ok := images[id]; !ok {
								t.Errorf("unexpected search ID %q", id)
								http.NotFound(w, r)
								return
							}
							w.Header().Set("Content-Type", "application/json")
							_ = json.NewEncoder(w).Encode(map[string]any{"normalized_id": id, "title": "Synthetic " + id, "cover_image_url": srv.URL + "/" + id + ".jpg", "sample_image_urls": []string{srv.URL + "/" + id + ".jpg"}})
							return
						}
						id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".jpg")
						data, ok := images[id]
						if !ok {
							t.Errorf("unexpected image path %q", r.URL.Path)
							http.NotFound(w, r)
							return
						}
						w.Header().Set("Content-Type", "image/jpeg")
						_, _ = w.Write(data)
					}))
					defer srv.Close()
					cfgPath, _ := createOfflineUpdateConfig(t, true, func(cfg *config.Config) {
						cfg.Scrapers.Override("libredmm").BaseURL = srv.URL
						cfg.Performance.MaxWorkers = 1
						cfg.Output.Download.DownloadCover = true
						cfg.Output.Download.DownloadPoster = true
						cfg.Output.MediaFormat.PosterFormat = "poster.jpg"
						cfg.Output.MediaFormat.FanartFormat = "fanart.jpg"
						cfg.Metadata.NFO.Format.DisplayTitle = ""
					})
					invoke := func(path string) {
						t.Helper()
						cmd := updatecmd.NewCommand()
						var out bytes.Buffer
						cmd.SetOut(&out)
						require.NoError(t, cmd.Flags().Set("force-overwrite", "true"))
						require.NoError(t, cmd.Flags().Set("extrafanart", "true"))
						require.NoError(t, cmd.Flags().Set("overwrite-existing-media", fmt.Sprint(overwrite)))
						require.NoError(t, updatecmd.Run(cmd, []string{path}, cfgPath), out.String())
					}
					if leaf {
						for _, id := range ids {
							invoke(dirs[id])
						}
					} else {
						invoke(root)
					}
					for _, id := range ids {
						dir := dirs[id]
						data, err := os.ReadFile(filepath.Join(dir, id+".mp4"))
						require.NoError(t, err)
						require.Equal(t, "synthetic video "+id, string(data))
						data, err = os.ReadFile(filepath.Join(dir, id+".nfo"))
						require.NoError(t, err)
						var movie nfo.Movie
						require.NoError(t, xml.Unmarshal(data, &movie))
						require.Equal(t, "Synthetic "+id, movie.Title)
						expected := images[id]
						if existingMedia && !overwrite {
							expected = originals[id]
						}
						for _, name := range mediaNames {
							data, err = os.ReadFile(filepath.Join(dir, name))
							require.NoError(t, err)
							if name == "poster.jpg" && !(existingMedia && !overwrite) {
								// Posters may be cropped/re-encoded; check the owning movie's color.
								img, err := jpeg.Decode(bytes.NewReader(data))
								require.NoError(t, err)
								want, err := jpeg.Decode(bytes.NewReader(expected))
								require.NoError(t, err)
								r, g, b, _ := img.At(img.Bounds().Min.X, img.Bounds().Min.Y).RGBA()
								wr, wg, wb, _ := want.At(0, 0).RGBA()
								require.InDelta(t, wr, r, 512)
								require.InDelta(t, wg, g, 512)
								require.InDelta(t, wb, b, 512)
							} else {
								require.Equal(t, expected, data, name+" must belong to "+id)
							}
						}
					}
					// No artifacts (including backups) may leak into the scanned ancestors.
					require.NoError(t, filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
						if err != nil {
							return err
						}
						if info.IsDir() {
							return nil
						}
						for _, dir := range dirs {
							rel, err := filepath.Rel(dir, path)
							if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
								return nil
							}
						}
						t.Errorf("artifact outside a movie directory: %s", path)
						return nil
					}))
				})
			}
		}
	}
}
