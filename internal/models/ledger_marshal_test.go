package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshalLedgerJSON(t *testing.T) {
	s := MarshalLedgerJSON(GeneratedFilesJSON{})
	require.Equal(t, `{}`, s)

	s = MarshalLedgerJSON(GeneratedFilesJSON{
		Replacements: []ReplacementEntry{{Destination: "/d", Backup: "/b", DestSeq: 1}},
		Roots:        []string{"/root"},
	})
	require.Contains(t, s, `"dest_seq":1`)
	require.Contains(t, s, `"roots"`)
}
