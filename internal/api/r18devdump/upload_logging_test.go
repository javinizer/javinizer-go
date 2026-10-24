package r18devdump

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureUploadLogs(t *testing.T) *logtest.Hook {
	t.Helper()
	logger := logging.WithField("test", "upload").Logger
	previous := logger.ReplaceHooks(make(logrus.LevelHooks))
	t.Cleanup(func() { logger.ReplaceHooks(previous) })
	return logtest.NewLocal(logger)
}

func TestUpload_LogsAsyncFailureOnceWithoutPayload(t *testing.T) {
	for _, tc := range []struct {
		name, dump, wantBatch string
	}{
		{"duplicate", "COPY public.derived_video (content_id, dvd_id, title_en) FROM stdin;\n" +
			"privatekey\tPRIVATE-001\tprivate first title\nprivatekey\tPRIVATE-002\tprivate second title\n\\.\n",
			"1 duplicate-key rows, 0 NULL-key rows"},
		{"typed value", "COPY public.derived_video (content_id, runtime_mins) FROM stdin;\nprivatekey\tprivate typed value\n\\.\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hook := captureUploadLogs(t)
			h, path, srv := newUploadHandler(t)
			t.Cleanup(h.rt.Shutdown)
			buildTestDump(t, path, "old00001", "OLD-001")
			before, err := os.ReadFile(path)
			require.NoError(t, err)
			status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, tc.dump), "private-upload.sql.gz"))
			require.Equal(t, http.StatusAccepted, status)
			awaitFixtureDone(t, h)
			lastErr, kind, running := handlerState(h)
			assert.NotEmpty(t, lastErr)
			assert.Equal(t, kindValidation, kind)
			assert.False(t, running)
			var summaries []*logrus.Entry
			for _, entry := range hook.AllEntries() {
				if strings.HasPrefix(entry.Message, "r18dev dump upload failed") {
					summaries = append(summaries, entry)
				}
			}
			require.Len(t, summaries, 1)
			entry := summaries[0]
			assert.Equal(t, logrus.WarnLevel, entry.Level)
			assert.Equal(t, kindValidation, entry.Data["error_kind"])
			if tc.wantBatch != "" {
				assert.Contains(t, entry.Data["batch_error"], tc.wantBatch)
				assert.Contains(t, entry.Data["batch_error"], "key columns: content_id")
			} else {
				assert.NotContains(t, entry.Data, "batch_error")
				assert.Contains(t, entry.Message, "see dump status")
				assert.Contains(t, lastErr, "private typed value", "existing status detail remains available")
			}
			logged := fmt.Sprint(entry.Data) + entry.Message
			assert.NotContains(t, logged, "private")
			assert.NotContains(t, logged, "PRIVATE")
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, before, after)
			assertNoUploadStages(t, path)
		})
	}
}

func TestUpload_SuccessDoesNotLogFailure(t *testing.T) {
	hook := captureUploadLogs(t)
	h, _, srv := newUploadHandler(t)
	t.Cleanup(h.rt.Shutdown)
	status, _, _ := srv.doUpload(t, buildUploadBody(t, gzBytes(t, uploadOneRowDump), "synthetic.sql.gz"))
	require.Equal(t, http.StatusAccepted, status)
	awaitFixtureDone(t, h)
	lastErr, kind, running := handlerState(h)
	require.Empty(t, lastErr)
	assert.Empty(t, kind)
	assert.False(t, running)
	for _, entry := range hook.AllEntries() {
		assert.NotContains(t, entry.Message, "r18dev dump upload failed")
	}
}
