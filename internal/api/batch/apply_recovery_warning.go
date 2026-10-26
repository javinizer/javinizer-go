package batch

import (
	"github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// Carry the operation's known warning rather than rereading a possibly failed
// persistence result. Generation stamping prevents an old apply's report from
// claiming a newer apply's result. This is reporting, not publication authority.
func makeOrganizeFileRecoveryWarningBroadcaster(job worker.BatchJobInterface, isUpdate bool, sink progressSink, generationRef *uint64) func(string, string) {
	return func(filePath, warning string) {
		status := websocket.ProgressStatus("organized")
		if isUpdate {
			status = websocket.ProgressStatus("updated")
		}
		sink(stampJobCountsForApply(&websocket.ProgressMessage{
			JobID: job.GetID(), FilePath: filePath, Status: status, Progress: 100,
			RecoveryWarning: warning,
		}, job, generationRef))
	}
}
