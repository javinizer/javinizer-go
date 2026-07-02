package nfo

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingWriter is an io.Writer that always returns an error.
type failingWriter struct{}

func (failingWriter) Write(p []byte) (int, error) {
	return 0, errors.New("write failed")
}

// failAfterNWriter succeeds for the first N bytes then fails.
type failAfterNWriter struct {
	remaining int
}

func (w *failAfterNWriter) Write(p []byte) (int, error) {
	if w.remaining <= 0 {
		return 0, errors.New("write failed")
	}
	n := len(p)
	if n > w.remaining {
		n = w.remaining
	}
	w.remaining -= n
	if n < len(p) {
		return n, errors.New("write failed")
	}
	return n, nil
}

// failingWriteFs wraps an afero.Fs and returns files that fail on Write.
type failingWriteFs struct {
	afero.Fs
}

type failingWriteFile struct {
	afero.File
}

func (fs *failingWriteFs) Create(name string) (afero.File, error) {
	f, err := fs.Fs.Create(name)
	if err != nil {
		return nil, err
	}
	return &failingWriteFile{File: f}, nil
}

func (f *failingWriteFile) Write(p []byte) (int, error) {
	return 0, errors.New("disk full")
}

func (f *failingWriteFile) WriteString(s string) (int, error) {
	return 0, errors.New("disk full")
}

func TestWriteNFO_SpecialCharactersNotNumericEscaped(t *testing.T) {
	fs := afero.NewMemMapFs()
	gen := NewGenerator(fs, &Config{})
	nfo := &Movie{
		Title:  `Test "Quote" 'Apostrophe' <Tag> & Ampersand`,
		Plot:   `She said "hello" & he didn't <3 it`,
		Studio: `Studio's "Best" & <Co>`,
	}

	err := gen.WriteNFO(nfo, "/test.nfo")
	require.NoError(t, err)

	data, err := afero.ReadFile(fs, "/test.nfo")
	require.NoError(t, err)
	output := string(data)

	assert.Contains(t, output, `"Quote"`, `double quotes should be literal, not &#34;`)
	assert.Contains(t, output, `'Apostrophe'`, `single quotes should be literal, not &#39;`)
	assert.Contains(t, output, `didn't`, `apostrophes in text should be literal`)
	assert.Contains(t, output, `Studio's`, `apostrophes in studio should be literal`)
	assert.NotContains(t, output, `&#34;`, "numeric quote escaping should not appear in element text")
	assert.NotContains(t, output, `&#39;`, "numeric apostrophe escaping should not appear in element text")

	assert.Contains(t, output, "&lt;Tag&gt;", "angle brackets must still be escaped")
	assert.Contains(t, output, "&amp; Ampersand", "ampersands must still be escaped")
	assert.Contains(t, output, "&lt;3", "angle brackets in plot must be escaped")
}

func TestWriteNFO_LiteralNumericEntityInSource(t *testing.T) {
	fs := afero.NewMemMapFs()
	gen := NewGenerator(fs, &Config{})
	nfo := &Movie{
		Title: `Literal &#34; in source`,
	}

	err := gen.WriteNFO(nfo, "/test.nfo")
	require.NoError(t, err)

	data, err := afero.ReadFile(fs, "/test.nfo")
	require.NoError(t, err)
	output := string(data)

	assert.Contains(t, output, "&amp;#34;", "literal &#34; in source must have its & escaped to &amp;")
}

func TestWriteNFO_QuoteInAttributeNotCorrupted(t *testing.T) {
	fs := afero.NewMemMapFs()
	gen := NewGenerator(fs, &Config{})
	nfo := &Movie{
		Title: `He said "hi"`,
		Thumb: []Thumb{{
			Aspect:  "poster",
			Preview: `Bob"s preview`, // attribute value containing a quote
			Value:   `He said "hi"`,  // element text containing a quote
		}},
		Ratings: ratings{Rating: []rating{{
			Name:  `Mary's rating`, // attribute value containing an apostrophe
			Value: 9.5,
		}}},
	}

	err := gen.WriteNFO(nfo, "/test.nfo")
	require.NoError(t, err)

	data, err := afero.ReadFile(fs, "/test.nfo")
	require.NoError(t, err)
	output := string(data)

	// Element text: quotes/apostrophes MUST be unescaped.
	assert.Contains(t, output, `He said "hi"`, "element text quotes must be unescaped")

	// Attribute values: quotes/apostrophes MUST stay escaped (not corrupted).
	assert.Contains(t, output, `preview="Bob&#34;s preview"`, "attribute quotes must stay escaped")
	assert.Contains(t, output, `name="Mary&#39;s rating"`, "attribute apostrophes must stay escaped")
	assert.NotContains(t, output, `Bob"s preview`, "attribute value must not be corrupted by global unescape")
	assert.NotContains(t, output, `Mary's rating`, "attribute apostrophe must not be corrupted")
}

func TestUnescapeQuotesInText(t *testing.T) {
	testCases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "element text quotes unescaped",
			in:   `<title>He said &#34;hi&#34;</title>`,
			want: `<title>He said "hi"</title>`,
		},
		{
			name: "element text apostrophes unescaped",
			in:   `<plot>didn&#39;t</plot>`,
			want: `<plot>didn't</plot>`,
		},
		{
			name: "attribute quotes preserved",
			in:   `<genre name="Bob&#34;s"></genre>`,
			want: `<genre name="Bob&#34;s"></genre>`,
		},
		{
			name: "attribute apostrophes preserved",
			in:   `<genre name="Mary&#39;s"></genre>`,
			want: `<genre name="Mary&#39;s"></genre>`,
		},
		{
			name: "xml declaration untouched",
			in:   `<?xml version="1.0" encoding="UTF-8"?><r>&#34;</r>`,
			want: `<?xml version="1.0" encoding="UTF-8"?><r>"</r>`,
		},
		{
			name: "gt inside quoted attribute does not end tag",
			in:   `<tag attr="a>b">x&#34;y</tag>`,
			want: `<tag attr="a>b">x"y</tag>`,
		},
		{
			name: "self-closing tag",
			in:   `<empty/>&#34;`,
			want: `<empty/>"`,
		},
		{
			name: "no entities",
			in:   `<title>plain text</title>`,
			want: `<title>plain text</title>`,
		},
		{
			name: "truncated entity left intact",
			in:   `<r>&#3</r>`,
			want: `<r>&#3</r>`,
		},
		{
			name: "ampersand already escaped stays escaped",
			in:   `<r>&amp; &#34;</r>`,
			want: `<r>&amp; "</r>`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, unescapeQuotesInText(tc.in))
		})
	}
}

func TestWriteNFOXML_HeaderWriteError(t *testing.T) {
	err := writeNFOXML(failingWriter{}, &Movie{Title: "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write XML header")
}

func TestWriteNFOXML_EncodeError(t *testing.T) {
	w := &failAfterNWriter{remaining: len(xml.Header)}
	err := writeNFOXML(w, &Movie{Title: "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to encode NFO")
}

func TestWriteNFOXML_NewlineWriteError(t *testing.T) {
	nfo := &Movie{Title: "test"}
	var buf bytes.Buffer
	require.NoError(t, writeNFOXML(&buf, nfo))
	totalSize := buf.Len()

	w := &failAfterNWriter{remaining: totalSize - 1}
	err := writeNFOXML(w, nfo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write final newline")
}

func TestWriteNFO_WriteFailure(t *testing.T) {
	fs := &failingWriteFs{Fs: afero.NewMemMapFs()}
	gen := NewGenerator(fs, &Config{})
	nfo := &Movie{Title: `Test`}

	err := gen.WriteNFO(nfo, "/test.nfo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write NFO file")
}

func TestWriteNFO_EncodeFailure(t *testing.T) {
	fs := afero.NewMemMapFs()
	gen := NewGenerator(fs, &Config{})
	gen.encodeFunc = func(io.Writer, *Movie) error {
		return fmt.Errorf("injected encode failure")
	}

	err := gen.WriteNFO(&Movie{Title: "test"}, "/test.nfo")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "injected encode failure")
}
