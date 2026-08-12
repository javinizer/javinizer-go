package actress

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMergeTimestampGap_OnlyTargetProvided(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	body := `{"target_id":"@TARGET@","source_id":"@SOURCE@","target_updated_at":"` + now + `"}`
	resp := postActressMerge(t, body)
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestMergeTimestampGap_OnlySourceProvided(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	body := `{"target_id":"@TARGET@","source_id":"@SOURCE@","source_updated_at":"` + now + `"}`
	resp := postActressMerge(t, body)
	assert.Equal(t, http.StatusBadRequest, resp.Code)
}
