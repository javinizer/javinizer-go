package poster

import (
	"net/http"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoveAssets pins the cleanup half of clearing the last poster source:
// the cached -full.jpg and preview go away (already-absent files are fine)
// and non-not-exist removal failures surface so the caller can reject the
// edit instead of persisting a state it cannot roll back.
func TestRemoveAssets(t *testing.T) {
	cases := []struct {
		name      string
		seedFull  []byte // nil = absent
		seedPrev  []byte // nil = absent
		jobID     string
		posterID  string
		failRem   bool
		wantErr   string
		wantGoneF bool // -full.jpg absent afterwards
		wantGoneP bool // preview absent afterwards
	}{
		{name: "both assets removed", seedFull: []byte("full"), seedPrev: []byte("prev"), wantGoneF: true, wantGoneP: true},
		{name: "nothing cached is not an error", wantGoneF: true, wantGoneP: true},
		{name: "only the full image cached", seedFull: []byte("full"), wantGoneF: true, wantGoneP: true},
		{name: "only the preview cached", seedPrev: []byte("prev"), wantGoneF: true, wantGoneP: true},
		{name: "invalid job id rejected", jobID: "..", wantErr: ""},
		{name: "invalid poster id rejected", posterID: "a/b", wantErr: ""},
		{name: "removal failure surfaces", seedFull: []byte("full"), seedPrev: []byte("prev"), failRem: true, wantErr: "remove poster asset"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			var fs afero.Fs = afero.NewMemMapFs()
			if tt.failRem {
				fs = &errFS{Fs: fs, failRemove: true}
			}
			pm := NewPosterManager(fs, "/tmp", http.DefaultClient)
			jobID, posterID := tt.jobID, tt.posterID
			if jobID == "" {
				jobID = snapJobID
			}
			if posterID == "" {
				posterID = snapID
			}
			fullPath, previewPath := snapshotPaths(fs, "/tmp", tt.seedFull, tt.seedPrev)

			err := pm.RemoveAssets(jobID, posterID)

			if tt.jobID == ".." || tt.posterID == "a/b" {
				require.Error(t, err)
				return
			}
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Contains(t, err.Error(), "injected remove failure")
				return
			}
			require.NoError(t, err)
			paths := map[string]bool{fullPath: tt.wantGoneF, previewPath: tt.wantGoneP}
			for path, wantGone := range paths {
				_, statErr := fs.Stat(path)
				assert.Equal(t, wantGone, os.IsNotExist(statErr), "%s gone=%v", path, wantGone)
			}
		})
	}
}

// TestScrapePosterGenerator_RemovePosterAssets covers the generator adapter:
// a manager-less generator holds no assets (no-op), and a managed one removes
// both cached assets.
func TestScrapePosterGenerator_RemovePosterAssets(t *testing.T) {
	t.Run("no manager is a no-op", func(t *testing.T) {
		gen := NewScrapePosterGenerator(nil, "", "")
		assert.NoError(t, gen.RemovePosterAssets(snapJobID, snapID))
	})
	t.Run("managed removal clears both assets", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		gen := NewScrapePosterGenerator(NewPosterManager(fs, "/tmp", http.DefaultClient), "", "")
		fullPath, previewPath := snapshotPaths(fs, "/tmp", []byte("full"), []byte("prev"))

		require.NoError(t, gen.RemovePosterAssets(snapJobID, snapID))

		for _, p := range []string{fullPath, previewPath} {
			_, statErr := fs.Stat(p)
			assert.True(t, os.IsNotExist(statErr), "%s must be removed", p)
		}
	})
}
