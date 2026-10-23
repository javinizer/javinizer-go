package tui

import (
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestTUIMergePolicyPreviewAndReset(t *testing.T) {
	repo := newActressRepoForTUIMergeTest(t)
	target := models.Actress{FirstName: "桜"}
	source := models.Actress{FirstName: "Source", LastName: "Example", DMMID: 881}
	require.NoError(t, repo.Create(t.Context(), &target))
	require.NoError(t, repo.Create(t.Context(), &source))
	model := New(TUIModelConfig{})
	model.SetActressRepo(repo)
	am := &model.actressMergeCtl.modal
	am.open()
	am.targetInput.SetValue(strconv.Itoa(int(target.ID)))
	am.sourceInput.SetValue(strconv.Itoa(int(source.ID)))
	_, cmd := am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	require.Nil(t, cmd)
	require.Equal(t, database.MergePolicyPreserveTarget, am.policy)
	require.NoError(t, am.loadPreview())
	require.Len(t, am.previewFields(), 5)
	require.Empty(t, am.resolutions)
	require.Empty(t, am.preview.ProposedMerged.LastName)
	require.Zero(t, am.preview.ProposedMerged.DMMID)
	am.conflictCursor = 2 // last_name
	require.Equal(t, "last_name", am.currentConflict().Field)
	require.Contains(t, am.View(), "(empty)")
	am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	require.Equal(t, "source", am.resolutions["last_name"])
	am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	require.Empty(t, am.resolutions)
	_, cmd = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	require.NotNil(t, cmd)
	require.True(t, am.busy)
	// A second action cannot execute against a stale preview during policy change.
	_, ignored := am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Nil(t, ignored)
	am.Update(cmd())
	require.False(t, am.busy)
	require.Equal(t, database.MergePolicyFillEmpty, am.policy)
	require.Equal(t, "Example", am.preview.ProposedMerged.LastName)
	require.Empty(t, am.resolutions)
	_, cmd = am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	msg := cmd()
	am.close()
	am.open()
	am.Update(msg) // dismissed preview must not revive the previous dialog
	require.Nil(t, am.preview)
	require.Empty(t, am.policy)
	require.Empty(t, am.resolutions)
	require.Equal(t, actressMergeStepInput, am.step)
}

func TestTUIPreserveMergeExecutesBlankDecision(t *testing.T) {
	repo := newActressRepoForTUIMergeTest(t)
	target := models.Actress{FirstName: "桜"}
	source := models.Actress{FirstName: "Source", LastName: "Example", DMMID: 882}
	require.NoError(t, repo.Create(t.Context(), &target))
	require.NoError(t, repo.Create(t.Context(), &source))
	model := New(TUIModelConfig{})
	model.SetActressRepo(repo)
	am := &model.actressMergeCtl.modal
	am.open()
	am.policy = database.MergePolicyPreserveTarget
	am.targetInput.SetValue(strconv.Itoa(int(target.ID)))
	am.sourceInput.SetValue(strconv.Itoa(int(source.ID)))
	require.NoError(t, am.loadPreview())
	msg := am.applyCmd()()
	am.Update(msg)
	require.NotNil(t, am.result)
	require.Empty(t, am.result.MergedActress.LastName)
	require.Zero(t, am.result.MergedActress.DMMID)
	am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	require.Empty(t, am.policy)
	require.Empty(t, am.resolutions)
	require.Nil(t, am.preview)
}

func TestTUIMergeFieldPreviewNavigation(t *testing.T) {
	model := New(TUIModelConfig{})
	am := &model.actressMergeCtl.modal
	require.Nil(t, am.previewFields())
	am.preview = &database.ActressMergePreview{Conflicts: []database.ActressMergeConflict{{Field: "last_name", DefaultResolution: "target", TargetValue: "", SourceValue: " Source "}}}
	require.Len(t, am.previewFields(), 1)
	am.conflictCursor = 9
	require.NotNil(t, am.currentConflict())
	require.Zero(t, am.conflictCursor)
	am.step = actressMergeStepConflict
	am.resolutions = map[string]string{"last_name": "source"}
	require.Contains(t, am.View(), "Source")
	am.Update(tea.KeyMsg{Type: tea.KeyDown})
	am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	require.Empty(t, am.policy)
	require.Empty(t, am.resolutions)
	require.Nil(t, am.preview)
}

func TestTUIMergePolicyToggleRetainsOverrides(t *testing.T) {
	repo := newActressRepoForTUIMergeTest(t)
	target := models.Actress{FirstName: "桜"}
	source := models.Actress{FirstName: "Source", LastName: "Example", DMMID: 883}
	require.NoError(t, repo.Create(t.Context(), &target))
	require.NoError(t, repo.Create(t.Context(), &source))
	model := New(TUIModelConfig{})
	model.SetActressRepo(repo)
	am := &model.actressMergeCtl.modal
	am.open()
	am.targetInput.SetValue(strconv.Itoa(int(target.ID)))
	am.sourceInput.SetValue(strconv.Itoa(int(source.ID)))
	require.NoError(t, am.loadPreview())
	am.conflictCursor = 2
	am.Update(tea.KeyMsg{Type: tea.KeySpace})
	require.Equal(t, "target", am.resolutions["last_name"])
	am.resolutions["last_name"] = "source"
	_, cmd := am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	am.Update(cmd())
	require.Equal(t, database.MergePolicyPreserveTarget, am.policy)
	require.Equal(t, "source", am.resolutions["last_name"])
	require.Equal(t, "Example", am.preview.ProposedMerged.LastName)
	require.Zero(t, am.preview.ProposedMerged.DMMID)
}
