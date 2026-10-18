package dmm

import (
	"context"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// These pins keep the guard-only extraction independently covered before
// the marker query resolver is introduced in the dependent slice.
func TestFoundationIdentityGuardBranches(t *testing.T) {
	number, ok := remasterTailNumber("h_003rct00156h")
	require.True(t, ok)
	require.Equal(t, "00156", number)
	_, _, ok = separatorSeriesTail("???-156H")
	require.False(t, ok)
}

func TestFoundationSearchRejectsConflictingDisplay(t *testing.T) {
	s, repo := newRemasterTestScraper(t)
	require.NoError(t, repo.Create(context.Background(), &models.ContentIDMapping{
		SearchID: "RCT-156H", ContentID: "1rct00156h", Source: s.Name(),
	}))
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "/search/=") {
			return 200, `<a href="/mono/dvd/-/detail/=/cid=1rct00156h/">candidate</a>`
		}
		if strings.Contains(u, "cid=1rct00156h") {
			return 200, `<h1 id="title" class="item">Wrong release</h1><table><tr><td>品番：</td><td>RCT-157-HD</td></tr></table>`
		}
		return 404, ""
	}})
	result, err := s.Search(context.Background(), "RCT-156H")
	require.Error(t, err)
	require.Nil(t, result)
}
