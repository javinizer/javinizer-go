package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestR18DumpReferencesAreOpaqueOwnedAndNeverJSONAuthority(t *testing.T) {
	var absent *ScraperResult
	require.Nil(t, absent.R18DumpActorReferences())
	r := &ScraperResult{Title: "Keep omitted public fields"}
	require.Nil(t, r.R18DumpActorReferences())
	ids := []string{"7", "007", "opaque-A"}
	r.SetR18DumpActorReferences(ids)
	ids[0] = "changed"
	require.Equal(t, []string{"7", "007", "opaque-A"}, r.R18DumpActorReferences())
	getter := r.R18DumpActorReferences()
	getter[0] = "changed"
	clone := r.Clone()
	clone.SetR18DumpActorReferences([]string{"other"})
	require.Equal(t, []string{"7", "007", "opaque-A"}, r.R18DumpActorReferences())
	encoded, err := json.Marshal(r)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "opaque-A")
	require.NoError(t, json.Unmarshal([]byte(`{"source":"r18dev","r18DumpActorReferences":["7"],"r18_dump_actor_references":["007"]}`), r))
	require.Nil(t, r.R18DumpActorReferences())
	require.Equal(t, "Keep omitted public fields", r.Title)
	r.SetR18DumpActorReferences([]string{"7"})
	require.Error(t, json.Unmarshal([]byte(`{"title":7}`), r))
	require.Nil(t, r.R18DumpActorReferences(), "failed typed JSON decode still clears authority")
	r.SetR18DumpActorReferences([]string{})
	require.NotNil(t, r.R18DumpActorReferences(), "known empty dump cast is not HTTP identity")
	require.NotNil(t, r.Clone().R18DumpActorReferences())
	r.SetR18DumpActorReferences(nil)
	require.Nil(t, r.R18DumpActorReferences())
	r.SetR18DumpActorReferences(make([]string, 1000))
	require.Len(t, r.R18DumpActorReferences(), 65, "retain one bounded overflow sentinel")
}
