package tui

import (
	"errors"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAuditFailedPolicyPreviewBlocksApply(t *testing.T) {
	model := New(TUIModelConfig{})
	am := &model.actressMergeCtl.modal
	am.step = actressMergeStepConflict
	am.preview = &database.ActressMergePreview{Fields: []database.ActressMergeField{{Field: "dmm_id", TargetValue: uint(0), SourceValue: uint(123), DefaultResolution: "source", ResultValue: uint(123)}}}
	_, cmd := am.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	require.NotNil(t, cmd)
	am.Update(actressPreviewResultMsg{err: errors.New("temporary preview failure"), token: am.mergeReqToken})
	_, apply := am.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Nil(t, apply, "must not merge with new policy after its preview failed")
}
func TestAuditDismissedPreviewDoesNotLeaveReopenedDialogBusy(t *testing.T) {
	model := New(TUIModelConfig{})
	model.SetActressRepo(&database.ActressRepository{})
	am := &model.actressMergeCtl.modal
	am.open()
	am.loadPreviewCmd()
	oldToken := am.mergeReqToken
	am.close()
	am.open()
	am.Update(actressPreviewResultMsg{err: errors.New("old request"), token: oldToken})
	require.False(t, am.busy, "reopened dialog must accept input after stale result ignored")
}
