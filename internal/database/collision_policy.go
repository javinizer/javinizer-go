package database

import (
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// CollisionPolicy is the configured collision resolution strategy.
type CollisionPolicy string

const (
	// CollisionPolicyBlock implements the credit identity lifecycle contract.
	CollisionPolicyBlock     CollisionPolicy = "block"
	CollisionPolicyAutoKeep  CollisionPolicy = "auto_keep"
	CollisionPolicyAutoAlias CollisionPolicy = "auto_alias"
)

// NormalizeCollisionPolicy maps unknown raw values to the safe default.
func NormalizeCollisionPolicy(raw string) CollisionPolicy {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case string(CollisionPolicyAutoKeep):
		return CollisionPolicyAutoKeep
	case string(CollisionPolicyAutoAlias):
		return CollisionPolicyAutoAlias
	default:
		return CollisionPolicyBlock
	}
}

// PolicyDecision carries the outcome of applying a collision policy.
type PolicyDecision struct {
	AutoResolved bool
	Resolution   string
	CreateAlias  bool
}

// ApplyFieldCollisionPolicy decides whether a field collision may be
// auto-resolved under the given policy and trusted-source set.
func ApplyFieldCollisionPolicy(policy CollisionPolicy, collision *models.CreditCollision, winningSource string, trustedSources map[string]bool) PolicyDecision {
	if collision == nil {
		return PolicyDecision{}
	}
	if collision.UserPinned {
		return PolicyDecision{AutoResolved: false}
	}
	if collision.Field == models.CreditFieldIdentityLink {
		return PolicyDecision{AutoResolved: false}
	}
	switch policy {
	case CollisionPolicyAutoKeep:
		return PolicyDecision{AutoResolved: true, Resolution: models.CollisionResolutionAutoKeep}
	case CollisionPolicyAutoAlias:
		if collision.Field == models.CreditFieldCreditedName {
			trusted := trustedSources[strings.TrimSpace(winningSource)]
			corroborated := collision.DistinctSourceCount() >= 2
			if trusted || corroborated {
				return PolicyDecision{AutoResolved: true, Resolution: models.CollisionResolutionAutoAlias, CreateAlias: true}
			}
		}
		return PolicyDecision{AutoResolved: true, Resolution: models.CollisionResolutionAutoKeep}
	default:
		return PolicyDecision{AutoResolved: false}
	}
}
