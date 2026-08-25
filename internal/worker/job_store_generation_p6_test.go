package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestJobStore_EditGenerationPublicationNeverLowers(t *testing.T) {
	store := freshStore(t)
	job := seedJobLifecycle(t, store, models.JobStatusCompleted, "")
	store.attachEditDeps(job)
	env := job.posterEditor.currentEnv()
	require.NotNil(t, env)

	job.mu.Lock()
	job.envelopeGeneration = 9
	job.mu.Unlock()
	env.generationCommitted(8)
	require.Equal(t, uint64(9), job.envelopeGeneration)
	env.generationCommitted(10)
	require.Equal(t, uint64(10), job.envelopeGeneration)
}
