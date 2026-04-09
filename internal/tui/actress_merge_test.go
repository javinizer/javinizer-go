package tui

import (
	"errors"
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTUI_BrowserKeyOpenActressMergeModal(t *testing.T) {
	_, cfg := testutil.CreateTestConfig(t, nil)
	model := New(cfg)
	model.SetActressRepo(&database.ActressRepository{})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	got := updated.(*Model)

	assert.True(t, got.showingActressMerge)
	assert.Equal(t, actressMergeStepInput, got.actressMergeStep)
	assert.Equal(t, 0, got.actressMergeFocus)
}

func TestTUI_BrowserKeyOpenActressMergeModalWithoutRepo(t *testing.T) {
	_, cfg := testutil.CreateTestConfig(t, nil)
	model := New(cfg)

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	got := updated.(*Model)

	assert.False(t, got.showingActressMerge)
}

func newActressRepoForTUIMergeTest(t *testing.T) *database.ActressRepository {
	t.Helper()

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := database.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})
	require.NoError(t, db.AutoMigrate())
	return database.NewActressRepository(db)
}

func TestTUI_ActressMergeConflictSelectionAndApply(t *testing.T) {
	repo := newActressRepoForTUIMergeTest(t)

	target := &models.Actress{
		DMMID:        74001,
		FirstName:    "Target",
		LastName:     "Actress",
		JapaneseName: "ターゲット",
	}
	source := &models.Actress{
		DMMID:        74002,
		FirstName:    "Source",
		LastName:     "Actress",
		JapaneseName: "ソース",
	}
	require.NoError(t, repo.Create(target))
	require.NoError(t, repo.Create(source))

	_, cfg := testutil.CreateTestConfig(t, nil)
	model := New(cfg)
	model.SetActressRepo(repo)
	model.openActressMergeModal()
	model.actressMergeTargetInput.SetValue(strconv.FormatUint(uint64(target.ID), 10))
	model.actressMergeSourceInput.SetValue(strconv.FormatUint(uint64(source.ID), 10))

	require.NoError(t, model.loadActressMergePreview())
	require.Equal(t, actressMergeStepConflict, model.actressMergeStep)
	require.NotNil(t, model.actressMergePreview)
	require.NotEmpty(t, model.actressMergePreview.Conflicts)

	// Select a deterministic conflict field and choose "source".
	firstNameConflictIdx := -1
	for i, conflict := range model.actressMergePreview.Conflicts {
		if conflict.Field == "first_name" {
			firstNameConflictIdx = i
			break
		}
	}
	require.NotEqual(t, -1, firstNameConflictIdx)
	model.actressMergeConflictCursor = firstNameConflictIdx

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model = updated.(*Model)
	assert.Equal(t, "source", model.actressMergeResolutions["first_name"])

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	assert.Equal(t, actressMergeStepResult, model.actressMergeStep)
	require.NotNil(t, model.actressMergeResult)

	merged, err := repo.FindByID(target.ID)
	require.NoError(t, err)
	assert.Equal(t, "Source", merged.FirstName)

	_, err = repo.FindByID(source.ID)
	require.Error(t, err)
}

// TestParseActressMergeID tests the parseActressMergeID pure function
func TestParseActressMergeID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedID  uint
		expectError bool
	}{
		{
			name:        "valid ID",
			input:       "123",
			expectedID:  123,
			expectError: false,
		},
		{
			name:        "valid ID with whitespace",
			input:       "  456  ",
			expectedID:  456,
			expectError: false,
		},
		{
			name:        "zero ID is invalid",
			input:       "0",
			expectedID:  0,
			expectError: true,
		},
		{
			name:        "negative ID is invalid",
			input:       "-1",
			expectedID:  0,
			expectError: true,
		},
		{
			name:        "empty string is invalid",
			input:       "",
			expectedID:  0,
			expectError: true,
		},
		{
			name:        "whitespace only is invalid",
			input:       "   ",
			expectedID:  0,
			expectError: true,
		},
		{
			name:        "non-numeric is invalid",
			input:       "abc",
			expectedID:  0,
			expectError: true,
		},
		{
			name:        "large valid ID",
			input:       "999999",
			expectedID:  999999,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := parseActressMergeID(tt.input)
			if tt.expectError {
				assert.Error(t, err)
				assert.Equal(t, uint(0), id)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedID, id)
			}
		})
	}
}

// TestFormatConflictValue tests the formatConflictValue pure function
func TestFormatConflictValue(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "nil value",
			input:    nil,
			expected: "(empty)",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "(empty)",
		},
		{
			name:     "whitespace only string",
			input:    "   ",
			expected: "(empty)",
		},
		{
			name:     "non-empty string",
			input:    "Test Value",
			expected: "Test Value",
		},
		{
			name:     "integer value",
			input:    123,
			expected: "123",
		},
		{
			name:     "float value",
			input:    3.14,
			expected: "3.14",
		},
		{
			name:     "boolean value",
			input:    true,
			expected: "true",
		},
		{
			name:     "string with content",
			input:    "日本語名",
			expected: "日本語名",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatConflictValue(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestNormalizeActressMergeError tests the normalizeActressMergeError pure function
func TestNormalizeActressMergeError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		expectedMsg string
	}{
		{
			name:        "nil error",
			err:         nil,
			expectedMsg: "",
		},
		{
			name:        "same ID error",
			err:         database.ErrActressMergeSameID,
			expectedMsg: "target and source must be different actress IDs",
		},
		{
			name:        "invalid ID error",
			err:         database.ErrActressMergeInvalidID,
			expectedMsg: "target and source must be positive actress IDs",
		},
		{
			name:        "generic error",
			err:         errors.New("database connection failed"),
			expectedMsg: "database connection failed",
		},
		{
			name:        "wrapped error",
			err:         errors.Join(database.ErrActressMergeSameID, errors.New("additional context")),
			expectedMsg: "target and source must be different actress IDs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeActressMergeError(tt.err)
			assert.Equal(t, tt.expectedMsg, result)
		})
	}
}
