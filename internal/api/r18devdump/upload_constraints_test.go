package r18devdump

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpload_ConstraintViolationPreservesWorkingDump(t *testing.T) {
	// Flush valid video rows before each corrupt block; a partially populated
	// transaction must never reach the swap or close the working handle.
	var prefix strings.Builder
	prefix.WriteString(uploadOneRowDump)
	prefix.WriteString("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&prefix, "new%05d\tNEW-%05d\n", i, i)
	}
	prefix.WriteString("\\.\n")
	const optionalRows = "COPY public.derived_video_actress (content_id, actress_id, ordinality, release_date) FROM stdin;\n" +
		"118ipx00535\tID\t\\N\t\\N\n118ipx00535\tid\t\\N\t\\N\n\\.\n" +
		"COPY public.unstored (id) FROM stdin;\n\\N\nrepeated\nrepeated\n\\.\n"
	for _, tc := range []struct{ name, block, table string }{
		{"null entity id", "COPY public.derived_actress (id, name_romaji) FROM stdin;\n\\N\tSynthetic\n\\.\n", "derived_actress"},
		{"null association id", "COPY public.derived_video_actress (content_id, actress_id) FROM stdin;\n118ipx00535\t\\N\n\\.\n", "derived_video_actress"},
		{"missing association id", "COPY public.derived_video_category (content_id) FROM stdin;\n118ipx00535\n\\.\n", "derived_video_category"},
		{"duplicate video", "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n118ipx00535\tOTHER-001\n\\.\n", "derived_video"},
		{"duplicate entity", "COPY public.derived_maker (id, name_en) FROM stdin;\nid1\tFirst\nid1\tSecond\n\\.\n", "derived_maker"},
		{"duplicate association", "COPY public.derived_video_actress (content_id, actress_id, ordinality) FROM stdin;\n118ipx00535\tid1\t1\n118ipx00535\tid1\t2\n\\.\n", "derived_video_actress"},
		{"identical trailer", "COPY public.source_dmm_trailer (content_id, url) FROM stdin;\n118ipx00535\t\\N\n118ipx00535\t\\N\n\\.\n", "source_dmm_trailer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
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
			status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, prefix.String()+tc.block), "synthetic.sql.gz"))
			require.Equal(t, http.StatusAccepted, status)
			awaitDone(t, h, 5*time.Second)
			require.True(t, h.rt.WaitBackgroundTasks(time.Second))
			lastErr, kind, running := handlerState(h)
			assert.Equal(t, kindValidation, kind)
			assert.Contains(t, lastErr, r18devdump.ErrDumpInvalid.Error())
			assert.Contains(t, lastErr, tc.table)
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

			status, _, _ = srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump+optionalRows), "retry.sql.gz"))
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
