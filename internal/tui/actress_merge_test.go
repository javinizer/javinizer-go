package tui

import (
	"context"
	"errors"
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTUI_BrowserKeyOpenActressMergeModal(t *testing.T) {
	model := New(TUIModelConfig{})
	model.SetActressRepo(&database.ActressRepository{})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	got := updated.(*Model)

	assert.True(t, got.actressMergeCtl.modal.showing)
	assert.Equal(t, actressMergeStepInput, got.actressMergeCtl.modal.step)
	assert.Equal(t, 0, got.actressMergeCtl.modal.focus)
}

func TestTUI_BrowserKeyOpenActressMergeModalWithoutRepo(t *testing.T) {
	model := New(TUIModelConfig{})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	got := updated.(*Model)

	assert.False(t, got.actressMergeCtl.modal.showing)
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

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = db.Close()
	})
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
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
	require.NoError(t, repo.Create(context.TODO(), target))
	require.NoError(t, repo.Create(context.TODO(), source))

	model := New(TUIModelConfig{})
	model.SetActressRepo(repo)
	model.actressMergeCtl.Open()
	model.actressMergeCtl.modal.targetInput.SetValue(strconv.FormatUint(uint64(target.ID), 10))
	model.actressMergeCtl.modal.sourceInput.SetValue(strconv.FormatUint(uint64(source.ID), 10))

	require.NoError(t, model.actressMergeCtl.LoadPreview())
	require.Equal(t, actressMergeStepConflict, model.actressMergeCtl.modal.step)
	require.NotNil(t, model.actressMergeCtl.modal.preview)
	require.NotEmpty(t, model.actressMergeCtl.modal.preview.Conflicts)

	// Select a deterministic conflict field and choose "source".
	firstNameConflictIdx := -1
	for i, conflict := range model.actressMergeCtl.modal.preview.Conflicts {
		if conflict.Field == "first_name" {
			firstNameConflictIdx = i
			break
		}
	}
	require.NotEqual(t, -1, firstNameConflictIdx)
	model.actressMergeCtl.modal.conflictCursor = firstNameConflictIdx

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model = updated.(*Model)
	assert.Equal(t, "source", model.actressMergeCtl.modal.resolutions["first_name"])

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = updated.(*Model)
	// Merge is now async (tea.Cmd): execute the returned cmd and feed the
	// result message back through Update so the modal applies it.
	if cmd != nil {
		mergeMsg := cmd()
		updated, _ = model.Update(mergeMsg)
		model = updated.(*Model)
	}
	assert.Equal(t, actressMergeStepResult, model.actressMergeCtl.modal.step)
	require.NotNil(t, model.actressMergeCtl.modal.result)

	merged, err := repo.FindByID(context.TODO(), target.ID)
	require.NoError(t, err)
	assert.Equal(t, "Source", merged.FirstName)

	_, err = repo.FindByID(context.TODO(), source.ID)
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
