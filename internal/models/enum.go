package models

import "github.com/javinizer/javinizer-go/internal/enumutil"

// String enum helpers — delegate to enumutil so all packages (including those
// that cannot import models due to circular dependencies) can use the same
// implementation via the enumutil leaf package.

// ScanStringEnum scans a SQL value into a string enum pointer.
var ScanStringEnum = enumutil.ScanStringEnum

// StringEnumValue returns the SQL driver value for a string enum.
var StringEnumValue = enumutil.StringEnumValue

// MarshalStringEnum marshals a string enum to JSON.
var MarshalStringEnum = enumutil.MarshalStringEnum

// UnmarshalStringEnum unmarshals a JSON value into a string enum pointer.
var UnmarshalStringEnum = enumutil.UnmarshalStringEnum
