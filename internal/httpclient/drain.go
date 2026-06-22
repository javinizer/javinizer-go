package httpclient

import (
	"io"

	"github.com/javinizer/javinizer-go/internal/logging"
)

func DrainAndClose(body io.ReadCloser) error {
	if body == nil {
		return nil
	}
	drainErr := drainBody(body)
	closeErr := body.Close()
	if closeErr != nil {
		if drainErr != nil {
			logging.Warnf("drain error suppressed by close error: %v", drainErr)
		}
		return closeErr
	}
	return drainErr
}

func drainBody(body io.Reader) error {
	_, err := io.Copy(io.Discard, io.LimitReader(body, 1<<20))
	return err
}
