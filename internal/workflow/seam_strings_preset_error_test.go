package workflow

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/stretchr/testify/require"
)

func TestResolveSeamStringsPropagatesPresetApplicationError(t *testing.T) {
	for _, preset := range []string{" conservative ", "\tGAP-FILL\n", " aggressive\t"} {
		t.Run(preset, func(t *testing.T) {
			// Validation normalizes whitespace, but the typed preset applicator
			// currently receives the original input. Preserve its error rather
			// than returning a partially resolved configuration on this boundary.
			normalized, err := ResolvePreset(preset)
			require.NoError(t, err)
			_, _, applicationErr := nfo.ApplyPresetTyped(preset, nfo.PreferNFO, true)
			require.Error(t, applicationErr)

			resolved, err := ResolveSeamStrings(SeamStringsInput{Preset: preset})
			require.Nil(t, resolved)
			require.EqualError(t, err, applicationErr.Error())
			require.NotErrorIs(t, err, ErrInvalidPreset, "failure comes from application, not validation")

			// The normalized counterpart remains usable.
			resolved, err = ResolveSeamStrings(SeamStringsInput{Preset: normalized})
			require.NoError(t, err)
			require.NotNil(t, resolved)
			scalar, array, err := nfo.ApplyPresetTyped(normalized, nfo.PreferNFO, true)
			require.NoError(t, err)
			require.Equal(t, scalar, resolved.ScalarStrategy)
			require.Equal(t, array, resolved.ArrayStrategy)
		})
	}
}
