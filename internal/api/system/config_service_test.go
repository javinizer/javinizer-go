package system

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigUpdateService_ValidateAndApply_PreserveRedactedSecrets(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	oldCfg.Database.DSN = "real-dsn-value"
	oldCfg.Metadata.Translation.OpenAI.APIKey = "real-openai-key"
	oldCfg.Metadata.Translation.DeepL.APIKey = "real-deepl-key"
	oldCfg.Metadata.Translation.Google.APIKey = "real-google-key"
	oldCfg.Metadata.Translation.OpenAICompatible.APIKey = "real-oai-key"
	oldCfg.Metadata.Translation.Anthropic.APIKey = "real-anthropic-key"

	newCfg := config.DefaultConfig(nil, nil)
	// Simulate what the frontend sends back after GET /config redacts values
	newCfg.Database.DSN = models.RedactedValue
	newCfg.Metadata.Translation.OpenAI.APIKey = models.RedactedValue
	newCfg.Metadata.Translation.DeepL.APIKey = models.RedactedValue
	newCfg.Metadata.Translation.Google.APIKey = models.RedactedValue
	newCfg.Metadata.Translation.OpenAICompatible.APIKey = models.RedactedValue
	newCfg.Metadata.Translation.Anthropic.APIKey = models.RedactedValue

	tempConfigFile := t.TempDir() + "/config.yaml"
	deps := createTestDeps(t, oldCfg, tempConfigFile)
	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.NoError(t, err)

	// Verify redacted values were preserved from old config
	savedCfg := deps.CoreDeps.GetConfig()
	assert.Equal(t, "real-dsn-value", savedCfg.Database.DSN)
	assert.Equal(t, "real-openai-key", savedCfg.Metadata.Translation.OpenAI.APIKey)
	assert.Equal(t, "real-deepl-key", savedCfg.Metadata.Translation.DeepL.APIKey)
	assert.Equal(t, "real-google-key", savedCfg.Metadata.Translation.Google.APIKey)
	assert.Equal(t, "real-oai-key", savedCfg.Metadata.Translation.OpenAICompatible.APIKey)
	assert.Equal(t, "real-anthropic-key", savedCfg.Metadata.Translation.Anthropic.APIKey)
}

func TestConfigUpdateService_ValidateAndApply_ValidationError(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)

	newCfg := config.DefaultConfig(nil, nil)
	newCfg.Metadata.Translation.Enabled = true
	newCfg.Metadata.Translation.Provider = "deepl"
	newCfg.Metadata.Translation.DeepL.APIKey = ""

	tempConfigFile := t.TempDir() + "/config.yaml"
	deps := createTestDeps(t, oldCfg, tempConfigFile)
	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.Error(t, err)

	// Should be a validationError (maps to 400)
	var valErr *validationError
	assert.ErrorAs(t, err, &valErr)
	assert.Contains(t, err.Error(), "deepl.api_key is required")
}

func TestConfigUpdateService_ValidateAndApply_NewerConfigVersion(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)

	newCfg := config.DefaultConfig(nil, nil)
	newCfg.ConfigVersion = config.CurrentConfigVersion + 1

	tempConfigFile := t.TempDir() + "/config.yaml"
	deps := createTestDeps(t, oldCfg, tempConfigFile)
	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.Error(t, err)

	// Prepare rejects newer config versions — this is a validationError
	var valErr *validationError
	assert.ErrorAs(t, err, &valErr)
	assert.Contains(t, err.Error(), "newer than supported version")
}

func TestConfigUpdateService_ValidateAndApply_Success(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)

	newCfg := config.DefaultConfig(nil, nil)
	newCfg.Server.Host = "0.0.0.0"
	newCfg.Server.Port = 9090

	tempConfigFile := t.TempDir() + "/config.yaml"
	deps := createTestDeps(t, oldCfg, tempConfigFile)
	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.NoError(t, err)

	// Verify the new config was applied
	savedCfg := deps.CoreDeps.GetConfig()
	assert.Equal(t, "0.0.0.0", savedCfg.Server.Host)
	assert.Equal(t, 9090, savedCfg.Server.Port)
}

func TestMapConfigErrorToHTTP(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		expectedCode int
		expectedMsg  string
	}{
		{
			name:         "validation error maps to 400",
			err:          &validationError{message: "bad config"},
			expectedCode: 400,
			expectedMsg:  "bad config",
		},
		{
			name:         "persist error maps to 500",
			err:          &persistError{message: "disk full"},
			expectedCode: 500,
			expectedMsg:  "disk full",
		},
		{
			name:         "reload error maps to 500",
			err:          &reloadError{originalErr: errors.New("boom"), message: "reload failed"},
			expectedCode: 500,
			expectedMsg:  "reload failed",
		},
		{
			name:         "rollback error maps to 500",
			err:          &rollbackError{rollbackErr: errors.New("rb"), originalErr: errors.New("orig"), message: "critical"},
			expectedCode: 500,
			expectedMsg:  "critical",
		},
		{
			name:         "nil error returns 0",
			err:          nil,
			expectedCode: 0,
			expectedMsg:  "",
		},
		{
			name:         "unknown error maps to 500",
			err:          errors.New("something unexpected"),
			expectedCode: 500,
			expectedMsg:  "something unexpected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, msg := mapConfigErrorToHTTP(tt.err)
			assert.Equal(t, tt.expectedCode, code)
			assert.Equal(t, tt.expectedMsg, msg)
		})
	}
}

func TestConfigUpdateService_PreserveRedactedProxyProfiles(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	oldCfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
		"main": {Username: "real-user", Password: "real-pass", URL: "http://proxy.example.com:8080"},
	}

	newCfg := config.DefaultConfig(nil, nil)
	newCfg.Scrapers.Proxy.Profiles = map[string]models.ProxyProfile{
		"main": {Username: models.RedactedValue, Password: models.RedactedValue, URL: "http://proxy.example.com:8080"},
	}

	tempConfigFile := t.TempDir() + "/config.yaml"
	deps := createTestDeps(t, oldCfg, tempConfigFile)
	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.NoError(t, err)

	savedCfg := deps.CoreDeps.GetConfig()
	assert.Equal(t, "real-user", savedCfg.Scrapers.Proxy.Profiles["main"].Username)
	assert.Equal(t, "real-pass", savedCfg.Scrapers.Proxy.Profiles["main"].Password)
}

func TestConfigUpdateService_PreserveRedactedScraperOverrideAPIKey(t *testing.T) {
	oldCfg := config.DefaultConfig(nil, nil)
	oldCfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"javstash": {APIKey: "real-javstash-key"},
	}

	newCfg := config.DefaultConfig(nil, nil)
	newCfg.Scrapers.Overrides = map[string]*models.ScraperSettings{
		"javstash": {APIKey: models.RedactedValue},
	}

	tempConfigFile := t.TempDir() + "/config.yaml"
	deps := createTestDeps(t, oldCfg, tempConfigFile)
	svc := NewConfigUpdateService(testkit.GetTestRuntime(deps), tempConfigFile)

	err := svc.ValidateAndApply(oldCfg, newCfg, nil)
	require.NoError(t, err)

	savedCfg := deps.CoreDeps.GetConfig()
	assert.Equal(t, "real-javstash-key", savedCfg.Scrapers.Overrides["javstash"].APIKey)
}
