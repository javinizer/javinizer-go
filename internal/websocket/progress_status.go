package websocket

import (
	"encoding/json"
	"fmt"
)

// ProgressStatus represents the status of a progress step in the WebSocket wire protocol.
type ProgressStatus string

const (
	// ProgressStatusSuccess indicates the step completed successfully.
	ProgressStatusSuccess ProgressStatus = "success"
	// ProgressStatusError indicates the step failed with an error.
	ProgressStatusError ProgressStatus = "error"
	// ProgressStatusPending indicates the step is waiting to be processed.
	ProgressStatusPending ProgressStatus = "pending"
	// ProgressStatusOrganizeCompleted indicates the organize (apply) phase
	// finished — the terminal frame the frontend's organize-controller
	// finalizes on. Emitted by the OnPhaseComplete broadcaster.
	ProgressStatusOrganizeCompleted ProgressStatus = "organization_completed"
	// ProgressStatusUpdateCompleted indicates the update-mode apply phase
	// finished — the update-mode terminal frame (mirrors
	// ProgressStatusOrganizeCompleted with the update verb).
	ProgressStatusUpdateCompleted ProgressStatus = "update_completed"
)

// String implements fmt.Stringer.
func (s ProgressStatus) String() string { return string(s) }

// MarshalJSON implements json.Marshaler.
func (s ProgressStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

// UnmarshalJSON implements json.Unmarshaler.
func (s *ProgressStatus) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err != nil {
		return fmt.Errorf("ProgressStatus: %w", err)
	}
	*s = ProgressStatus(str)
	return nil
}
