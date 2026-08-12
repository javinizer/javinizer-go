package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type capableStub struct{ fields []string }

func (c capableStub) ActressFields() []string { return c.fields }

func TestResolverSupportsActressField(t *testing.T) {
	require.True(t, ResolverSupportsActressField(struct{}{}, "actress"), "undeclared resolver is fully capable")
	require.True(t, ResolverSupportsActressField(capableStub{}, "actress"), "empty declaration declares everything")
	require.True(t, ResolverSupportsActressField(capableStub{[]string{"actress_url"}}, "actress_url"))
	require.False(t, ResolverSupportsActressField(capableStub{[]string{"actress_url"}}, "actress_first_name"))
}
