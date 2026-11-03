package template

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// This tests the existing private, per-engine resolver callback contract. It
// does not claim that a currently registered built-in resolver returns errors.
func TestResolveTagPreservesResolverFailureAndExecuteFallsBack(t *testing.T) {
	engine := NewEngine()
	original := engine.tagRegistry["TITLE"]
	defer func() { engine.tagRegistry["TITLE"] = original }()
	failure := errors.New("owned resolver failure")
	calls := 0
	engine.tagRegistry["TITLE"] = func(*Context) (string, error) {
		calls++
		return "must not leak", failure
	}
	ctx := &Context{
		Title: "Original title",
		Translations: map[string]models.MovieTranslation{
			"en": {Language: "en", Title: "Translated title"},
		},
	}

	value, err := engine.resolveTag("TITLE", "en", ctx)
	require.Empty(t, value)
	require.Same(t, failure, err, "resolver errors precede translation and modifiers")
	require.Equal(t, 1, calls)

	// Execute intentionally substitutes an empty string for a failed tag.
	// Repeated identical tags share one resolution per execution.
	calls = 0
	output, err := engine.Execute("before<TITLE:en>between<TITLE:en>after", ctx)
	require.NoError(t, err)
	require.Equal(t, "beforebetweenafter", output)
	require.Equal(t, 1, calls)

	engine.tagRegistry["TITLE"] = original
	output, err = engine.Execute("<TITLE:en>", ctx)
	require.NoError(t, err)
	require.Equal(t, "Translated title", output, "a failed render must not poison later execution")
}
