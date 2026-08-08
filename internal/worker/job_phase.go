package worker

// JobPhase names the phase that owns a Running job's lifecycle
// (POSTER-WRITE-HARDENING D16). Persisted on the job envelope at phase entry
// (current_phase key) so a restart distinguishes a scrape-phase Running job
// (edits rejected) from an apply-phase one (edits admitted).
type JobPhase string

// The two lifecycle-owning phases a Running job can be in (D16 persisted tags).
const (
	JobPhaseScrape JobPhase = "scrape"
	JobPhaseApply  JobPhase = "apply"
)
