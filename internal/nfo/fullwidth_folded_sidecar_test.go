package nfo

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestFindNFOMixedWidthSidecarPriority(t *testing.T) {
	for _, perFile := range []bool{false, true} {
		for _, existing := range []string{"ＲＣＴ－１５６－ＨＤ.nfo", "ＲＣＴ－１５６－ＨＤ．ｎｆｏ", "RCT-156-HD.nfo", "RCT-156-HD．ｎｆｏ"} {
			t.Run(existing+map[bool]string{false: "/fallback", true: "/per-file"}[perFile], func(t *testing.T) {
				fs := afero.NewOsFs()
				dir := t.TempDir()
				rawDir := filepath.Join(dir, "資料　dir")
				require.NoError(t, fs.MkdirAll(rawDir, 0o755))
				sidecar := filepath.Join(rawDir, existing)
				require.NoError(t, afero.WriteFile(fs, sidecar, []byte("<movie><title>preserved</title></movie>"), 0o644))
				movie := &models.Movie{ID: "RCT-156H", Title: "fresh"}
				cfg := NFONameConfig{FilenameTemplate: "[<ID>] <TITLE>.nfo", PerFile: perFile, IsMultiPart: perFile}
				video := filepath.Join(rawDir, "ＲＣＴ－１５６－ＨＤ．ｍｋｖ")
				found := findNFOFile(fs, rawDir, movie, cfg, video, nil)
				require.Equal(t, sidecar, found)
				body, err := afero.ReadFile(fs, found)
				require.NoError(t, err)
				assert.Contains(t, string(body), "preserved")
				// An exact raw-stem sidecar wins over either folded alternate.
				raw := filepath.Join(rawDir, "ＲＣＴ－１５６－ＨＤ.nfo")
				require.NoError(t, afero.WriteFile(fs, raw, []byte("raw priority"), 0o644))
				assert.Equal(t, raw, findNFOFile(fs, rawDir, movie, cfg, video, nil))
			})
		}
	}
}

func TestNFOSidecarFoldStaysWithinFilename(t *testing.T) {
	assert.Equal(t, []string{"ＲＣＴ.nfo", "ＲＣＴ．ｎｆｏ", "RCT.nfo", "RCT．ｎｆｏ"}, nfoSidecarFilenames("ＲＣＴ．ｍｋｖ"))
	for _, stem := range []string{"．．／foreign", "．．＼foreign", "Ｃ：foreign"} {
		names := nfoSidecarFilenames(stem + "．ｍｋｖ")
		assert.Equal(t, []string{stem + ".nfo", stem + "．ｎｆｏ"}, names)
	}
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/movies", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/movies/RCT-157-HD.nfo", []byte("foreign"), 0o644))
	assert.Empty(t, findNFOFile(fs, "/movies", &models.Movie{ID: "RCT-156H"}, NFONameConfig{}, "/movies/ＲＣＴ－１５６－ＨＤ．ｍｋｖ", nil))
}
