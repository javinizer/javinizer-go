package r18devdump

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const casingDump = "COPY public.derived_video (content_id, dvd_id) FROM stdin;\n1223bbspgS01_m\tBBSPGS-01\n\\.\n"

func assertCasingInstalled(t *testing.T, h *dumpHandler, path string) {
	t.Helper()
	awaitFixtureDone(t, h)
	lastErr, kind, _ := handlerState(h)
	assert.Empty(t, lastErr)
	assert.Empty(t, kind)
	store, err := r18devdump.Open(path)
	require.NoError(t, err)
	defer func() { _ = store.Close() }()
	cid, err := store.LookupByDVDID(context.Background(), "BBSPGS-01")
	require.NoError(t, err)
	assert.Equal(t, "1223bbspgS01_m", cid)
	for _, query := range []string{"1223bbspgS01_m", "1223bbspgs01_m"} {
		dvd, err := store.LookupByContentID(context.Background(), query)
		require.NoError(t, err)
		assert.Equal(t, "BBSPGS-01", dvd)
	}
}

func TestDumpCasing_DownloadAndUploads(t *testing.T) {
	t.Run("download", func(t *testing.T) {
		data := gzipped(casingDump)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(data)
		}))
		defer server.Close()
		h, path := newTestHandler(t)
		h.httpClient = server.Client()
		h.reloadFn = func(*config.Config, bool) error { return nil }
		original := r18devdump.LatestDumpURL
		r18devdump.LatestDumpURL = server.URL + "/r18dotdev_dump_2026-10-20.sql.gz"
		defer func() { r18devdump.LatestDumpURL = original }()
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/r18dev/dump/download", nil)
		h.startDownload(ctx)
		require.Equal(t, http.StatusAccepted, recorder.Code)
		assertCasingInstalled(t, h, path)
	})
	for _, sidecar := range []bool{false, true} {
		name := "raw_upload"
		if sidecar {
			name = "copied_sidecar"
		}
		t.Run(name, func(t *testing.T) {
			h, path, server := newUploadHandler(t)
			data := gzBytes(t, casingDump)
			filename := "r18dotdev_dump_2026-10-20.sql.gz"
			if sidecar {
				source := filepath.Join(t.TempDir(), "source.db")
				_, err := r18devdump.Import(context.Background(), strings.NewReader(casingDump), source, r18devdump.ImportOptions{SourceURL: "https://example.invalid/source.sql.gz"})
				require.NoError(t, err)
				data, err = os.ReadFile(source)
				require.NoError(t, err)
				filename = "copied.db"
			}
			status, _, _ := server.doUpload(t, buildUploadBody(t, data, filename))
			require.Equal(t, http.StatusAccepted, status)
			assertCasingInstalled(t, h, path)
		})
	}
}
