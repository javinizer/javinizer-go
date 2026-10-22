package actress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestMergePolicyHTTPPreviewAndApply(t *testing.T) {
	for _, policy := range []string{"", "fill_empty", "preserve_target"} {
		t.Run(policy, func(t *testing.T) {
			repo := newMockActressRepo(t)
			target := models.Actress{FirstName: "桜"}
			source := models.Actress{FirstName: "Source", LastName: "Example", DMMID: 781}
			require.NoError(t, repo.Create(t.Context(), &target))
			require.NoError(t, repo.Create(t.Context(), &source))
			router := gin.New()
			deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
			router.POST("/preview", previewActressMerge(deps))
			router.POST("/merge", mergeActresses(deps))
			body := fmt.Sprintf(`{"target_id":%d,"source_id":%d,"policy":%q,"resolutions":{"first_name":"source"}}`, target.ID, source.ID, policy)
			call := func(path string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				router.ServeHTTP(w, req)
				return w
			}
			w := call("/preview")
			require.Equal(t, 200, w.Code, w.Body.String())
			var preview contracts.ActressMergePreviewResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
			require.Len(t, preview.Fields, 5)
			require.Equal(t, "Source", preview.ProposedMerged.FirstName)
			require.Contains(t, w.Body.String(), `"target_value":""`)
			if policy == "preserve_target" {
				require.Empty(t, preview.ProposedMerged.LastName)
				require.Zero(t, preview.ProposedMerged.DMMID)
			} else {
				require.Equal(t, "Example", preview.ProposedMerged.LastName)
				require.Equal(t, 781, preview.ProposedMerged.DMMID)
			}
			w = call("/merge")
			require.Equal(t, 200, w.Code, w.Body.String())
			var result contracts.ActressMergeResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
			require.Equal(t, preview.ProposedMerged.LastName, result.MergedActress.LastName)
			require.Equal(t, preview.ProposedMerged.DMMID, result.MergedActress.DMMID)
		})
	}
}

func TestMergePolicyHTTPRejectsInvalidOptions(t *testing.T) {
	router := gin.New()
	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: newMockActressRepo(t)}}
	router.POST("/preview", previewActressMerge(deps))
	router.POST("/merge", mergeActresses(deps))
	for _, path := range []string{"/preview", "/merge"} {
		for _, option := range []string{`"policy":"invalid"`, `"resolutions":{"aliases":"source"}`, `"resolutions":{"last_name":"invalid"}`} {
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{"target_id":1,"source_id":2,`+option+`}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, 400, w.Code, w.Body.String())
		}
	}
}
