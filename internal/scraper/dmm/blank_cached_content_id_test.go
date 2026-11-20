package dmm

import (
	"context"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestGetURLResolvesBlankCachedContentID(t *testing.T) {
	const (
		searchID  = "TST-123"
		contentID = "tst00123"
		related   = "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=tst00123/"
		unrelated = "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=alt00042/"
	)
	for _, tc := range []struct {
		name     string
		value    string
		hasMatch bool
	}{
		{name: "empty mapping", hasMatch: true},
		{name: "whitespace mapping", value: " \t ", hasMatch: true},
		{name: "no matching listing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := newDMMTestRepo(t)
			require.NoError(t, repo.Create(ctx, &models.ContentIDMapping{
				SearchID: searchID, ContentID: tc.value, Source: "dmm",
			}))
			s := newDMMSearchContainmentScraper(t, repo)
			// A different product of the same URL priority comes first.
			body := `<a href="` + unrelated + `">Another product</a>`
			if tc.hasMatch {
				body += `<a href="` + related + `">Requested product</a>`
			}
			s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
				require.Contains(t, []string{searchURLFor("tst123"), searchURLFor("tst-123"), searchURLFor(contentID)}, u)
				return http.StatusOK, body
			}})

			got, err := s.GetURL(ctx, searchID)
			mapping, lookupErr := repo.FindBySearchID(ctx, searchID)
			require.NoError(t, lookupErr)
			if !tc.hasMatch {
				require.ErrorContains(t, err, "no matching content-id")
				require.Empty(t, got)
				require.Equal(t, tc.value, mapping.ContentID, "a failed lookup must not cache the unrelated ID")
				return
			}
			require.NoError(t, err)
			require.Equal(t, related, got)
			require.Equal(t, contentID, mapping.ContentID)
		})
	}
}
