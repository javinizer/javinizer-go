package javbus

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPerformerNamesNormalizeWhitespaceWithoutEmptyEntries(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div id="info"><a href="/star/1">&#x3000;&nbsp;&#9;</a><a href="/star/1"> Solo </a><a href="/star/1"> Ada&nbsp;Van&#x2009;Example </a><a href="/star/1">花子</a><a href="/star/1">Solo</a><a href="/star/1">&#x200b;</a></div>`))
	require.NoError(t, err)
	got := extractActresses(doc)
	require.Equal(t, []models.ActressInfo{
		{FirstName: "Solo"},
		{FirstName: "Ada", LastName: "Van Example"},
		{JapaneseName: "花子"},
		{FirstName: "\u200b"},
	}, got)
}
