package scrape_test

import (
	"bytes"
	"testing"

	"github.com/javinizer/javinizer-go/cmd/javinizer/commands/scrape"
	"github.com/stretchr/testify/assert"
)

// Tests

func TestScrapeCommand_Structure(t *testing.T) {
	cmd := scrape.NewCommand()

	assert.Equal(t, "scrape [id]", cmd.Use)
	assert.Contains(t, cmd.Short, "Scrape metadata")
	assert.NotNil(t, cmd.RunE, "RunE should be set")

	// Verify command has Args validation set (can't compare functions directly)
	assert.NotNil(t, cmd.Args, "Args validation should be set")
}

func TestScrapeCommand_Flags(t *testing.T) {
	cmd := scrape.NewCommand()

	tests := []struct {
		name         string
		flag         string
		shorthand    string
		expectedType string
		hasDefault   bool
	}{
		{"force", "force", "f", "bool", true},
		{"scrapers", "scrapers", "s", "stringSlice", false},
		{"scrape-actress", "scrape-actress", "", "bool", false},
		{"no-scrape-actress", "no-scrape-actress", "", "bool", false},
		{"browser", "browser", "", "bool", false},
		{"no-browser", "no-browser", "", "bool", false},
		{"browser-timeout", "browser-timeout", "", "int", false},
		{"actress-db", "actress-db", "", "bool", false},
		{"no-actress-db", "no-actress-db", "", "bool", false},
		{"genre-replacement", "genre-replacement", "", "bool", false},
		{"no-genre-replacement", "no-genre-replacement", "", "bool", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag := cmd.Flags().Lookup(tt.flag)
			assert.NotNil(t, flag, "Flag %s should exist", tt.flag)

			if tt.shorthand != "" {
				assert.Equal(t, tt.shorthand, flag.Shorthand, "Flag %s shorthand mismatch", tt.flag)
			}
		})
	}
}

func TestScrapeCommand_FlagDefaults(t *testing.T) {
	cmd := scrape.NewCommand()

	// Test default values
	forceFlag := cmd.Flags().Lookup("force")
	assert.Equal(t, "false", forceFlag.DefValue, "force should default to false")

	browserTimeoutFlag := cmd.Flags().Lookup("browser-timeout")
	assert.Equal(t, "0", browserTimeoutFlag.DefValue, "browser-timeout should default to 0")
}

func TestScrapeCommand_HelpText(t *testing.T) {
	cmd := scrape.NewCommand()

	// Capture help output
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})

	_ = cmd.Execute()

	output := buf.String()

	// Verify help text contains key information
	assert.Contains(t, output, "scrape", "Help should mention command name")
	assert.Contains(t, output, "Flags:", "Help should show flags section")
	assert.Contains(t, output, "--force", "Help should document --force flag")
	assert.Contains(t, output, "--scrapers", "Help should document --scrapers flag")
}

func TestScrapeCommand_FlagParsing(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		checkFlag   string
		expectedVal string
	}{
		{
			name:        "force flag short form",
			args:        []string{"-f", "TEST-001"},
			checkFlag:   "force",
			expectedVal: "true",
		},
		{
			name:        "force flag long form",
			args:        []string{"--force", "TEST-001"},
			checkFlag:   "force",
			expectedVal: "true",
		},
		{
			name:        "scrapers flag",
			args:        []string{"--scrapers", "dmm,r18dev", "TEST-001"},
			checkFlag:   "scrapers",
			expectedVal: "[dmm,r18dev]",
		},
		{
			name:        "scrapers flag short form",
			args:        []string{"-s", "dmm", "TEST-001"},
			checkFlag:   "scrapers",
			expectedVal: "[dmm]",
		},
		{
			name:        "browser timeout",
			args:        []string{"--browser-timeout", "30", "TEST-001"},
			checkFlag:   "browser-timeout",
			expectedVal: "30",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := scrape.NewCommand()
			cmd.SetArgs(tt.args)

			// Don't actually execute, just parse flags
			err := cmd.ParseFlags(tt.args)
			assert.NoError(t, err, "Flag parsing should succeed")

			flag := cmd.Flags().Lookup(tt.checkFlag)
			assert.NotNil(t, flag, "Flag %s should exist", tt.checkFlag)
			assert.Equal(t, tt.expectedVal, flag.Value.String(), "Flag value mismatch")
		})
	}
}

func TestScrapeCommand_MutuallyExclusiveFlags(t *testing.T) {
	// Test that mutually exclusive flags can both be defined
	// (the actual mutual exclusion is enforced in the command logic, not by cobra)
	cmd := scrape.NewCommand()

	tests := []struct {
		name  string
		flag1 string
		flag2 string
	}{
		{"scrape-actress flags", "scrape-actress", "no-scrape-actress"},
		{"browser flags", "browser", "no-browser"},
		{"actress-db flags", "actress-db", "no-actress-db"},
		{"genre-replacement flags", "genre-replacement", "no-genre-replacement"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flag1 := cmd.Flags().Lookup(tt.flag1)
			flag2 := cmd.Flags().Lookup(tt.flag2)

			assert.NotNil(t, flag1, "Flag %s should exist", tt.flag1)
			assert.NotNil(t, flag2, "Flag %s should exist", tt.flag2)
		})
	}
}

func TestScrapeCommand_RequiresArgument(t *testing.T) {
	cmd := scrape.NewCommand()

	// Command should have Args validation set
	assert.NotNil(t, cmd.Args, "Args validation should be set")

	// Test that command fails without argument
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err, "Command should fail without ID argument")
	assert.Contains(t, err.Error(), "accepts 1 arg(s), received 0")
}

func TestScrapeCommand_Integration_CachedMovie(t *testing.T) {
	t.Skip("Skipping integration test - requires full command context with root command")

	// Note: This test is skipped because the scrape command needs to be run
	// within the context of the root command to have access to the --config flag.
	// The business logic for caching is tested in internal/commandutil/scraping_test.go
	// and this command-level test would require significant test infrastructure.
}

func TestScrapeCommand_Integration_ForceRefresh(t *testing.T) {
	t.Skip("Skipping integration test - requires full command context with root command")

	// Note: This test is skipped because the scrape command needs to be run
	// within the context of the root command to have access to the --config flag.
	// The business logic for force refresh is tested in internal/commandutil/scraping_test.go
}

func TestScrapeCommand_CanBeInstantiated(t *testing.T) {
	cmd := scrape.NewCommand()

	// Verify command can be created without errors
	assert.NotNil(t, cmd)
	assert.Equal(t, "scrape [id]", cmd.Use)
	assert.NotNil(t, cmd.RunE, "RunE should be set")
}
