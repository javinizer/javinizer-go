package javdb

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPlainTextCastKeepsFirstOccurrence(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="cast">春子 <span>、夏子</span> / 春子, 夏子</div>`))
	require.NoError(t, err)
	cast := doc.Find("#cast")
	require.Equal(t, []models.ActressInfo{{JapaneseName: "春子"}, {JapaneseName: "夏子"}}, extractActresses(cast))
	require.Equal(t, "春子 、夏子 / 春子, 夏子", nodeText(cast.Get(0)))
	require.Empty(t, nodeText(nil))
}
