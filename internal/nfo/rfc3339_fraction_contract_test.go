package nfo_test

import (
	"encoding/xml"
	"fmt"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestParseNFOFractionalReleaseDateContract(t *testing.T) {
	for _, tc := range []struct {
		name, value   string
		nanos, offset int
	}{
		{"whole seconds", "2024-02-29T12:34:56Z", 0, 0},
		{"decimal tenth", "2024-02-29T12:34:56.1Z", 100000000, 0},
		{"comma tenth", "2024-02-29T12:34:56,1Z", 100000000, 0},
		{"nanoseconds", "2024-02-29T12:34:56.123456789Z", 123456789, 0},
		{"truncate subnanoseconds", "2024-02-29T12:34:56.123456789987654321Z", 123456789, 0},
		{"comma subnanoseconds", "2024-02-29T12:34:56,123456789987654321Z", 123456789, 0},
		{"positive timezone", "2024-02-29T12:34:56.123456789+05:30", 123456789, 19800},
		{"negative timezone", "2024-02-29T12:34:56.123456789-07:00", 123456789, -25200},
		{"whitespace", " 2024-02-29T12:34:56.000000001Z \n", 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := parseDateContractNFO(t, tc.value, "1999-01-01", 0)
			require.Empty(t, result.Warnings)
			require.NotNil(t, result.Movie.ReleaseDate)
			expected := time.Date(2024, 2, 29, 12, 34, 56, tc.nanos, time.FixedZone("", tc.offset))
			require.True(t, expected.Equal(*result.Movie.ReleaseDate))
			_, offset := result.Movie.ReleaseDate.Zone()
			require.Equal(t, tc.offset, offset)
			require.Equal(t, tc.nanos, result.Movie.ReleaseDate.Nanosecond())
			require.Equal(t, 2024, result.Movie.ReleaseYear)
		})
	}
}

func TestParseNFODateFailureAndFallbackContract(t *testing.T) {
	for _, value := range []string{
		"2024-02-29T12:34:60.1Z", "2024-13-29T12:34:56.1Z",
		"2024-02-30T12:34:56.1Z", "2024-02-29T12:34:56.1+25:00",
		"2024-02-29T12:34:56.Z", "2024-02-29T12:34:56.1Z trailing",
	} {
		t.Run(value, func(t *testing.T) {
			result := parseDateContractNFO(t, value, "1999-01-01", 2001)
			require.Nil(t, result.Movie.ReleaseDate, "invalid preferred releasedate must not silently use premiered")
			require.Equal(t, 2001, result.Movie.ReleaseYear)
			require.Equal(t, []string{fmt.Sprintf("failed to parse date %q: unable to parse date %q with known formats", value, value)}, result.Warnings)
		})
	}
	for _, value := range []string{"2024-02-29", "2024/02/29", "29-02-2024", "02/29/2024"} {
		t.Run(value, func(t *testing.T) {
			result := parseDateContractNFO(t, "", value, 2001)
			require.Empty(t, result.Warnings)
			require.NotNil(t, result.Movie.ReleaseDate)
			require.True(t, time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC).Equal(*result.Movie.ReleaseDate))
			require.Equal(t, 2001, result.Movie.ReleaseYear, "explicit year remains authoritative")
		})
	}
}

func parseDateContractNFO(t *testing.T, releasedate, premiered string, year int) *nfo.ParseResult {
	t.Helper()
	fs := afero.NewMemMapFs()
	data, err := xml.Marshal(nfo.Movie{Title: "Date contract", ID: "IPX-535", ReleaseDate: releasedate, Premiered: premiered, Year: year})
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, "/fixture.nfo", data, 0600))
	result, err := nfo.ParseNFO(fs, "/fixture.nfo")
	require.NoError(t, err)
	require.Equal(t, "/fixture.nfo", result.Source)
	require.Equal(t, "Date contract", result.NFOTitle)
	return result
}
