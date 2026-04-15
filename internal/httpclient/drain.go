package httpclient

import (
	"io"
)

func DrainAndClose(body io.ReadCloser) error {
	if body == nil {
		return nil
	}
	drainErr := drainBody(body)
	closeErr := body.Close()
	if closeErr != nil {
		return closeErr
	}
	return drainErr
}

func drainBody(body io.Reader) error {
	_, err := io.Copy(io.Discard, io.LimitReader(body, 1<<20))
	return err
}
