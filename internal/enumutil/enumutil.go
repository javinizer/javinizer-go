// Package enumutil provides generic helpers for implementing string-backed
// enumeration types that satisfy json.Marshaler, json.Unmarshaler,
// sql.Scanner, and driver.Valuer.
//
// Extracted from internal/models so that packages that cannot import models
// (due to circular dependencies) can still use the same helpers. Both models
// and operationmode import this package; it has zero internal dependencies.
package enumutil

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// MarshalStringEnum marshals a string enum value to JSON bytes.
func MarshalStringEnum(s string) ([]byte, error) {
	return json.Marshal(s)
}

// UnmarshalStringEnum unmarshals JSON bytes into a string enum target.
func UnmarshalStringEnum(target *string, data []byte) error {
	var v string
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("invalid enum value: %w", err)
	}
	*target = v
	return nil
}

// ScanStringEnum implements sql.Scanner for a string enum target.
func ScanStringEnum(target *string, value any) error {
	if value == nil {
		*target = ""
		return nil
	}
	switch v := value.(type) {
	case string:
		*target = v
	case []byte:
		*target = string(v)
	default:
		return fmt.Errorf("cannot scan %T into string enum", value)
	}
	return nil
}

// StringEnumValue implements driver.Valuer for a string enum value.
func StringEnumValue(s string) (driver.Value, error) {
	return s, nil
}
