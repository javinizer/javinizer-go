package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func getMigration(fromVersion int) (Migration, bool) {
	m, ok := migrations[fromVersion]
	return m, ok
}

func resetMigrations() {
	migrations = make(map[int]Migration)
	migrationContext = MigrationContext{}
}

// restoreRealMigrations re-registers the production LegacyMigration.
// Must be called in deferred cleanup after resetMigrations() in tests
// that need a clean slate, so that subsequent tests aren't broken.
func restoreRealMigrations() {
	RegisterMigration(NewLegacyMigration())
}

func TestRegisterMigration(t *testing.T) {
	resetMigrations()
	defer restoreRealMigrations()

	mock := &mockMigration{
		fromVersions: []int{0, 1, 2},
		toVersion:    3,
		description:  "test migration",
	}

	RegisterMigration(mock)

	m, exists := getMigration(0)
	assert.True(t, exists)
	assert.Equal(t, 3, m.ToVersion())

	m, exists = getMigration(1)
	assert.True(t, exists)

	m, exists = getMigration(2)
	assert.True(t, exists)
}

func TestMigrateToCurrent(t *testing.T) {
	resetMigrations()
	defer restoreRealMigrations()

	mock := &mockMigration{
		fromVersions: []int{0, 1, 2},
		toVersion:    3,
		description:  "test",
		migrateFn: func(cfg *Config) error {
			cfg.Server.Port = 9999
			return nil
		},
	}
	RegisterMigration(mock)

	cfg := &Config{ConfigVersion: 0}
	err := MigrateToCurrent(cfg)

	assert.NoError(t, err)
	assert.Equal(t, 3, cfg.ConfigVersion)
	assert.Equal(t, 9999, cfg.Server.Port)
}

func TestMigrateToCurrent_NoMigration(t *testing.T) {
	resetMigrations()
	defer restoreRealMigrations()

	cfg := &Config{ConfigVersion: 5}
	err := MigrateToCurrent(cfg)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no migration from version 5")
}

func TestMigrateToCurrent_AlreadyCurrent(t *testing.T) {
	resetMigrations()
	defer restoreRealMigrations()

	cfg := &Config{ConfigVersion: CurrentConfigVersion}
	err := MigrateToCurrent(cfg)

	assert.NoError(t, err)
}

type mockMigration struct {
	fromVersions []int
	toVersion    int
	description  string
	migrateFn    func(*Config) error
}

func (m *mockMigration) FromVersions() []int { return m.fromVersions }
func (m *mockMigration) ToVersion() int      { return m.toVersion }
func (m *mockMigration) Description() string { return m.description }
func (m *mockMigration) Migrate(cfg *Config) error {
	if m.migrateFn != nil {
		return m.migrateFn(cfg)
	}
	return nil
}
