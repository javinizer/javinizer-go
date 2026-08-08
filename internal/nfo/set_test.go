package nfo

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetString_MarshalTieredFormat(t *testing.T) {
	movie := Movie{
		Title: "Test",
		Set:   SetString("Beautiful Days Series"),
	}
	xmlData, err := xml.MarshalIndent(movie, "", "  ")
	require.NoError(t, err)
	xmlStr := string(xmlData)

	assert.Contains(t, xmlStr, "<set>")
	assert.Contains(t, xmlStr, "<name>Beautiful Days Series</name>")
	assert.NotContains(t, xmlStr, "<set>Beautiful Days Series</set>")
}

func TestSetString_MarshalEmpty(t *testing.T) {
	movie := Movie{Title: "Test", Set: SetString("")}
	xmlData, err := xml.MarshalIndent(movie, "", "  ")
	require.NoError(t, err)
	assert.NotContains(t, string(xmlData), "<set>")
}

func TestSetString_UnmarshalTieredFormat(t *testing.T) {
	xmlStr := `<movie><title>Test</title><set><name>Beautiful Days Series</name></set></movie>`
	var movie Movie
	err := xml.Unmarshal([]byte(xmlStr), &movie)
	require.NoError(t, err)
	assert.Equal(t, "Beautiful Days Series", string(movie.Set))
}

func TestSetString_UnmarshalLegacyFlatFormat(t *testing.T) {
	xmlStr := `<movie><title>Test</title><set>Beautiful Days Series</set></movie>`
	var movie Movie
	err := xml.Unmarshal([]byte(xmlStr), &movie)
	require.NoError(t, err)
	assert.Equal(t, "Beautiful Days Series", string(movie.Set))
}

func TestSetString_UnmarshalTieredWithWhitespace(t *testing.T) {
	xmlStr := `<movie><set>
  <name>  Trimmed Series  </name>
</set></movie>`
	var movie Movie
	err := xml.Unmarshal([]byte(xmlStr), &movie)
	require.NoError(t, err)
	assert.Equal(t, "Trimmed Series", string(movie.Set))
}

func TestSetString_UnmarshalEmptySet(t *testing.T) {
	xmlStr := `<movie><title>Test</title><set></set></movie>`
	var movie Movie
	err := xml.Unmarshal([]byte(xmlStr), &movie)
	require.NoError(t, err)
	assert.Empty(t, movie.Set)
}

func TestSetString_UnmarshalSkipsUnknownChildren(t *testing.T) {
	xmlStr := `<movie><set><overview>Extra</overview><name>Real Series</name></set></movie>`
	var movie Movie
	err := xml.Unmarshal([]byte(xmlStr), &movie)
	require.NoError(t, err)
	assert.Equal(t, "Real Series", string(movie.Set))
}

func TestSetString_RoundTrip(t *testing.T) {
	original := Movie{Title: "Round Trip", Set: SetString("Collection Name")}
	xmlData, err := xml.MarshalIndent(original, "", "  ")
	require.NoError(t, err)

	var parsed Movie
	require.NoError(t, xml.Unmarshal(xmlData, &parsed))
	assert.Equal(t, original.Set, parsed.Set)
}

func TestSetString_LegacyRoundTrip(t *testing.T) {
	legacy := `<movie><title>Legacy</title><set>Legacy Series</set></movie>`
	var parsed Movie
	require.NoError(t, xml.Unmarshal([]byte(legacy), &parsed))
	assert.Equal(t, "Legacy Series", string(parsed.Set))

	remarshaled, err := xml.MarshalIndent(parsed, "", "  ")
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(remarshaled), "<name>Legacy Series</name>"),
		"remarshaled NFO should use the tiered format even when parsed from legacy flat form")
}
