package actress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/require"
)

func TestImportUserAliasReplacementParity(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		for _, aliases := range []string{"New Alias", "", "omitted", "Old Alias"} {
			for _, name := range []string{"Manual", "Changed"} {
				t.Run(fmt.Sprintf("overwrite=%t/aliases=%s/name=%s", overwrite, aliases, name), func(t *testing.T) {
					repo := newMockActressRepo(t)
					router := gin.New()
					deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
					router.POST("/import", importActresses(deps))
					router.PUT("/actresses/:id", updateActress(deps))
					call := func(method, path, body string) string {
						q := httptest.NewRequest(method, path, bytes.NewBufferString(body))
						q.Header.Set("Content-Type", "application/json")
						w := httptest.NewRecorder()
						router.ServeHTTP(w, q)
						require.Equal(t, 200, w.Code, w.Body.String())
						return w.Body.String()
					}
					call("POST", "/import", `{"actresses":[{"dmm_id":9234,"first_name":"Imported","aliases":"Old Alias"}]}`)
					a, err := repo.FindByDMMID(t.Context(), 9234)
					require.NoError(t, err)
					call("PUT", fmt.Sprintf("/actresses/%d", a.ID), `{"dmm_id":9234,"first_name":"Manual","aliases":"Old Alias"}`)
					item := map[string]any{"dmm_id": 9234, "first_name": name}
					wantAlias := aliases
					if aliases != "omitted" {
						item["aliases"] = aliases
					} else {
						wantAlias = ""
					}
					body, err := json.Marshal(map[string]any{"overwrite_names": overwrite, "actresses": []any{item}})
					require.NoError(t, err)
					out := call("POST", "/import", string(body))
					a, err = repo.FindByID(t.Context(), a.ID)
					require.NoError(t, err)
					require.Equal(t, "Manual", a.FirstName)
					require.Equal(t, database.ActressOriginUser, a.Origin)
					require.Equal(t, wantAlias, a.Aliases)
					var summary importSummaryResponse
					require.NoError(t, json.Unmarshal([]byte(out), &summary))
					require.Zero(t, summary.Errors)
					if wantAlias != "Old Alias" {
						require.Equal(t, 1, summary.Updated)
						require.Zero(t, summary.Protected)
					} else if name != "Manual" {
						require.Equal(t, 1, summary.Protected)
					} else {
						require.Equal(t, 1, summary.Unchanged)
					}
				})
			}
		}
	}
}
