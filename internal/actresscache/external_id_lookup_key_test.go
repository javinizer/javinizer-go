package actresscache_test

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/stretchr/testify/require"
)

func TestExternalIDLookupKeyPreservesExactLookup(t *testing.T) {
	s := build(t, evidence())
	require.Equal(t, actresscache.UniqueLookupResult{}, s.LookupUniqueKey(actresscache.ExternalIDLookupKey{}))

	for _, ref := range []actresscache.ExternalID{
		{Namespace: "dmm", Value: "7"},
		{Namespace: "dmm", Value: "07"},
		{Namespace: "other", Value: "01"},
		{Namespace: "DMM", Value: "7"},
	} {
		t.Run(ref.Namespace+"/"+ref.Value, func(t *testing.T) {
			want, wantErr := s.LookupUnique(ref.Namespace, ref.Value)
			key, err := actresscache.NewExternalIDLookupKey(ref.Namespace, ref.Value)
			if wantErr != nil {
				require.EqualError(t, err, wantErr.Error())
				require.Equal(t, actresscache.UniqueLookupResult{}, s.LookupUniqueKey(key))
				return
			}
			require.NoError(t, err)
			require.Equal(t, want, s.LookupUniqueKey(key))
		})
	}
}
