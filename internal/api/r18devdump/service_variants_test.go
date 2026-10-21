package r18devdump

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The upstream key is (content_id, service_code), not content_id alone.
// Reproduce the reported 40-row batch: 33 distinct CIDs plus seven rental
// variants. Different DVD IDs and titles ensure no lookup may accidentally
// select one service's metadata over the other.
func serviceVariantAPIDump() string {
	var dump strings.Builder
	dump.WriteString("COPY public.derived_video (content_id, dvd_id, service_code, title_en) FROM stdin;\n")
	for i := 1; i <= 33; i++ {
		fmt.Fprintf(&dump, "118ipx%05d\tIPX-%03d\tdigital\tDigital %d\n", i, i, i)
	}
	for i := 1; i <= 7; i++ {
		fmt.Fprintf(&dump, "118ipx%05d\tRENTAL-%03d\trental\tRental %d\n", i, i, i)
	}
	dump.WriteString("\\.\n")
	return dump.String()
}

func serviceVariantAPIStatus(t *testing.T, h *dumpHandler) dumpStatusResponse {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/status", nil)
	h.getStatus(c)
	require.Equal(t, http.StatusOK, w.Code)
	var status dumpStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &status))
	return status
}

func assertServiceVariantsInstalled(t *testing.T, h *dumpHandler, path, source string) {
	t.Helper()
	awaitDone(t, h, 10*time.Second)
	require.True(t, h.rt.WaitBackgroundTasks(time.Second))
	status := serviceVariantAPIStatus(t, h)
	assert.False(t, status.Running)
	assert.Empty(t, status.LastError)
	assert.Empty(t, status.LastErrorKind)
	require.True(t, status.Present)
	assert.EqualValues(t, 40, status.RowCount)
	assert.Equal(t, source, status.SourceURL)
	assert.Equal(t, "2026-10-20", status.SourceDate)

	db, err := sql.Open("sqlite3", path+"?mode=ro")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	rows, err := db.Query("SELECT content_id, dvd_id, service_code, title_en FROM videos ORDER BY content_id, service_code")
	require.NoError(t, err)
	var count int
	for rows.Next() {
		var cid, dvdID, service, title string
		require.NoError(t, rows.Scan(&cid, &dvdID, &service, &title))
		i := count/2 + 1
		if count >= 14 {
			i = count - 6
		}
		assert.Equal(t, fmt.Sprintf("118ipx%05d", i), cid)
		if count < 14 && count%2 == 1 {
			assert.Equal(t, "rental", service)
			assert.Equal(t, fmt.Sprintf("RENTAL-%03d", i), dvdID)
			assert.Equal(t, fmt.Sprintf("Rental %d", i), title)
		} else {
			assert.Equal(t, "digital", service)
			assert.Equal(t, fmt.Sprintf("IPX-%03d", i), dvdID)
			assert.Equal(t, fmt.Sprintf("Digital %d", i), title)
		}
		count++
	}
	require.NoError(t, rows.Err())
	require.NoError(t, rows.Close())
	assert.Equal(t, 40, count, "every service variant must survive installation")

	store, err := r18devdump.Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	ctx := context.Background()
	for i := 1; i <= 7; i++ {
		cid := fmt.Sprintf("118ipx%05d", i)
		dvdID, err := store.LookupByContentID(ctx, cid)
		assert.ErrorIs(t, err, models.ErrDumpMiss)
		assert.Empty(t, dvdID)
		for _, id := range []string{cid, fmt.Sprintf("IPX-%03d", i), fmt.Sprintf("RENTAL-%03d", i)} {
			matches, err := store.MatchByDisplayID(ctx, id)
			assert.ErrorIs(t, err, models.ErrDumpMiss, "ambiguous service variants for %s must remain a miss", id)
			assert.Empty(t, matches)
			movie, err := store.LookupMovie(ctx, id)
			assert.ErrorIs(t, err, models.ErrDumpMiss)
			assert.Nil(t, movie, "must not expose arbitrary service metadata for %s", id)
		}
		for _, id := range []string{fmt.Sprintf("IPX-%03d", i), fmt.Sprintf("RENTAL-%03d", i)} {
			resolved, err := store.LookupByDVDID(ctx, id)
			assert.ErrorIs(t, err, models.ErrDumpMiss)
			assert.Empty(t, resolved)
		}
	}
	dvdID, err := store.LookupByContentID(ctx, "118ipx00033")
	require.NoError(t, err)
	assert.Equal(t, "IPX-033", dvdID, "unambiguous rows must remain usable")
}

func TestUpload_ServiceVariantsPreserved(t *testing.T) {
	h, path, srv := newUploadHandler(t)
	t.Cleanup(h.rt.Shutdown)
	var completed atomic.Int32
	h.broadcastProgressFn = func(phase string, _, _ int64, _ string) {
		if phase == "done" {
			completed.Add(1)
		}
	}
	const filename = "r18dotdev_dump_2026-10-20.sql.gz"
	status, body, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, serviceVariantAPIDump()), filename))
	require.Equal(t, http.StatusAccepted, status, body)
	assertServiceVariantsInstalled(t, h, path, filename)
	assert.EqualValues(t, 1, completed.Load())
	assertNoUploadStages(t, path)
}

func TestUpload_DuplicateServiceTuplePreservesWorkingDump(t *testing.T) {
	for _, tc := range []struct{ name, block string }{
		{"digital", "COPY public.derived_video (content_id, dvd_id, service_code) FROM stdin;\n118ipx00001\tOTHER-001\tdigital\n\\.\n"},
		{"null legacy service", "COPY public.derived_video (content_id, dvd_id, service_code) FROM stdin;\nlegacy00001\tLEGACY-001\t\\N\nlegacy00001\tOTHER-001\t\\N\n\\.\n"},
		{"omitted legacy service", "COPY public.derived_video (content_id, dvd_id) FROM stdin;\nlegacy00001\tLEGACY-001\nlegacy00001\tOTHER-001\n\\.\n"},
		{"empty service", "COPY public.derived_video (content_id, dvd_id, service_code) FROM stdin;\nlegacy00001\tLEGACY-001\t\nlegacy00001\tOTHER-001\t\n\\.\n"},
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
			status, body, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, serviceVariantAPIDump()+tc.block), "invalid.sql.gz"))
			require.Equal(t, http.StatusAccepted, status, body)
			awaitDone(t, h, 10*time.Second)
			require.True(t, h.rt.WaitBackgroundTasks(time.Second))
			outcome := serviceVariantAPIStatus(t, h)
			assert.False(t, outcome.Running)
			assert.True(t, outcome.Present)
			assert.Equal(t, kindValidation, outcome.LastErrorKind)
			assert.Contains(t, outcome.LastError, r18devdump.ErrDumpInvalid.Error())
			assert.Contains(t, outcome.LastError, "derived_video")
			assert.Zero(t, completed.Load())
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, before, after, "invalid composite duplicate must preserve working sidecar byte for byte")
			assert.Same(t, old, h.rt.Deps().CoreDeps.ReplaceR18DevDumpCloser(old))
			dvdID, err := old.LookupByContentID(context.Background(), "old00001")
			require.NoError(t, err)
			assert.Equal(t, "OLD-001", dvdID)
			for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
				assert.NoFileExists(t, path+suffix)
			}
			assertNoUploadStages(t, path)
		})
	}
}

func TestStartDownload_ServiceVariantsPreserved(t *testing.T) {
	h, path, _ := newUploadHandler(t)
	t.Cleanup(h.rt.Shutdown)
	compressed := gzBytes(t, serviceVariantAPIDump())
	const dumpURLPath = "/dumps/r18dotdev_dump_2026-10-20.sql.gz"
	mux := http.NewServeMux()
	mux.HandleFunc("/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dumpURLPath, http.StatusFound)
	})
	mux.HandleFunc(dumpURLPath, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(compressed)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	h.httpClient = srv.Client()
	originalURL := r18devdump.LatestDumpURL
	r18devdump.LatestDumpURL = srv.URL + "/latest"
	t.Cleanup(func() { r18devdump.LatestDumpURL = originalURL })
	var completed atomic.Int32
	h.broadcastProgressFn = func(phase string, _, _ int64, _ string) {
		if phase == "done" {
			completed.Add(1)
		}
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)
	h.startDownload(c)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	assertServiceVariantsInstalled(t, h, path, srv.URL+dumpURLPath)
	assert.EqualValues(t, 1, completed.Load())
}
