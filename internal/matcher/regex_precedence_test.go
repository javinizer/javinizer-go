package matcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Preserve custom regex precedence over every automated ASCII remaster tier.
func TestMatchFile_CustomRegexOutranksAutomatedTiers(t *testing.T) {
	cfg := &Config{RegexEnabled: true, RegexPattern: `(rct\d+)`}
	m, err := NewMatcher(cfg)
	require.NoError(t, err)

	got := matchOne(t, m, "1rct00156h.mkv")
	require.NotNil(t, got)
	assert.Equal(t, "RCT00156", got.ID, "custom regex capture must win over the automated content id")
	assert.Equal(t, "regex", got.MatchedBy)

	// Control: without a custom regex the same filename resolves to the
	// tier-2 content id, proving the regex produced the different id.
	plain, err := NewMatcher(&Config{})
	require.NoError(t, err)
	plainGot := matchOne(t, plain, "1rct00156h.mkv")
	require.NotNil(t, plainGot)
	assert.Equal(t, "1RCT00156H", plainGot.ID)
	assert.Equal(t, "contentid", plainGot.MatchedBy)
}

func TestMatchString_CustomRegexSeesArgumentUntouched(t *testing.T) {
	cfg := &Config{RegexEnabled: true, RegexPattern: `^special_(.+)\.mkv$`}
	m, err := NewMatcher(cfg)
	require.NoError(t, err)
	assert.Equal(t, "FOO", m.MatchString("special_foo.mkv"), "extension-anchored custom regex must see the argument with its extension")

	pathCfg := &Config{RegexEnabled: true, RegexPattern: `^special/([a-z]+)\.mkv$`}
	pathMatcher, err := NewMatcher(pathCfg)
	require.NoError(t, err)
	assert.Equal(t, "FOO", pathMatcher.MatchString("special/foo.mkv"), "path-anchored custom regex must see the argument with its path")

	stemCfg := &Config{RegexEnabled: true, RegexPattern: `^(ABC)-(\d+)$`}
	stemMatcher, err := NewMatcher(stemCfg)
	require.NoError(t, err)
	assert.Equal(t, "ABC", stemMatcher.MatchString("ABC-123.mkv"), "stem-anchored custom regex must keep matching via the folded retry")
	assert.Equal(t, "ABC", stemMatcher.MatchString("ABC-123"))

	// A caret-anchored prefix regex on a path argument matches through the
	// basename retry, not the raw attempt: the raw argument keeps its path.
	prefixCfg := &Config{RegexEnabled: true, RegexPattern: `^(IPX-\d+)`}
	prefixMatcher, err := NewMatcher(prefixCfg)
	require.NoError(t, err)
	assert.Equal(t, "IPX-535", prefixMatcher.MatchString("dir/IPX-535.mkv"))
}

func TestMatchString_CustomRegexOutranksAutomatedTiers(t *testing.T) {
	t.Run("outranks tier-2 content id", func(t *testing.T) {
		cfg := &Config{RegexEnabled: true, RegexPattern: `(rct\d+)`}
		m, err := NewMatcher(cfg)
		require.NoError(t, err)
		assert.Equal(t, "RCT00156", m.MatchString("1rct00156h.mp4"), "custom regex must capture the intended id")

		plain, err := NewMatcher(&Config{})
		require.NoError(t, err)
		assert.Equal(t, "1RCT00156H", plain.MatchString("1rct00156h.mp4"), "control: automated path still yields the content id")
	})
}
