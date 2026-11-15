package jav321

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPerformerNamesNormalizeWhitespaceWithoutEmptyEntries(t *testing.T) {
	got := extractActresses(`<b>出演者</b>: <a href="/star/1">&#x3000;&nbsp;&#9;</a><a href="/star/1"> Solo </a><a href="/star/1"> Ada&nbsp;Van&#x2009;Example </a><a href="/star/1">花子</a><a href="/star/1">Solo</a><a href="/star/1">&#x200b;</a><br>`)
	require.Equal(t, []models.ActressInfo{
		{FirstName: "Solo"},
		{FirstName: "Ada", LastName: "Van Example"},
		{JapaneseName: "花子"},
		{FirstName: "\u200b"},
	}, got)
}
