package r18devdump

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedNullDVDRow adds a null-dvd_id row (content_id only) to a handler test dump.
func seedNullDVDRow(t *testing.T, dumpPath, contentID, releaseDate, serviceCode string) {
	t.Helper()
	db, err := openRawDB(dumpPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.Exec(`INSERT INTO videos (content_id, dvd_id, dvd_id_norm, release_date, service_code) VALUES (?, NULL, '', ?, ?)`,
		contentID, releaseDate, serviceCode)
	require.NoError(t, err)
}

func searchJSON(t *testing.T, h *dumpHandler, query string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/search?q="+query, nil)
	h.search(c)
	var parsed map[string]any
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &parsed))
	}
	return w, parsed
}

func TestSearch_PresentNoDVDID_Returns200WithState(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118ipx00535", "IPX-535")
	seedNullDVDRow(t, dumpPath, "lulu00441", "2026-07-03", "digital")
	seedNullDVDRow(t, dumpPath, "lulu441", "2026-07-07", "mono")

	w, resp := searchJSON(t, h, "LULU-441")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "no_dvd_id", resp["state"])
	require.Nil(t, resp["content_id"], "no mapping for null-dvd_id rows")
	matches, ok := resp["matches"].([]any)
	require.True(t, ok, "matches must be an array")
	require.Len(t, matches, 2)
	first := matches[0].(map[string]any)
	assert.Equal(t, "lulu00441", first["content_id"])
	assert.Equal(t, "2026-07-03", first["release_date"])
	assert.Equal(t, "digital", first["service_code"])
	second := matches[1].(map[string]any)
	assert.Equal(t, "lulu441", second["content_id"])
}

func TestSearch_PresentNoDVDID_DirectContentIDQuery(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118ipx00535", "IPX-535")
	seedNullDVDRow(t, dumpPath, "lulu00441", "2026-07-03", "digital")

	w, resp := searchJSON(t, h, "lulu00441")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "no_dvd_id", resp["state"])
}

func TestSearch_MappedSetsStateAndMatches(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")

	w, resp := searchJSON(t, h, "ABF-030")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "mapped", resp["state"])
	assert.Equal(t, "118abf030", resp["content_id"])
	matches, ok := resp["matches"].([]any)
	require.True(t, ok)
	require.Len(t, matches, 1)
	assert.Equal(t, "ABF-030", matches[0].(map[string]any)["dvd_id"])
}

func TestSearch_WhitespacePaddedContentIDQueryKeepsDirection(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118abf030", "ABF-030")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/r18dev/dump/search?q=%20118abf030%20", nil)
	h.search(c)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "mapped", resp["state"])
	assert.Equal(t, "ABF-030", resp["dvd_id"], "padded exact content-id query reports the dvd_id direction")
	assert.Nil(t, resp["content_id"])
}

func TestSearch_AbsentStill404(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	buildTestDump(t, dumpPath, "118ipx00535", "IPX-535")

	w, _ := searchJSON(t, h, "NOPE-999")
	require.Equal(t, http.StatusNotFound, w.Code)
}
