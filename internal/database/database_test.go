package database

import (
	"testing"

	"gorm.io/gorm/logger"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedLevel logger.LogLevel
	}{
		{"lowercase silent", "silent", logger.Silent},
		{"lowercase info", "info", logger.Info},
		{"lowercase warn", "warn", logger.Warn},
		{"lowercase error", "error", logger.Error},
		{"empty string defaults to silent", "", logger.Silent},
		{"mixed case Info", "Info", logger.Info},
		{"uppercase WARN", "WARN", logger.Warn},
		{"with leading whitespace", "  info", logger.Info},
		{"with trailing whitespace", "warn  ", logger.Warn},
		{"with both whitespace", "  error  ", logger.Error},
		{"invalid value defaults to silent", "invalid", logger.Silent},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the actual parseLogLevel function used in production
			result := parseLogLevel(tt.input)

			if result != tt.expectedLevel {
				t.Errorf("parseLogLevel(%q) = %v, want %v", tt.input, result, tt.expectedLevel)
			}
		})
	}
}
