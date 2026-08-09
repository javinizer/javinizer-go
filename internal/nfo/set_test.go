package nfo

import (
	"encoding/xml"
	"strings"
	"testing"

	"bytes"
	"github.com/spf13/afero"
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

func TestSetString_UnmarshalSelfClosingEmptySet(t *testing.T) {
	xmlStr := `<movie><title>Test</title><set/></movie>`
	var movie Movie
	err := xml.Unmarshal([]byte(xmlStr), &movie)
	require.NoError(t, err)
	assert.Empty(t, movie.Set)
}

func TestSetString_UnmarshalUnknownChildrenBeforeName(t *testing.T) {
	xmlStr := `<movie><set><overview>Extra</overview><name>Real Series</name></set></movie>`
	var movie Movie
	err := xml.Unmarshal([]byte(xmlStr), &movie)
	require.NoError(t, err)
	assert.Equal(t, "Real Series", string(movie.Set))
}

func TestSetString_UnmarshalUnknownChildrenAfterName(t *testing.T) {
	xmlStr := `<movie><set><name>Real Series</name><overview>Extra</overview></set></movie>`
	var movie Movie
	err := xml.Unmarshal([]byte(xmlStr), &movie)
	require.NoError(t, err)
	assert.Equal(t, "Real Series", string(movie.Set))
}

func TestSetString_UnmarshalNameWinsOverTrailingChardata(t *testing.T) {
	xmlStr := `<movie><set><name>Real Series</name>trailing text</set></movie>`
	var movie Movie
	err := xml.Unmarshal([]byte(xmlStr), &movie)
	require.NoError(t, err)
	assert.Equal(t, "Real Series", string(movie.Set))
}

func TestSetString_UnmarshalNameWinsOverLeadingChardata(t *testing.T) {
	xmlStr := `<movie><set>leading text<name>Real Series</name></set></movie>`
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

func TestSetString_UnmarshalAccumulatesSplitChardata(t *testing.T) {
	// A legacy flat value split across multiple CharData tokens (here by an
	// empty unknown child) is accumulated, not truncated to the first token.
	xmlStr := `<movie><set>abc<overview/>def</set></movie>`
	var movie Movie
	err := xml.Unmarshal([]byte(xmlStr), &movie)
	require.NoError(t, err)
	assert.Equal(t, "abcdef", string(movie.Set),
		"split flat chardata should be concatenated, matching plain string unmarshaling")
}

func TestSetString_UnmarshalEmptyNameWinsOverFlat(t *testing.T) {
	xmlStr := `<movie><set>Flat Collection<name></name></set></movie>`
	var movie Movie
	err := xml.Unmarshal([]byte(xmlStr), &movie)
	require.NoError(t, err)
	assert.Empty(t, movie.Set,
		"empty <name> child value wins per spec (name wins regardless of order); degenerate input")
}

func TestSetString_WriteNFOEscaping(t *testing.T) {
	fs := afero.NewMemMapFs()
	gen := NewGenerator(fs, &Config{})
	nfo := &Movie{
		Title: "Test",
		Set:   SetString(`Bob's "Best" <Series> & Co`),
	}

	err := gen.WriteNFO(nfo, "/test.nfo")
	require.NoError(t, err)

	data, err := afero.ReadFile(fs, "/test.nfo")
	require.NoError(t, err)
	output := string(data)

	assert.Contains(t, output, `<name>Bob's "Best" &lt;Series&gt; &amp; Co</name>`,
		"set name in <name> chardata: quotes/apostrophes literal (unescaped by unescapeQuotesInText), angle brackets/ampersands escaped")
	assert.NotContains(t, output, `&#34;`, "double quotes in <name> must not be numeric-escaped")
	assert.NotContains(t, output, `&#39;`, "apostrophes in <name> must not be numeric-escaped")

	parsed, perr := ParseNFO(fs, "/test.nfo")
	require.NoError(t, perr)
	require.NotNil(t, parsed.Movie)
	assert.Equal(t, `Bob's "Best" <Series> & Co`, parsed.Movie.Series,
		"set name with special characters must round-trip exactly through WriteNFO+ParseNFO")
}

func TestSetString_MarshalXMLEmptyDirectCall(t *testing.T) {
	err := SetString("").MarshalXML(xml.NewEncoder(failingWriter{}), xml.StartElement{Name: xml.Name{Local: "set"}})
	require.NoError(t, err, "empty SetString returns nil before touching the encoder")
}

func TestSetString_MarshalXMLEncoderError(t *testing.T) {
	var s SetString = "Series"
	// failAfterNWriter{remaining: 0} fails on the first write, so EncodeToken(start)
	// (which flushes the start element) surfaces its error immediately.
	err := s.MarshalXML(xml.NewEncoder(&failAfterNWriter{remaining: 0}), xml.StartElement{Name: xml.Name{Local: "set"}})
	require.Error(t, err, "MarshalXML should surface the EncodeToken(start) write error")
}

func TestSetString_UnmarshalXMLTokenError(t *testing.T) {
	var s SetString
	// After consuming <set>, the trailing "<" is not a valid token, so the
	// d.Token() call inside UnmarshalXML returns a syntax error.
	d := xml.NewDecoder(bytes.NewReader([]byte("<set><")))
	d.Token()
	err := s.UnmarshalXML(d, xml.StartElement{Name: xml.Name{Local: "set"}})
	require.Error(t, err, "invalid token input should surface a Token error")
}

func TestSetString_UnmarshalXMLUnknownChildSkipError(t *testing.T) {
	var s SetString
	d := xml.NewDecoder(bytes.NewReader([]byte("<set><bad><nested></name></set>")))
	d.Token()
	err := s.UnmarshalXML(d, xml.StartElement{Name: xml.Name{Local: "set"}})
	require.Error(t, err, "malformed unknown child should surface a Skip error")
}

func TestSetString_UnmarshalXMLNameDecodeError(t *testing.T) {
	var s SetString
	// <name> with a mismatched nested element fails d.DecodeElement.
	d := xml.NewDecoder(bytes.NewReader([]byte("<set><name><unclosed></name></set>")))
	d.Token()
	err := s.UnmarshalXML(d, xml.StartElement{Name: xml.Name{Local: "set"}})
	require.Error(t, err, "malformed <name> child should surface a DecodeElement error")
}

func TestSetString_UnmarshalCDATAChardataPreserved(t *testing.T) {
	// Regression for Codex P2: a legacy flat value split across CharData by a
	// CDATA section must not be truncated to the first token.
	xmlStr := `<movie><set>Part 1 <![CDATA[& Part 2]]></set></movie>`
	var movie Movie
	err := xml.Unmarshal([]byte(xmlStr), &movie)
	require.NoError(t, err)
	assert.Equal(t, "Part 1 & Part 2", string(movie.Set),
		"chardata split by a CDATA section must be concatenated")
}
