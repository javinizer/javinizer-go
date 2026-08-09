package worker

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// The committed-detection scan must glide over nil rows and nil-movie rows
// without flipping either gate — legless cases reverse-and-sweep cleanly.
func TestReconcileRekeySkipsNilRows(t *testing.T) {
	fs, dir := witnessFixture(t)
	w, _ := json.Marshal(rekeyWitness{OldID: "OLD-N", NewID: "NEW-N"})
	require.NoError(t, afero.WriteFile(fs, dir+"/.rekey-nil.json", w, 0o644))
	res := map[string]*resultstore.MovieResult{
		"/f/nilrow.mp4": nil,
		"/f/nilmovie.mp4": {
			ResultID:      "res-n",
			Revision:      3,
			Status:        models.JobStatusCompleted,
			Movie:         nil,
			FileMatchInfo: models.FileMatchInfo{Path: "/f/nilmovie.mp4"},
		},
	}
	payload, err := json.Marshal(res)
	require.NoError(t, err)
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(&models.Job{Results: string(payload)}, nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	_, wErr := fs.Stat(dir + "/.rekey-nil.json")
	assert.Error(t, wErr, "nothing to reverse → witness swept")
}
