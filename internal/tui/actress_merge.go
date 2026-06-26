package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/javinizer/javinizer-go/internal/database"
)

// newActressMergeModal constructs the actress merge modal with the provided deps.
// Accepts only the narrow deps interface instead of *Model.
func newActressMergeModal(deps actressMergeDeps) actressMergeModal {
	mergeTargetInput := textinput.New()
	mergeTargetInput.Placeholder = "Target actress ID"
	mergeTargetInput.CharLimit = 20
	mergeTargetInput.Width = 20

	mergeSourceInput := textinput.New()
	mergeSourceInput.Placeholder = "Source actress ID"
	mergeSourceInput.CharLimit = 20
	mergeSourceInput.Width = 20

	return actressMergeModal{
		targetInput: mergeTargetInput,
		sourceInput: mergeSourceInput,
		focus:       0,
		step:        actressMergeStepInput,
		resolutions: make(map[string]string),
		showing:     false,
		deps:        deps,
	}
}

const (
	actressMergeStepInput    = "input"
	actressMergeStepConflict = "conflicts"
	actressMergeStepResult   = "result"
)

// SetActressRepo sets the actress repository used by the merge modal.
func (m *Model) SetActressRepo(repo database.ActressRepositoryInterface) {
	m.actressRepo = repo
}

// --- actressMergeModal methods ---

func (am *actressMergeModal) setFocus(focus int) {
	if focus < 0 || focus > 1 {
		focus = 0
	}
	am.focus = focus
	if focus == 0 {
		am.targetInput.Focus()
		am.sourceInput.Blur()
		return
	}
	am.sourceInput.Focus()
	am.targetInput.Blur()
}

func (am *actressMergeModal) reset() {
	am.step = actressMergeStepInput
	am.preview = nil
	am.result = nil
	am.err = ""
	am.conflictCursor = 0
	am.resolutions = make(map[string]string)
	am.targetInput.SetValue("")
	am.sourceInput.SetValue("")
	am.setFocus(0)
}

func (am *actressMergeModal) open() {
	if am.deps.ActressRepo() == nil {
		am.deps.AddLog("warn", "Actress merge unavailable: repository not initialized")
		return
	}
	am.showing = true
	am.reset()
}

func (am *actressMergeModal) close() {
	am.showing = false
	am.reset()
	am.targetInput.Blur()
	am.sourceInput.Blur()
}

// actressMergeOpTimeout bounds the blocking DB work performed by the merge
// modal so a slow/hung repository cannot stall the TUI indefinitely.
const actressMergeOpTimeout = 30 * time.Second

// actressPreviewResultMsg carries the outcome of an async PreviewMerge lookup.
// token ties the result to the request that produced it so stale replies can
// be ignored.
type actressPreviewResultMsg struct {
	preview *database.ActressMergePreview
	err     error
	token   uint64
}

// actressMergeResultMsg carries the outcome of an async Merge operation.
// token ties the result to the request that produced it so stale replies can
// be ignored.
type actressMergeResultMsg struct {
	result *database.ActressMergeResult
	err    error
	token  uint64
}

func (am *actressMergeModal) loadPreviewCmd() tea.Cmd {
	// Snapshot the live modal inputs BEFORE returning the cmd so a later edit
	// (or a second Enter press) cannot race with the background read. The token
	// lets handlePreviewResult ignore stale/out-of-order replies.
	targetVal := am.targetInput.Value()
	sourceVal := am.sourceInput.Value()
	token := am.nextMergeToken()
	return func() tea.Msg {
		repo := am.deps.ActressRepo()
		if repo == nil {
			return actressPreviewResultMsg{err: fmt.Errorf("actress repository not initialized"), token: token}
		}
		targetID, err := parseActressMergeID(targetVal)
		if err != nil {
			return actressPreviewResultMsg{err: fmt.Errorf("target ID: %w", err), token: token}
		}
		sourceID, err := parseActressMergeID(sourceVal)
		if err != nil {
			return actressPreviewResultMsg{err: fmt.Errorf("source ID: %w", err), token: token}
		}
		ctx, cancel := context.WithTimeout(context.Background(), actressMergeOpTimeout)
		defer cancel()
		preview, err := repo.PreviewMerge(ctx, targetID, sourceID)
		if err != nil {
			return actressPreviewResultMsg{err: err, token: token}
		}
		return actressPreviewResultMsg{preview: preview, token: token}
	}
}

func (am *actressMergeModal) applyCmd() tea.Cmd {
	// Snapshot the live modal inputs and clone resolutions BEFORE returning the
	// cmd so later edits cannot race with the background write. The token lets
	// handleMergeResult ignore stale/out-of-order replies.
	targetVal := am.targetInput.Value()
	sourceVal := am.sourceInput.Value()
	resolutionsCopy := make(map[string]string, len(am.resolutions))
	for k, v := range am.resolutions {
		resolutionsCopy[k] = v
	}
	token := am.nextMergeToken()
	return func() tea.Msg {
		repo := am.deps.ActressRepo()
		if repo == nil {
			return actressMergeResultMsg{err: fmt.Errorf("actress repository not initialized"), token: token}
		}
		targetID, err := parseActressMergeID(targetVal)
		if err != nil {
			return actressMergeResultMsg{err: fmt.Errorf("target ID: %w", err), token: token}
		}
		sourceID, err := parseActressMergeID(sourceVal)
		if err != nil {
			return actressMergeResultMsg{err: fmt.Errorf("source ID: %w", err), token: token}
		}
		ctx, cancel := context.WithTimeout(context.Background(), actressMergeOpTimeout)
		defer cancel()
		result, err := repo.Merge(ctx, targetID, sourceID, resolutionsCopy)
		if err != nil {
			return actressMergeResultMsg{err: err, token: token}
		}
		return actressMergeResultMsg{result: result, token: token}
	}
}

// nextMergeToken returns the next in-flight request token and records it as the
// current one, so only the most-recently-launched request's reply is applied.
func (am *actressMergeModal) nextMergeToken() uint64 {
	am.mergeReqToken++
	return am.mergeReqToken
}

// handlePreviewResult applies the async PreviewMerge outcome to modal state.
// Stale/out-of-order replies (token mismatch) are ignored.
func (am *actressMergeModal) handlePreviewResult(m actressPreviewResultMsg) (tea.Model, tea.Cmd) {
	if m.token != am.mergeReqToken {
		return am, nil
	}
	am.mergeReqToken = 0
	if m.err != nil {
		am.err = normalizeActressMergeError(m.err)
		am.deps.AddLog("warn", "Actress merge preview failed: "+m.err.Error())
		return am, nil
	}
	am.preview = m.preview
	am.result = nil
	am.err = ""
	am.conflictCursor = 0
	am.resolutions = make(map[string]string, len(m.preview.DefaultResolutions))
	for field, decision := range m.preview.DefaultResolutions {
		am.resolutions[field] = decision
	}
	for _, conflict := range m.preview.Conflicts {
		if _, ok := am.resolutions[conflict.Field]; !ok {
			am.resolutions[conflict.Field] = conflict.DefaultResolution
		}
	}
	am.step = actressMergeStepConflict
	return am, nil
}

// handleMergeResult applies the async Merge outcome to modal state.
// Stale/out-of-order replies (token mismatch) are ignored.
func (am *actressMergeModal) handleMergeResult(m actressMergeResultMsg) (tea.Model, tea.Cmd) {
	if m.token != am.mergeReqToken {
		return am, nil
	}
	am.mergeReqToken = 0
	if m.err != nil {
		am.err = normalizeActressMergeError(m.err)
		am.deps.AddLog("warn", "Actress merge failed: "+m.err.Error())
		return am, nil
	}
	am.result = m.result
	am.err = ""
	am.step = actressMergeStepResult
	am.deps.AddLog("info", fmt.Sprintf("Merged actress #%d into #%d", m.result.MergedFromID, m.result.MergedActress.ID))
	return am, nil
}

func (am *actressMergeModal) loadPreview() error {
	repo := am.deps.ActressRepo()
	if repo == nil {
		return fmt.Errorf("actress repository not initialized")
	}
	targetID, err := parseActressMergeID(am.targetInput.Value())
	if err != nil {
		return fmt.Errorf("target ID: %w", err)
	}
	sourceID, err := parseActressMergeID(am.sourceInput.Value())
	if err != nil {
		return fmt.Errorf("source ID: %w", err)
	}
	preview, err := repo.PreviewMerge(context.Background(), targetID, sourceID)
	if err != nil {
		return err
	}
	am.preview = preview
	am.result = nil
	am.err = ""
	am.conflictCursor = 0
	am.resolutions = make(map[string]string, len(preview.DefaultResolutions))
	for field, decision := range preview.DefaultResolutions {
		am.resolutions[field] = decision
	}
	for _, conflict := range preview.Conflicts {
		if _, ok := am.resolutions[conflict.Field]; !ok {
			am.resolutions[conflict.Field] = conflict.DefaultResolution
		}
	}
	am.step = actressMergeStepConflict
	return nil
}

func (am *actressMergeModal) currentConflict() *database.ActressMergeConflict {
	if am.preview == nil || len(am.preview.Conflicts) == 0 {
		return nil
	}
	if am.conflictCursor < 0 {
		am.conflictCursor = 0
	}
	if am.conflictCursor >= len(am.preview.Conflicts) {
		am.conflictCursor = len(am.preview.Conflicts) - 1
	}
	return &am.preview.Conflicts[am.conflictCursor]
}

// Init is a no-op; actress merge modals do not issue commands on init.
func (am *actressMergeModal) Init() tea.Cmd { return nil }

// Update processes Bubble Tea messages for the actress merge modal.
func (am *actressMergeModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case actressPreviewResultMsg:
		return am.handlePreviewResult(m)
	case actressMergeResultMsg:
		return am.handleMergeResult(m)
	}
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return am, nil
	}
	switch am.step {
	case actressMergeStepInput:
		return am.updateInputStep(keyMsg)
	case actressMergeStepConflict:
		return am.updateConflictStep(keyMsg)
	case actressMergeStepResult:
		return am.updateResultStep(keyMsg)
	default:
		am.step = actressMergeStepInput
		return am, nil
	}
}

func (am *actressMergeModal) updateInputStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		am.close()
		return am, nil
	case "tab", "up", "down":
		if am.focus == 0 {
			am.setFocus(1)
		} else {
			am.setFocus(0)
		}
		return am, nil
	case "enter":
		if am.focus == 0 {
			am.setFocus(1)
			return am, nil
		}
		// Run the blocking preview lookup off the event loop via tea.Cmd with a
		// bounded context, so the TUI stays responsive while the DB I/O runs.
		am.err = ""
		return am, am.loadPreviewCmd()
	}
	var cmd tea.Cmd
	if am.focus == 0 {
		am.targetInput, cmd = am.targetInput.Update(msg)
	} else {
		am.sourceInput, cmd = am.sourceInput.Update(msg)
	}
	return am, cmd
}

func (am *actressMergeModal) updateConflictStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		am.close()
		return am, nil
	case "r":
		am.step = actressMergeStepInput
		am.preview = nil
		am.result = nil
		am.err = ""
		am.setFocus(0)
		return am, nil
	case "up", "k":
		if am.conflictCursor > 0 {
			am.conflictCursor--
		}
		return am, nil
	case "down", "j":
		if am.preview != nil && am.conflictCursor < len(am.preview.Conflicts)-1 {
			am.conflictCursor++
		}
		return am, nil
	case "t", "h", "left":
		if conflict := am.currentConflict(); conflict != nil {
			am.resolutions[conflict.Field] = "target"
		}
		return am, nil
	case "s", "l", "right":
		if conflict := am.currentConflict(); conflict != nil {
			am.resolutions[conflict.Field] = "source"
		}
		return am, nil
	case " ", "space":
		if conflict := am.currentConflict(); conflict != nil {
			current := am.resolutions[conflict.Field]
			if current == "source" {
				am.resolutions[conflict.Field] = "target"
			} else {
				am.resolutions[conflict.Field] = "source"
			}
		}
		return am, nil
	case "enter":
		// Run the blocking merge off the event loop via tea.Cmd with a bounded
		// context, so the TUI stays responsive while the DB I/O runs.
		am.err = ""
		return am, am.applyCmd()
	}
	return am, nil
}

func (am *actressMergeModal) updateResultStep(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "enter":
		am.close()
		return am, nil
	case "r":
		keepTarget := strings.TrimSpace(am.targetInput.Value())
		am.reset()
		am.targetInput.SetValue(keepTarget)
		am.setFocus(1)
		return am, nil
	}
	return am, nil
}

// View renders the actress merge modal overlay.
func (am *actressMergeModal) View() string {
	width := am.deps.Width()
	height := am.deps.Height()
	modalWidth := 78
	if width > 0 && width-4 < modalWidth {
		modalWidth = width - 4
	}
	if modalWidth < 50 {
		modalWidth = 50
	}
	modalHeight := 24
	if height > 0 && height-4 < modalHeight {
		modalHeight = height - 4
	}
	if modalHeight < 14 {
		modalHeight = 14
	}
	amModalStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(1, 2).Width(modalWidth).Height(modalHeight)
	amTitleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63")).MarginBottom(1)
	amErrorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	muted := lipgloss.NewStyle().Faint(true)
	lines := []string{amTitleStyle.Render("Actress Merge")}
	if am.err != "" {
		lines = append(lines, amErrorStyle.Render("Error: "+am.err), "")
	}
	switch am.step {
	case actressMergeStepInput:
		targetLabel := "  Target ID: "
		sourceLabel := "  Source ID: "
		if am.focus == 0 {
			targetLabel = "▸ Target ID: "
		} else {
			sourceLabel = "▸ Source ID: "
		}
		lines = append(lines, targetLabel+am.targetInput.View(), sourceLabel+am.sourceInput.View(), "", muted.Render("Tab/↑↓ switch field • Enter on Source loads preview • Esc cancel"))
	case actressMergeStepConflict:
		if am.preview == nil {
			lines = append(lines, "No merge preview loaded.", "", muted.Render("Press r to go back or Esc to close."))
			break
		}
		lines = append(lines, fmt.Sprintf("Merging source #%d into target #%d", am.preview.Source.ID, am.preview.Target.ID), fmt.Sprintf("Conflicts: %d", len(am.preview.Conflicts)), "")
		if len(am.preview.Conflicts) == 0 {
			lines = append(lines, "No field conflicts found. Default merge behavior will be used.", "", muted.Render("Enter apply merge • r edit IDs • Esc cancel"))
			break
		}
		for i, conflict := range am.preview.Conflicts {
			cursor := "  "
			if i == am.conflictCursor {
				cursor = "▸ "
			}
			decision := am.resolutions[conflict.Field]
			if decision == "" {
				decision = conflict.DefaultResolution
			}
			lines = append(lines, fmt.Sprintf("%s%s [%s]", cursor, conflict.Field, decision))
		}
		if conflict := am.currentConflict(); conflict != nil {
			decision := am.resolutions[conflict.Field]
			if decision == "" {
				decision = conflict.DefaultResolution
			}
			lines = append(lines, "", fmt.Sprintf("Field: %s (selected: %s)", conflict.Field, decision), "  target: "+formatConflictValue(conflict.TargetValue), "  source: "+formatConflictValue(conflict.SourceValue))
		}
		lines = append(lines, "", muted.Render("↑↓ choose field • t/s select value • Space toggle • Enter apply • r edit IDs • Esc cancel"))
	case actressMergeStepResult:
		if am.result == nil {
			lines = append(lines, "Merge finished, but no result is available.", "", muted.Render("r new merge • Esc close"))
			break
		}
		lines = append(lines, fmt.Sprintf("Merged actress #%d into #%d", am.result.MergedFromID, am.result.MergedActress.ID), fmt.Sprintf("Updated movies: %d", am.result.UpdatedMovies), fmt.Sprintf("Conflicts resolved: %d", am.result.ConflictsResolved), fmt.Sprintf("Aliases added: %d", am.result.AliasesAdded), "", muted.Render("r new merge with same target • Enter/Esc close"))
	}
	content := strings.Join(lines, "\n")
	modal := amModalStyle.Render(content)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modal)
}

// --- package-level helpers ---

func parseActressMergeID(raw string) (uint, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("actress ID is required")
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil || parsed == 0 {
		return 0, fmt.Errorf("invalid actress ID: %q", value)
	}
	return uint(parsed), nil
}

func formatConflictValue(value any) string {
	if value == nil {
		return "(empty)"
	}
	switch v := value.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return "(empty)"
		}
		return v
	default:
		return fmt.Sprintf("%v", value)
	}
}

func normalizeActressMergeError(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, database.ErrActressMergeSameID) {
		return "target and source must be different actress IDs"
	}
	if errors.Is(err, database.ErrActressMergeInvalidID) {
		return "target and source must be positive actress IDs"
	}
	return err.Error()
}
