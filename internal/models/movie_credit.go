package models

import (
	"strings"
	"time"
)

const (
	// CreditOriginScrape implements the credit identity lifecycle contract.
	CreditOriginScrape CreditOrigin = "scrape"
	// CreditOriginUser implements the credit identity lifecycle contract.
	CreditOriginUser CreditOrigin = "user"
)

// CreditOrigin implements the credit identity lifecycle contract.
type CreditOrigin string

const (
	// CreditFieldCreditedName implements the credit identity lifecycle contract.
	CreditFieldCreditedName = "credited_name"
	// CreditFieldReportedThumb implements the credit identity lifecycle contract.
	CreditFieldReportedThumb = "reported_thumb_url"
	CreditFieldIdentityLink  = "identity_link"
)

const (
	// CollisionStatusOpen implements the credit identity lifecycle contract.
	CollisionStatusOpen = "open"
	// CollisionStatusResolved implements the credit identity lifecycle contract.
	CollisionStatusResolved = "resolved"
)

const (
	// CollisionResolutionKeepIdentity implements the credit identity lifecycle contract.
	CollisionResolutionKeepIdentity = "keep_identity"
	// CollisionResolutionAdoptCanonical implements the credit identity lifecycle contract.
	CollisionResolutionAdoptCanonical = "adopt_canonical"
	CollisionResolutionAdoptAlias     = "adopt_alias"
	CollisionResolutionReassign       = "reassign"
	CollisionResolutionAutoKeep       = "auto_keep"
	CollisionResolutionAutoAlias      = "auto_alias"
	CollisionResolutionByRemoval      = "resolved_by_removal"
)

// MovieCredit implements the credit identity lifecycle contract.
type MovieCredit struct {
	ID                    uint      `json:"id" gorm:"primaryKey"`
	MovieContentID        string    `json:"movie_content_id" gorm:"not null;uniqueIndex:idx_movie_credits_pair;index"`
	ActressID             uint      `json:"actress_id" gorm:"not null;uniqueIndex:idx_movie_credits_pair;index"`
	CreditedName          string    `json:"credited_name"`
	CreditedJapaneseName  string    `json:"credited_japanese_name"`
	ReportedThumbURL      string    `json:"reported_thumb_url"`
	Source                string    `json:"source"`
	Origin                string    `json:"origin"`
	OrderIndex            int       `json:"order_index"`
	OrderPinned           bool      `json:"order_pinned"`
	OverrideName          string    `json:"override_name"`
	UserOverride          bool      `json:"user_override"`
	Suppressed            bool      `json:"suppressed"`
	LegacyInferred        bool      `json:"legacy_inferred"`
	DisplayForceCanonical bool      `json:"display_force_canonical"`
	Actress               *Actress  `json:"actress,omitempty" gorm:"foreignKey:ActressID"`
	Scraped               Actress   `json:"-" gorm:"-"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// MovieCredit implements the credit identity lifecycle contract.
func (MovieCredit) TableName() string {
	return "movie_credits"
}

// MovieCredit implements the credit identity lifecycle contract.
func (c *MovieCredit) EffectiveOrigin() string {
	if c.Origin == string(CreditOriginUser) {
		return string(CreditOriginUser)
	}
	return string(CreditOriginScrape)
}

// MovieCredit implements the credit identity lifecycle contract.
func (c *MovieCredit) IsScrapeOwned() bool {
	return c.Origin != string(CreditOriginUser) && !c.UserOverride && !c.Suppressed
}

// MovieCredit implements the credit identity lifecycle contract.
func (c *MovieCredit) DisplayName(actress *Actress) string {
	if c.UserOverride && strings.TrimSpace(c.OverrideName) != "" {
		return c.OverrideName
	}
	if actress == nil {
		return c.CreditedName
	}
	if actress.Verified {
		return actress.FullName()
	}
	if strings.TrimSpace(c.CreditedName) != "" {
		return c.CreditedName
	}
	return actress.FullName()
}

// CreditCollision implements the credit identity lifecycle contract.
type CreditCollision struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	CreditID       uint      `json:"credit_id" gorm:"index;not null"`
	MovieContentID string    `json:"movie_content_id" gorm:"index;not null"`
	Field          string    `json:"field"`
	ReportedValue  string    `json:"reported_value"`
	CanonicalValue string    `json:"canonical_value"`
	Status         string    `json:"status"`
	Resolution     string    `json:"resolution"`
	Occurrences    int       `json:"occurrences"`
	SourcesSeen    string    `json:"sources_seen"`
	UserPinned     bool      `json:"user_pinned"`
	LastSeenAt     time.Time `json:"last_seen_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// CreditCollision implements the credit identity lifecycle contract.
func (CreditCollision) TableName() string {
	return "credit_collisions"
}

// CreditCollision implements the credit identity lifecycle contract.
func (c *CreditCollision) IsOpen() bool {
	return c.Status == CollisionStatusOpen
}

// CreditCollision implements the credit identity lifecycle contract.
func (c *CreditCollision) AddSource(source string) {
	source = strings.TrimSpace(source)
	if source == "" {
		return
	}
	for _, s := range strings.Split(c.SourcesSeen, ",") {
		if strings.TrimSpace(s) == source {
			return
		}
	}
	if c.SourcesSeen == "" {
		c.SourcesSeen = source
		return
	}
	c.SourcesSeen = c.SourcesSeen + "," + source
}

// CreditCollision implements the credit identity lifecycle contract.
func (c *CreditCollision) DistinctSourceCount() int {
	if c.SourcesSeen == "" {
		return 0
	}
	return len(strings.Split(c.SourcesSeen, ","))
}
