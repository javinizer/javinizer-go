package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseGeneratedFiles(t *testing.T) {
	gf, err := ParseGeneratedFiles("")
	require.NoError(t, err)
	require.Empty(t, gf.Delete)
	require.Empty(t, gf.Replacements)

	_, err = ParseGeneratedFiles(`{"delete":bogus`)
	require.Error(t, err, "malformed JSON surfaces")

	gf, err = ParseGeneratedFiles(`{"delete":["/a"],"move_back":[{"original_path":"/b","new_path":"/c"}],"replacements":[{"destination":"/d","backup":"/e","dest_seq":3}]}`)
	require.NoError(t, err)
	require.Equal(t, int64(3), gf.Replacements[0].DestSeq)
	require.Len(t, gf.MoveBack, 1)
}
