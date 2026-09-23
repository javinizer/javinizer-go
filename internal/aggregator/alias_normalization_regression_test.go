package aggregator

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type normalizedAliasLookupStub struct {
	aliases map[string]string
}

func (s normalizedAliasLookupStub) GetAliasMap(context.Context) (map[string]string, error) {
	return s.aliases, nil
}

func TestAliasResolverNormalizesLoadedKeysAndLookups(t *testing.T) {
	cfg := &MetadataConfig{ActressDatabase: actressDatabaseConfigView{Enabled: true, ConvertAlias: true}}
	resolver := NewAliasResolver(cfg, normalizedAliasLookupStub{aliases: map[string]string{
		"  Stage   Name ": "Canonical Person",
		"が":               "Japanese Canonical",
	}})

	actress := &models.Actress{FirstName: "STAGE", LastName: "name"}
	resolver.Resolve(actress)
	require.Equal(t, "Canonical", actress.LastName)
	require.Equal(t, "Person", actress.FirstName)
	require.Equal(t, "Canonical Person", resolver.CanonicalName("", " stage ", " NAME "))
	require.Equal(t, "Canonical Person", resolver.CanonicalName("", "ＳＴＡＧＥ", "ＮＡＭＥ"))
	require.Equal(t, "Japanese Canonical", resolver.CanonicalName("か\u3099", "", ""))
}
