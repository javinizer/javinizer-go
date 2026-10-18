package r18devdump

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDownload_EOFProbeTimeoutPreservesGzipValidation(t *testing.T) {
	first := gzipped(t, "first member\n")
	second := gzipped(t, "second member\n")
	damaged := bytes.Clone(first)
	damaged[len(damaged)-8] ^= 0x01 // corrupt the CRC, retaining the trailer
	cases := []struct {
		name    string
		body    []byte
		want    string
		wantErr error
	}{
		{"complete", first, "first member\n", nil},
		{"complete concatenated members", append(bytes.Clone(first), second...), "first member\nsecond member\n", nil},
		{"truncated first member", first[:len(first)-5], "", io.ErrUnexpectedEOF},
		{"truncated second member", append(bytes.Clone(first), second[:len(second)-5]...), "", io.ErrUnexpectedEOF},
		{"invalid checksum", damaged, "", gzip.ErrChecksum},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				parent, cancel := context.WithCancel(context.Background())
				defer cancel()
				var probeContext context.Context
				calls := 0
				client := &http.Client{Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
					calls++
					if req.Header.Get("Range") != "" {
						probeContext = req.Context()
						<-req.Context().Done()
						return nil, req.Context().Err()
					}
					return &http.Response{StatusCode: http.StatusOK, ContentLength: -1, Request: req,
						Header: http.Header{"Etag": {`"stable-v1"`}}, Body: io.NopCloser(bytes.NewReader(tc.body))}, nil
				})}
				var received []byte
				_, err := Download(parent, client, "", "", nil, func(r io.Reader, _ DownloadResult) error {
					var readErr error
					received, readErr = io.ReadAll(r)
					return readErr
				})
				require.NoError(t, parent.Err(), "optional probe must never cancel the import context")
				if tc.wantErr != nil {
					require.ErrorIs(t, err, tc.wantErr)
				} else {
					require.NoError(t, err)
					require.Equal(t, tc.want, string(received))
				}
				if tc.wantErr != gzip.ErrChecksum {
					require.NotNil(t, probeContext, "fixture must exercise the optional EOF probe timeout")
					require.ErrorIs(t, probeContext.Err(), context.Canceled)
					require.Equal(t, 2, calls)
				}
			})
		})
	}
}

type probeContextBody struct {
	ctx context.Context
	io.Reader
	closed bool
}

func (b *probeContextBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	return b.Reader.Read(p)
}
func (b *probeContextBody) Close() error { b.closed = true; return nil }

func TestEOFProbe_AdoptedBodyOutlivesHeaderBudgetAndClosesContext(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		defer cancel()
		var child context.Context
		var body *probeContextBody
		var events []string
		r := &resumeReader{ctx: parent, url: "http://synthetic.invalid/dump", etag: `"stable-v1"`, total: -1, offset: 3, body: io.NopCloser(strings.NewReader(""))}
		r.options = DownloadOptions{
			OnEOFProbeStart: func() { events = append(events, "start") },
			OnEOFProbeDone:  func() { events = append(events, "done") },
		}
		r.client = &http.Client{Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
			events = append(events, "headers")
			child = req.Context()
			body = &probeContextBody{ctx: child, Reader: strings.NewReader("def")}
			return &http.Response{StatusCode: http.StatusPartialContent, Request: req, Header: http.Header{"Content-Range": {"bytes 3-5/6"}}, Body: body}, nil
		})}
		require.False(t, r.classifyUnknownEOF())
		require.Equal(t, []string{"start", "headers", "done"}, events,
			"probe callbacks must bracket only header waiting, not adopted body reads")
		time.Sleep(2 * eofProbeHeaderTimeout)
		require.NoError(t, child.Err(), "header timer must be stopped when a 206 body is adopted")
		got, err := io.ReadAll(r)
		require.NoError(t, err)
		require.Equal(t, "def", string(got))
		require.NoError(t, r.Close())
		require.True(t, body.closed)
		require.ErrorIs(t, child.Err(), context.Canceled)
		require.NoError(t, parent.Err())
	})
}

func TestEOFProbe_ParentCancellationStopsProbe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		parent, cancel := context.WithCancel(context.Background())
		defer cancel()
		r := &resumeReader{ctx: parent, url: "http://synthetic.invalid/dump", etag: `"stable-v1"`, total: -1, body: io.NopCloser(strings.NewReader(""))}
		r.client = &http.Client{Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
			go func() { time.Sleep(time.Second); cancel() }()
			<-req.Context().Done()
			return nil, req.Context().Err()
		})}
		started := time.Now()
		require.True(t, r.classifyUnknownEOF())
		require.ErrorIs(t, parent.Err(), context.Canceled)
		require.Equal(t, time.Second, time.Since(started))
	})
}

func TestEOFProbe_UnadoptedResponseClosesContext(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusRequestedRangeNotSatisfiable, http.StatusPartialContent} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			parent := context.Background()
			var child context.Context
			var body *probeContextBody
			r := &resumeReader{ctx: parent, url: "http://synthetic.invalid/dump", etag: `"stable-v1"`, total: -1, body: io.NopCloser(strings.NewReader(""))}
			r.client = &http.Client{Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
				child = req.Context()
				body = &probeContextBody{ctx: child, Reader: strings.NewReader("")}
				return &http.Response{StatusCode: status, Request: req, Header: make(http.Header), Body: body}, nil
			})}
			require.True(t, r.classifyUnknownEOF())
			require.True(t, body.closed)
			require.ErrorIs(t, child.Err(), context.Canceled)
		})
	}
}

func TestEOFProbe_LateResponseHeadersAreNotAdopted(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var child context.Context
		var body *probeContextBody
		original := &probeContextBody{ctx: context.Background(), Reader: strings.NewReader("")}
		r := &resumeReader{ctx: context.Background(), url: "http://synthetic.invalid/dump", etag: `"stable-v1"`, total: -1, offset: 3, body: original}
		r.client = &http.Client{Transport: rtFunc(func(req *http.Request) (*http.Response, error) {
			child = req.Context()
			time.Sleep(2 * eofProbeHeaderTimeout) // custom transport returns after cancellation
			body = &probeContextBody{ctx: child, Reader: strings.NewReader("def")}
			return &http.Response{StatusCode: http.StatusPartialContent, Request: req, Header: http.Header{"Content-Range": {"bytes 3-5/6"}}, Body: body}, nil
		})}
		require.True(t, r.classifyUnknownEOF())
		require.Same(t, original, r.body)
		require.EqualValues(t, -1, r.total)
		require.True(t, body.closed)
		require.ErrorIs(t, child.Err(), context.Canceled)
	})
}
