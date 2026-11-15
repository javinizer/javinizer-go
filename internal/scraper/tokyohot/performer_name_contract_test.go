package tokyohot

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPerformerNameTokensPreserveSiteSeparators(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<dl class="info"><dt>Model</dt><dd>
 &#x3000;Alice&nbsp;Bob&#x2009;Carol / 花子、Alice | &#x200b; ／ Dana
 </dd></dl>`))
	require.NoError(t, err)
	require.Equal(t, []models.ActressInfo{
		{FirstName: "Alice"}, {FirstName: "Bob"}, {FirstName: "Carol"},
		{JapaneseName: "花子"}, {FirstName: "\u200b"}, {FirstName: "Dana"},
	}, extractActresses(doc))
}
