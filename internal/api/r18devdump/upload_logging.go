package r18devdump

import (
	"errors"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
)

// Upload errors may contain arbitrary row values, filenames or source URLs.
// Keep those in the operation's status response, not in a new server log.
// ImportBatchError is deliberately payload-free, so its counts/key columns
// are safe to log and help diagnose strict upload failures.
func logUploadFailure(kind string, err error) {
	entry := logging.WithField("error_kind", kind)
	var batchErr *r18devdump.ImportBatchError
	if errors.As(err, &batchErr) {
		entry.WithField("batch_error", batchErr.Error()).Warn("r18dev dump upload failed")
		return
	}
	entry.Warn("r18dev dump upload failed; see dump status for details")
}
