package models

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPublicationRecoveryModePresence(t *testing.T) {
	zero := uint32(0)
	ledger := GeneratedFilesJSON{MoveBack: []FileMove{{Proof: &DeleteEntry{IdentityMode: &zero}}, {Proof: &DeleteEntry{}}}}
	parsed, err := ParseGeneratedFiles(MarshalLedgerJSON(ledger))
	require.NoError(t, err)
	require.NotNil(t, parsed.MoveBack[0].Proof.IdentityMode)
	require.Zero(t, *parsed.MoveBack[0].Proof.IdentityMode)
	require.Nil(t, parsed.MoveBack[1].Proof.IdentityMode)
}
