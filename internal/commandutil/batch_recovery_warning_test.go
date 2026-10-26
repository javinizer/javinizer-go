package commandutil

import (
	"bytes"
	"context"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCLICommittedRecoveryWarningVisible(t *testing.T) {
	const warning = "Output committed; backup cleanup pending. Do not reapply."
	for _, update := range []bool{false, true} {
		var buf bytes.Buffer
		emitter := &recordingEmitter{}
		hook := cliBatchPostApply(emitter, &buf, "warning-job", false, update, &atomic.Int64{}, &sync.Mutex{})
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		hook(ctx, &worker.ApplyFileContext{FilePath: "/f.mp4", Movie: &models.Movie{ID: "TEST-001"}}, &worker.ApplyFileResult{Result: &workflow.ApplyResult{RecoveryWarning: warning, PublicationFinalized: true}})
		require.Contains(t, buf.String(), warning)
		found := false
		for _, event := range emitter.events {
			require.NotEqual(t, models.SeverityError, event.Severity)
			if event.Severity == models.SeverityWarn {
				require.Contains(t, event.Message, warning)
				found = true
			}
		}
		require.True(t, found)
	}
}
