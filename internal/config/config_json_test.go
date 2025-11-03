package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDatabaseConfigJSONSerialization(t *testing.T) {
	// Create a DatabaseConfig with all fields set
	dbConfig := DatabaseConfig{
		Type:     "sqlite",
		DSN:      "data/test.db",
		LogLevel: "silent",
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(dbConfig)
	if err != nil {
		t.Fatalf("Failed to marshal DatabaseConfig: %v", err)
	}

	// Verify JSON contains LogLevel field (PascalCase for web UI compatibility)
	jsonStr := string(jsonData)
	if !strings.Contains(jsonStr, "LogLevel") {
		t.Errorf("JSON output missing 'LogLevel' field. Got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, "silent") {
		t.Errorf("JSON output missing 'silent' value. Got: %s", jsonStr)
	}

	// Unmarshal back to struct
	var decoded DatabaseConfig
	if err := json.Unmarshal(jsonData, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal DatabaseConfig: %v", err)
	}

	// Verify all fields were preserved
	if decoded.Type != dbConfig.Type {
		t.Errorf("Type mismatch: got %s, want %s", decoded.Type, dbConfig.Type)
	}
	if decoded.DSN != dbConfig.DSN {
		t.Errorf("DSN mismatch: got %s, want %s", decoded.DSN, dbConfig.DSN)
	}
	if decoded.LogLevel != dbConfig.LogLevel {
		t.Errorf("LogLevel mismatch: got %s, want %s", decoded.LogLevel, dbConfig.LogLevel)
	}
}
