package matcher

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestASCIIActivationExtensionAndRegexPrecedence(t *testing.T) {
	for _, ext := range []string{".mp4", ".MKV", ".avi", ".wmv", ".flv", ".mov", ".m4v", ".webm", ".mpg", ".mpeg", ".m2ts", ".ts"} {
		m, err := NewMatcher(&Config{})
		require.NoError(t, err)
		got := m.MatchFile(models.FileMatchInfo{Name: "RCT-156-HD-2" + ext})
		require.NotNil(t, got)
		require.Equal(t, "RCT-156H", got.ID)
		require.Equal(t, 2, got.PartNumber)
		require.True(t, got.IsMultiPart)
		require.Equal(t, "RCT-156H", m.MatchString("RCT-156-HD-2"+ext))
	}
	for _, tc := range []struct{ name, pattern, want, by string }{
		{"special_foo.mkv", `^special_(.+)\.mkv$`, "FOO", "regex"},
		{"ABC-123.mkv", `^(ABC)-(\d+)$`, "ABC", "regex"},
		{"RCT156HD.mkv", `(ZZZ-\d+)`, "RCT-156H", "builtin"},
		{"1rct00156h.mkv", `(rct\d+)`, "RCT00156", "regex"},
	} {
		t.Run(tc.name+tc.pattern, func(t *testing.T) {
			m, err := NewMatcher(&Config{RegexEnabled: true, RegexPattern: tc.pattern})
			require.NoError(t, err)
			got := m.MatchFile(models.FileMatchInfo{Name: tc.name})
			require.NotNil(t, got)
			require.Equal(t, tc.want, got.ID)
			require.Equal(t, tc.by, got.MatchedBy)
			require.Equal(t, tc.want, m.MatchString(tc.name))
		})
	}
	for _, pattern := range []string{`^foo$`, `^(?:foo|(ABC-\d+))$`} {
		m, err := NewMatcher(&Config{RegexEnabled: true, RegexPattern: pattern})
		require.NoError(t, err)
		require.Empty(t, m.MatchString("foo.mkv"))
	}
	require.Equal(t, "RCT-156-HD.notes", stripVideoExtension("RCT-156-HD.notes"))
}
