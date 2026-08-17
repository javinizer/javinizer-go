package models

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarshalLedgerJSON_CoverageW1B_ValueOnlyContract(t *testing.T) {
	// Strings, including invalid UTF-8, are still JSON-marshalable. This
	// exercises the helper's real contract without relying on a fabricated
	// unsupported field value.
	got := MarshalLedgerJSON(GeneratedFilesJSON{
		Delete: []string{"\xff"},
		MoveBack: []FileMove{{
			OriginalPath: "old",
			NewPath:      "new",
		}},
		Replacements: []ReplacementEntry{{
			Destination: "/destination",
			Backup:      "/backup",
			DestSeq:     2,
			Installed:   true,
		}},
		Roots: []string{"/root"},
	})

	require.Contains(t, got, `"replacements"`)
	require.Contains(t, got, `"installed":true`)
	require.Contains(t, got, `"roots"`)
}
