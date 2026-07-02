package nfo

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	assert.NotContains(t, output, `&#34;`, "numeric quote escaping should not appear")
	assert.NotContains(t, output, `&#39;`, "numeric apostrophe escaping should not appear")

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
	assert.NotContains(t, output, "&#34; ", "the literal entity should not be unescaped — only encoder-produced ones")
}
