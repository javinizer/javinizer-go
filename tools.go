//go:build tools
// +build tools

// Package tools tracks development tool dependencies.
// This file ensures that `go mod` tracks tool dependencies used via `go run`.
//
// Tools are not installed globally but run via `go run github.com/tool/name@version`
// This keeps all dependencies project-local and version-controlled.
package tools

import (
	_ "github.com/ory/go-acc" // Coverage aggregation tool (used in make coverage)
)
