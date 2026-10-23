package videocollision

import "fmt"

// Policy controls how an existing video destination is handled.
type Policy string

// Fail, Skip, and Replace are the supported video collision policies.
const (
	Fail    Policy = "fail"
	Skip    Policy = "skip"
	Replace Policy = "replace"
)

// Normalize validates a policy and defaults an omitted policy to Fail.
func Normalize(policy Policy) (Policy, error) {
	if policy == "" {
		return Fail, nil
	}
	switch policy {
	case Fail, Skip, Replace:
		return policy, nil
	default:
		return "", fmt.Errorf("invalid video_collision_policy %q", policy)
	}
}

// Decision records a per-source action and optional signed review receipt.
type Decision struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Policy      Policy `json:"policy"`
	ReviewToken string `json:"review_token"`
}

// Collision describes a planned video destination for user review.
type Collision struct {
	SelectedSource  string `json:"selected_source,omitempty"`
	Source          string `json:"source"`
	Destination     string `json:"destination"`
	SourceSize      int64  `json:"source_size"`
	DestinationSize int64  `json:"destination_size"`
	Kind            string `json:"kind"`
	ReviewToken     string `json:"review_token,omitempty"`
}

// ConflictError identifies a video collision that requires refreshed review.
type ConflictError struct {
	Code      string    `json:"code"`
	Collision Collision `json:"collision"`
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s: video destination conflict: %s -> %s (%s); refresh video collision review", e.Code, e.Collision.Source, e.Collision.Destination, e.Collision.Kind)
}
