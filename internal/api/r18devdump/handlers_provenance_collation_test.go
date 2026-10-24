package r18devdump

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func collatedUploadProvenance(t *testing.T, collation, key string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "collated.db")
	require.NoError(t, os.WriteFile(path, validSidecarFixture(t, "https://example.invalid/new.sql.gz"), 0600))
	db, err := sql.Open("sqlite3", path)
	require.NoError(t, err)
	defer db.Close()
	replacement := strings.ToUpper(key)
	if collation == "RTRIM" {
		replacement = key + " "
	}
	for _, stmt := range []string{
		"CREATE TABLE dump_meta_collated (key TEXT COLLATE " + collation + " PRIMARY KEY, value TEXT)",
		"INSERT INTO dump_meta_collated SELECT key, value FROM dump_meta",
		"DROP TABLE dump_meta",
		"ALTER TABLE dump_meta_collated RENAME TO dump_meta",
		fmt.Sprintf("UPDATE dump_meta SET key = '%s' WHERE key = '%s'", replacement, key),
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err)
	}
	require.NoError(t, db.Close())
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func TestUpload_SidecarNoncanonicalProvenancePreservesWorkingDump(t *testing.T) {
	for _, collation := range []string{"NOCASE", "RTRIM"} {
		for _, key := range []string{"source_url", "source_date", "imported_at"} {
			t.Run(collation+"/"+key, func(t *testing.T) {
				h, path, srv := newUploadHandler(t)
				before := validSidecarFixture(t, "https://example.invalid/working.sql.gz")
				require.NoError(t, os.WriteFile(path, before, 0600))
				beforeStatus := readDumpStatus(t, h)
				require.True(t, beforeStatus.Present)
				status, body, _ := srv.doUpload(t, buildUploadBody(t, collatedUploadProvenance(t, collation, key), "upload.db"))
				require.Equal(t, http.StatusAccepted, status, body)
				awaitFixtureDone(t, h)
				afterStatus := readDumpStatus(t, h)
				assert.False(t, afterStatus.Running)
				assert.True(t, afterStatus.Present)
				assert.Equal(t, kindValidation, afterStatus.LastErrorKind)
				assert.Contains(t, afterStatus.LastError, "missing required provenance keys")
				assert.Equal(t, beforeStatus.SourceURL, afterStatus.SourceURL)
				assert.Equal(t, beforeStatus.SourceDate, afterStatus.SourceDate)
				assert.Equal(t, beforeStatus.ImportedAt, afterStatus.ImportedAt)
				after, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.True(t, bytes.Equal(before, after), "rejected sidecar must not replace the working dump")
				leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".r18dev-dump-upload-*"))
				require.NoError(t, err)
				assert.Empty(t, leftovers)
				require.True(t, h.tryAcquireDumpOp(), "validation failure releases the operation guard")
				h.releaseDumpOp()
			})
		}
	}
}
