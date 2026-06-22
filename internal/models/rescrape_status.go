package models

import (
	"database/sql/driver"
	"fmt"
)

// RescrapeStatus classifies the final result of a rescrape operation.
type RescrapeStatus string

const (
	RescrapeStatusSuccess  RescrapeStatus = "success"  // Scrape succeeded, result committed
	RescrapeStatusFailed   RescrapeStatus = "failed"   // Scrape failed or produced no result
	RescrapeStatusGone     RescrapeStatus = "gone"     // Job was deleted or reached terminal state
	RescrapeStatusConflict RescrapeStatus = "conflict" // Revision conflict — concurrent modification detected
)

// String implements fmt.Stringer.
func (s RescrapeStatus) String() string { return string(s) }

// MarshalJSON implements json.Marshaler.
func (s RescrapeStatus) MarshalJSON() ([]byte, error) {
	return MarshalStringEnum(string(s))
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *RescrapeStatus) UnmarshalJSON(data []byte) error {
	return UnmarshalStringEnum((*string)(s), data)
}

// Scan implements sql.Scanner for GORM compatibility.
func (s *RescrapeStatus) Scan(value any) error {
	if err := ScanStringEnum((*string)(s), value); err != nil {
		return fmt.Errorf("RescrapeStatus: %w", err)
	}
	return nil
}

// Value implements driver.Valuer for GORM compatibility.
func (s RescrapeStatus) Value() (driver.Value, error) {
	return StringEnumValue(string(s))
}
