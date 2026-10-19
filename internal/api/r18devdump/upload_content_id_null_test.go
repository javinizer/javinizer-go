package r18devdump

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpload_NullOrMissingContentIDPreservesWorkingDump(t *testing.T) {
	for _, tc := range []struct{ table, otherColumn, otherValue string }{
		{"derived_video", "dvd_id", "NULL-001"},
		{"source_dmm_trailer", "url", "https://example.invalid/trailer.mp4"},
		{"derived_video_actress", "actress_id", "actress1"},
		{"derived_video_category", "category_id", "category1"},
		{"derived_video_director", "director_id", "director1"},
	} {
		for _, shape := range []string{"null", "missing column"} {
			t.Run(tc.table+"/"+shape, func(t *testing.T) {
				h, path, srv := newUploadHandler(t)
				t.Cleanup(h.rt.Shutdown)
				buildTestDump(t, path, "old00001", "OLD-001")
				before, err := os.ReadFile(path)
				require.NoError(t, err)
				old, err := r18devdump.Open(path)
				require.NoError(t, err)
				t.Cleanup(func() { _ = old.Close() })
				h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(old)
				var completed atomic.Int32
				h.broadcastProgressFn = func(phase string, _, _ int64, _ string) {
					if phase == "done" {
						completed.Add(1)
					}
				}

				columns, values := tc.otherColumn+", content_id", tc.otherValue+"\t\\N"
				if shape == "missing column" {
					columns, values = tc.otherColumn, tc.otherValue
				}
				// A good video makes the old zero-row guard insufficient.
				dump := uploadOneRowDump + fmt.Sprintf("COPY public.%s (%s) FROM stdin;\n%s\n\\.\n", tc.table, columns, values)
				status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, dump), "synthetic.sql.gz"))
				require.Equal(t, http.StatusAccepted, status)
				awaitDone(t, h, 5*time.Second)
				require.True(t, h.rt.WaitBackgroundTasks(time.Second))
				lastErr, kind, running := handlerState(h)
				assert.Equal(t, kindValidation, kind)
				assert.Contains(t, lastErr, r18devdump.ErrDumpNonCanonicalID.Error())
				assert.Contains(t, lastErr, "content_id")
				assert.False(t, running)
				assert.Zero(t, completed.Load(), "invalid dumps must not report successful installation")
				after, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(t, sha256.Sum256(before), sha256.Sum256(after), "working dump bytes must remain unchanged")
				assert.Same(t, old, h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(old), "the swap must not detach the working handle")
				dvdID, err := old.LookupByContentID(context.Background(), "old00001")
				assert.NoError(t, err, "the working handle must remain usable")
				assert.Equal(t, "OLD-001", dvdID)
				for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
					assert.NoFileExists(t, path+suffix)
				}
				assertNoUploadStages(t, path)

				// Rejection must release admission and all staging resources so
				// a subsequent valid upload can install normally.
				status, _, _ = srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "retry.sql.gz"))
				require.Equal(t, http.StatusAccepted, status)
				awaitDone(t, h, 5*time.Second)
				require.True(t, h.rt.WaitBackgroundTasks(time.Second))
				lastErr, kind, running = handlerState(h)
				assert.Empty(t, lastErr)
				assert.Empty(t, kind)
				assert.False(t, running)
				assert.EqualValues(t, 1, completed.Load())
				fresh, err := r18devdump.Open(path)
				require.NoError(t, err)
				defer func() { _ = fresh.Close() }()
				dvdID, err = fresh.LookupByContentID(context.Background(), "118ipx00535")
				require.NoError(t, err)
				assert.Equal(t, "IPX-535", dvdID)
				assertNoUploadStages(t, path)
			})
		}
	}
}
