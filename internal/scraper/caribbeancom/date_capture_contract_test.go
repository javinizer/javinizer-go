package caribbeancom

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReleaseDateRequiresSixDigitCalendarDate(t *testing.T) {
	for _, id := range []string{"10124-001", "1101240-001", "023124-001", "130124-001"} {
		t.Run(id, func(t *testing.T) { require.Nil(t, parseReleaseDateFromID(id)) })
	}
	got := parseReleaseDateFromID("022924-001")
	require.NotNil(t, got)
	require.Equal(t, "2024-02-29", got.Format("2006-01-02"))
}
