package worker

import (
	"encoding/json"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRescrapeStatus_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status models.RescrapeStatus
		want   string
	}{
		{name: "zero value", status: models.RescrapeStatus(""), want: ""},
		{name: "success", status: models.RescrapeStatusSuccess, want: "success"},
		{name: "failed", status: models.RescrapeStatusFailed, want: "failed"},
		{name: "gone", status: models.RescrapeStatusGone, want: "gone"},
		{name: "conflict", status: models.RescrapeStatusConflict, want: "conflict"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tt.status.String())
		})
	}
}

func TestRescrapeStatus_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status models.RescrapeStatus
	}{
		{name: "success", status: models.RescrapeStatusSuccess},
		{name: "failed", status: models.RescrapeStatusFailed},
		{name: "gone", status: models.RescrapeStatusGone},
		{name: "conflict", status: models.RescrapeStatusConflict},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tt.status)
			require.NoError(t, err)
			require.JSONEq(t, `"`+tt.status.String()+`"`, string(data))

			var got models.RescrapeStatus
			require.NoError(t, json.Unmarshal(data, &got))
			require.Equal(t, tt.status, got)
		})
	}
}

func TestRescrapeStatus_UnmarshalJSON_ZeroValue(t *testing.T) {
	t.Parallel()

	var got models.RescrapeStatus
	require.NoError(t, json.Unmarshal([]byte(`"success"`), &got))
	require.Equal(t, models.RescrapeStatusSuccess, got)
}

func TestRescrapeStatus_UnmarshalJSON_ArbitraryString(t *testing.T) {
	t.Parallel()

	var got models.RescrapeStatus
	require.NoError(t, json.Unmarshal([]byte(`"unknown"`), &got))
	assert.Equal(t, models.RescrapeStatus("unknown"), got)
}

func TestRescrapeStatus_UnmarshalJSON_NonString(t *testing.T) {
	t.Parallel()

	var got models.RescrapeStatus
	err := json.Unmarshal([]byte(`42`), &got)
	require.Error(t, err)
}

func TestRescrapeStatus_Scan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input any
		want  models.RescrapeStatus
	}{
		{name: "nil", input: nil, want: models.RescrapeStatus("")},
		{name: "string success", input: "success", want: models.RescrapeStatusSuccess},
		{name: "string failed", input: "failed", want: models.RescrapeStatusFailed},
		{name: "string gone", input: "gone", want: models.RescrapeStatusGone},
		{name: "string conflict", input: "conflict", want: models.RescrapeStatusConflict},
		{name: "bytes", input: []byte("success"), want: models.RescrapeStatusSuccess},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got models.RescrapeStatus
			require.NoError(t, got.Scan(tt.input))
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRescrapeStatus_Scan_InvalidType(t *testing.T) {
	t.Parallel()

	var got models.RescrapeStatus
	err := got.Scan(42)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RescrapeStatus")
}

func TestRescrapeStatus_Value(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status models.RescrapeStatus
		want   string
	}{
		{name: "success", status: models.RescrapeStatusSuccess, want: "success"},
		{name: "failed", status: models.RescrapeStatusFailed, want: "failed"},
		{name: "gone", status: models.RescrapeStatusGone, want: "gone"},
		{name: "conflict", status: models.RescrapeStatusConflict, want: "conflict"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.status.Value()
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
