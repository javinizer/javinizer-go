package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestAppendMissingStringsEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		existing    []string
		defaults    []string
		want        []string
		wantChanged bool
	}{
		{
			name:        "empty existing and defaults",
			existing:    []string{},
			defaults:    []string{},
			want:        []string{},
			wantChanged: false,
		},
		{
			name:        "preserves existing duplicates and appends missing defaults once",
			existing:    []string{"dmm", "dmm"},
			defaults:    []string{"dmm", "r18dev"},
			want:        []string{"dmm", "dmm", "r18dev"},
			wantChanged: true,
		},
		{
			name:        "already contains all defaults",
			existing:    []string{"r18dev", "dmm"},
			defaults:    []string{"r18dev", "dmm"},
			want:        []string{"r18dev", "dmm"},
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed := appendMissingStrings(tt.existing, tt.defaults)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantChanged, changed)
		})
	}
}

func TestApplyCompatibilityRules_NilConfig(t *testing.T) {
	changed, err := applyCompatibilityRules(nil)
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestApplyCompatibilityRules_LegacyConfigGetsNormalized(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ConfigVersion = 0
	cfg.Scrapers.Priority = nil
	cfg.System.UpdateCheckIntervalHours = 0

	changed, err := applyCompatibilityRules(cfg)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)
	assert.Equal(t, legacyScraperPriorityBaseline(), cfg.Scrapers.Priority)
	assert.Equal(t, 24, cfg.System.UpdateCheckIntervalHours)
}

func TestApplyCompatibilityRules_CurrentVersionNoChange(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ConfigVersion = CurrentConfigVersion
	cfg.Scrapers.Priority = []string{"dmm"}
	cfg.System.UpdateCheckIntervalHours = 12

	beforePriority := append([]string{}, cfg.Scrapers.Priority...)
	beforeInterval := cfg.System.UpdateCheckIntervalHours

	changed, err := applyCompatibilityRules(cfg)
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, beforePriority, cfg.Scrapers.Priority)
	assert.Equal(t, beforeInterval, cfg.System.UpdateCheckIntervalHours)
}

func TestApplyCompatibilityRules_NewerVersionReturnsErrorWithoutMutation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ConfigVersion = CurrentConfigVersion + 1
	cfg.Scrapers.Priority = []string{"dmm"}

	beforeVersion := cfg.ConfigVersion
	beforePriority := append([]string{}, cfg.Scrapers.Priority...)

	changed, err := applyCompatibilityRules(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer than supported version")
	assert.False(t, changed)
	assert.Equal(t, beforeVersion, cfg.ConfigVersion)
	assert.Equal(t, beforePriority, cfg.Scrapers.Priority)
}

func TestNormalize_Idempotent(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Database.Type = " SQLITE "
	cfg.Scrapers.R18Dev.Language = ""
	cfg.Scrapers.JavLibrary.Language = " JA "
	cfg.Scrapers.Referer = ""
	cfg.Metadata.Translation.Provider = " OpenAI "
	cfg.Metadata.Translation.TimeoutSeconds = 0

	changed := Normalize(cfg)
	require.True(t, changed)
	assert.Equal(t, "sqlite", cfg.Database.Type)
	assert.Equal(t, "en", cfg.Scrapers.R18Dev.Language)
	assert.Equal(t, "ja", cfg.Scrapers.JavLibrary.Language)
	assert.Equal(t, "https://www.dmm.co.jp/", cfg.Scrapers.Referer)
	assert.Equal(t, "openai", cfg.Metadata.Translation.Provider)
	assert.Equal(t, 60, cfg.Metadata.Translation.TimeoutSeconds)

	changed = Normalize(cfg)
	assert.False(t, changed)
}

func TestPrepare_RunsCompatibilityNormalizeAndValidate(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ConfigVersion = 0
	cfg.Scrapers.Priority = []string{"dmm"}
	cfg.Database.Type = " SQLITE "
	cfg.Scrapers.Referer = ""

	changed, err := Prepare(cfg)
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)
	assert.Equal(t, "sqlite", cfg.Database.Type)
	assert.Equal(t, "https://www.dmm.co.jp/", cfg.Scrapers.Referer)
	assert.Contains(t, cfg.Scrapers.Priority, "r18dev")
	assert.Contains(t, cfg.Scrapers.Priority, "fc2")
}

func TestValidate_DoesNotMutateConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Database.Type = " SQLITE "
	cfg.Scrapers.R18Dev.Language = ""
	cfg.Scrapers.Referer = ""

	before := *cfg
	before.Scrapers.Priority = append([]string{}, cfg.Scrapers.Priority...)

	require.NoError(t, cfg.Validate())
	assert.Equal(t, before.Database.Type, cfg.Database.Type)
	assert.Equal(t, before.Scrapers.R18Dev.Language, cfg.Scrapers.R18Dev.Language)
	assert.Equal(t, before.Scrapers.Referer, cfg.Scrapers.Referer)
}

func TestSave_NoOpWhenContentUnchanged(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	cfg.Server.Port = 9099

	require.NoError(t, Save(cfg, cfgPath))
	firstInfo, err := os.Stat(cfgPath)
	require.NoError(t, err)

	// Ensure the filesystem mtime granularity can observe rewrites.
	time.Sleep(1100 * time.Millisecond)

	require.NoError(t, Save(cfg, cfgPath))
	secondInfo, err := os.Stat(cfgPath)
	require.NoError(t, err)

	assert.Equal(t, firstInfo.ModTime(), secondInfo.ModTime())
}

func TestSave_RewritesWhenContentChanges(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig()
	cfg.Server.Port = 9099

	require.NoError(t, Save(cfg, cfgPath))
	firstInfo, err := os.Stat(cfgPath)
	require.NoError(t, err)

	time.Sleep(1100 * time.Millisecond)

	cfg.Server.Port = 9100
	require.NoError(t, Save(cfg, cfgPath))

	secondInfo, err := os.Stat(cfgPath)
	require.NoError(t, err)
	assert.True(t, secondInfo.ModTime().After(firstInfo.ModTime()))

	loaded, err := Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, 9100, loaded.Server.Port)
}

func TestSave_ConcurrentWritersProduceValidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	const writers = 8
	var wg sync.WaitGroup
	wg.Add(writers)
	errCh := make(chan error, writers)

	for i := 0; i < writers; i++ {
		i := i
		go func() {
			defer wg.Done()
			cfg := DefaultConfig()
			cfg.Server.Port = 9000 + i
			cfg.Logging.Level = "debug"
			if err := Save(cfg, cfgPath); err != nil {
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}

	raw, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.False(t, bytes.Contains(raw, []byte("\x00")))

	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	require.Equal(t, yaml.DocumentNode, doc.Kind)

	loaded, err := Load(cfgPath)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, loaded.Server.Port, 9000)
	assert.Less(t, loaded.Server.Port, 9000+writers)
	assert.Equal(t, "debug", loaded.Logging.Level)

	_, err = os.Stat(cfgPath + ".lock")
	assert.True(t, os.IsNotExist(err))
}

func TestReleaseConfigFileLock_DoesNotRemoveForeignLock(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	lockPath := cfgPath + ".lock"

	unlock, err := acquireConfigFileLock(cfgPath)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(lockPath, []byte("foreign-token"), 0600))

	unlock()

	data, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	assert.Equal(t, "foreign-token", string(data))
}

func TestReplaceFileOnWindows_RestoresBackupWhenReplaceFails(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "config.yaml")
	missingTempPath := filepath.Join(tmpDir, "missing-tmp.yaml")

	require.NoError(t, os.WriteFile(targetPath, []byte("old-config"), 0644))

	err := replaceFileOnWindows(targetPath, missingTempPath)
	require.Error(t, err)

	data, readErr := os.ReadFile(targetPath)
	require.NoError(t, readErr)
	assert.Equal(t, "old-config", string(data))
}

func TestParseConfigLockMetadata(t *testing.T) {
	tests := []struct {
		name    string
		content string
		ok      bool
	}{
		{
			name:    "current format",
			content: "pid=123,time=456",
			ok:      true,
		},
		{
			name:    "legacy newline format",
			content: "pid=123\ntime=456\n",
			ok:      true,
		},
		{
			name:    "invalid format",
			content: "token-only",
			ok:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, ok := parseConfigLockMetadata(tt.content)
			assert.Equal(t, tt.ok, ok)
		})
	}
}

func TestShouldReapConfigLock(t *testing.T) {
	now := time.Now()
	staleModTime := now.Add(-configLockStaleAge - time.Minute)

	staleMalformed := []byte("corrupt-lock")
	assert.True(t, shouldReapConfigLock(staleMalformed, staleModTime, now))

	recentMalformed := []byte("corrupt-lock")
	assert.False(t, shouldReapConfigLock(recentMalformed, now, now))

	if runtime.GOOS == "windows" {
		// Parseable stale locks are reaped by age on Windows (except current process PID).
		parseableDeadPID := []byte(fmt.Sprintf(
			"pid=%d,time=%d",
			os.Getpid()+1000000,
			now.Add(-configLockStaleAge-time.Minute).UnixNano(),
		))
		assert.True(t, shouldReapConfigLock(parseableDeadPID, staleModTime, now))

		parseableCurrentPID := []byte(fmt.Sprintf(
			"pid=%d,time=%d",
			os.Getpid(),
			now.Add(-configLockStaleAge-time.Minute).UnixNano(),
		))
		assert.False(t, shouldReapConfigLock(parseableCurrentPID, staleModTime, now))
		return
	}

	deadPIDContent := []byte(fmt.Sprintf(
		"pid=%d,time=%d",
		os.Getpid()+1000000,
		now.Add(-configLockStaleAge-time.Minute).UnixNano(),
	))
	assert.True(t, shouldReapConfigLock(deadPIDContent, staleModTime, now))

	alivePIDContent := []byte(fmt.Sprintf(
		"pid=%d,time=%d",
		os.Getpid(),
		now.Add(-configLockStaleAge-time.Minute).UnixNano(),
	))
	assert.False(t, shouldReapConfigLock(alivePIDContent, staleModTime, now))

	recentContent := []byte(fmt.Sprintf(
		"pid=%d,time=%d",
		os.Getpid()+1000000,
		now.Add(-time.Second).UnixNano(),
	))
	assert.False(t, shouldReapConfigLock(recentContent, now, now))
}

func TestAcquireConfigFileLock_ReapsStaleMalformedLock(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	lockPath := cfgPath + ".lock"

	require.NoError(t, os.WriteFile(lockPath, []byte("corrupt"), 0600))
	stale := time.Now().Add(-configLockStaleAge - time.Minute)
	require.NoError(t, os.Chtimes(lockPath, stale, stale))

	unlock, err := acquireConfigFileLock(cfgPath)
	require.NoError(t, err)
	unlock()
}
