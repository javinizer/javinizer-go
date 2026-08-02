package javdb

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// An exact-name search returning multiple DISTINCT actor IDs must not attach
// an arbitrary profile to the requested identity.
func TestFindActorIDRejectsAmbiguousNames(t *testing.T) {
	s := actorTestScraper(&staticRoundTripper{responses: map[string]string{
		"https://javdb.test/actors?locale=en&search=name": `<a href="/actors/AA" title="name"></a><a href="/actors/BB">name</a>`,
	}})
	ambiguous, ambErr := s.findActorID(context.Background(), "name")
	require.NoError(t, ambErr)
	require.Empty(t, ambiguous)

	// Repeated links to the SAME actor ID remain resolvable.
	s.client.SetTransport(&staticRoundTripper{responses: map[string]string{
		"https://javdb.test/actors?locale=en&search=name": `<a href="/actors/AA" title="name"></a><a href="/actors/AA">name</a>`,
	}})
	single, singleErr := s.findActorID(context.Background(), "name")
	require.NoError(t, singleErr)
	require.Equal(t, "AA", single)
}
