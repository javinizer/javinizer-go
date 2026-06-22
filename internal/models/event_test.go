package models

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventCategory_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "scraper", EventCategoryScraper.String())
	assert.Equal(t, "organize", EventCategoryOrganize.String())
	assert.Equal(t, "system", EventCategorySystem.String())
}

func TestEventCategory_MarshalJSON(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(EventCategoryScraper)
	require.NoError(t, err)
	assert.Equal(t, `"scraper"`, string(data))
}

func TestEventCategory_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	var e EventCategory
	require.NoError(t, json.Unmarshal([]byte(`"organize"`), &e))
	assert.Equal(t, EventCategoryOrganize, e)
}

func TestEventCategory_UnmarshalJSON_Invalid(t *testing.T) {
	t.Parallel()
	var e EventCategory
	err := json.Unmarshal([]byte(`123`), &e)
	assert.Error(t, err)
}

func TestEventCategory_Scan_String(t *testing.T) {
	t.Parallel()
	var e EventCategory
	require.NoError(t, e.Scan("system"))
	assert.Equal(t, EventCategorySystem, e)
}

func TestEventCategory_Scan_Bytes(t *testing.T) {
	t.Parallel()
	var e EventCategory
	require.NoError(t, e.Scan([]byte("scraper")))
	assert.Equal(t, EventCategoryScraper, e)
}

func TestEventCategory_Scan_Nil(t *testing.T) {
	t.Parallel()
	var e EventCategory
	require.NoError(t, e.Scan(nil))
	assert.Equal(t, EventCategory(""), e)
}

func TestEventCategory_Value(t *testing.T) {
	t.Parallel()
	v, err := EventCategoryOrganize.Value()
	require.NoError(t, err)
	assert.Equal(t, "organize", v)
}

func TestEventCategory_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	for _, cat := range []EventCategory{EventCategoryScraper, EventCategoryOrganize, EventCategorySystem} {
		data, err := json.Marshal(cat)
		require.NoError(t, err)
		var got EventCategory
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, cat, got)
	}
}

func TestEventCategory_ScanRoundTrip(t *testing.T) {
	t.Parallel()
	for _, cat := range []EventCategory{EventCategoryScraper, EventCategoryOrganize, EventCategorySystem} {
		v, err := cat.Value()
		require.NoError(t, err)
		var got EventCategory
		require.NoError(t, got.Scan(v))
		assert.Equal(t, cat, got)
	}
}

// ---------------------------------------------------------------------------
// EventSeverity
// ---------------------------------------------------------------------------

func TestEventSeverity_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "debug", SeverityDebug.String())
	assert.Equal(t, "info", SeverityInfo.String())
	assert.Equal(t, "warn", SeverityWarn.String())
	assert.Equal(t, "error", SeverityError.String())
}

func TestEventSeverity_MarshalJSON(t *testing.T) {
	t.Parallel()
	data, err := json.Marshal(SeverityWarn)
	require.NoError(t, err)
	assert.Equal(t, `"warn"`, string(data))
}

func TestEventSeverity_UnmarshalJSON(t *testing.T) {
	t.Parallel()
	var e EventSeverity
	require.NoError(t, json.Unmarshal([]byte(`"error"`), &e))
	assert.Equal(t, SeverityError, e)
}

func TestEventSeverity_UnmarshalJSON_Invalid(t *testing.T) {
	t.Parallel()
	var e EventSeverity
	err := json.Unmarshal([]byte(`[]`), &e)
	assert.Error(t, err)
}

func TestEventSeverity_Scan_String(t *testing.T) {
	t.Parallel()
	var e EventSeverity
	require.NoError(t, e.Scan("info"))
	assert.Equal(t, SeverityInfo, e)
}

func TestEventSeverity_Scan_Bytes(t *testing.T) {
	t.Parallel()
	var e EventSeverity
	require.NoError(t, e.Scan([]byte("debug")))
	assert.Equal(t, SeverityDebug, e)
}

func TestEventSeverity_Scan_Nil(t *testing.T) {
	t.Parallel()
	var e EventSeverity
	require.NoError(t, e.Scan(nil))
	assert.Equal(t, EventSeverity(""), e)
}

func TestEventSeverity_Value(t *testing.T) {
	t.Parallel()
	v, err := SeverityDebug.Value()
	require.NoError(t, err)
	assert.Equal(t, "debug", v)
}

func TestEventSeverity_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []EventSeverity{SeverityDebug, SeverityInfo, SeverityWarn, SeverityError} {
		data, err := json.Marshal(s)
		require.NoError(t, err)
		var got EventSeverity
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, s, got)
	}
}

func TestEventSeverity_ScanRoundTrip(t *testing.T) {
	t.Parallel()
	for _, s := range []EventSeverity{SeverityDebug, SeverityInfo, SeverityWarn, SeverityError} {
		v, err := s.Value()
		require.NoError(t, err)
		var got EventSeverity
		require.NoError(t, got.Scan(v))
		assert.Equal(t, s, got)
	}
}

// ---------------------------------------------------------------------------
// Event struct
// ---------------------------------------------------------------------------

func TestEvent_TableName(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "events", Event{}.TableName())
}

func TestEvent_Struct(t *testing.T) {
	t.Parallel()
	e := Event{
		ID:        1,
		EventType: EventCategoryScraper,
		Severity:  SeverityError,
		Message:   "scrape failed",
		Context:   `{"url":"https://example.com"}`,
		Source:    "r18dev",
	}
	assert.Equal(t, EventCategoryScraper, e.EventType)
	assert.Equal(t, SeverityError, e.Severity)
	assert.Equal(t, "scrape failed", e.Message)
	assert.Equal(t, "r18dev", e.Source)
}
