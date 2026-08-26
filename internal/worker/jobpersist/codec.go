package jobpersist

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/applyplan"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// JobResultsEnvelope wraps domain results and provenance data for persistence.
// The Results text column stores this envelope instead of
// a raw map[string]*MovieResult.
type JobResultsEnvelope struct {
	Domain     map[string]*resultstore.MovieResult    `json:"domain"`
	Provenance map[string]*resultstore.ProvenanceData `json:"provenance,omitempty"`
	// CurrentPhase mirrors JobLifecycle's phase marker at persist time
	// (POSTER-WRITE-HARDENING D16): "" idle, "scrape"/"apply" while Running.
	CurrentPhase    string `json:"current_phase,omitempty"`
	ApplyGeneration uint64 `json:"apply_generation,omitempty"`
}

// Snapshot holds only fields representable in models.Job (the DB row). It is
// the value type that flows through the persistence codec: Encode produces a
// *models.Job from a Snapshot, and Decode produces a Snapshot from a
// *models.Job. It intentionally excludes worker-only fields (PersistError,
// IsDeleted, ResultIndex) that are not persisted in the database.
type Snapshot struct {
	ID                    string
	PruneVersion          uint64
	EnvelopeGeneration    uint64
	Status                models.JobStatus
	TotalFiles            int
	Completed             int
	Failed                int
	Progress              float64
	Files                 []string
	Results               map[string]*resultstore.MovieResult
	Provenance            map[string]*resultstore.ProvenanceData
	Excluded              map[string]bool
	FileMatchInfo         map[string]models.FileMatchInfo
	Destination           string
	TempDir               string
	OperationModeOverride operationmode.OperationMode
	ApplyPlan             *applyplan.Plan
	StartedAt             time.Time
	CompletedAt           *time.Time
	OrganizedAt           *time.Time
	RevertedAt            *time.Time
	Update                bool
	// CurrentPhase is the lifecycle phase marker durable on the envelope (D16).
	CurrentPhase    string
	ApplyGeneration uint64
}

// MarshalFn is swappable for testing. Defaults to json.Marshal.
var MarshalFn = json.Marshal

// Encode takes an immutable Snapshot value and produces a *models.Job with all
// JSON text columns (Files, Results, Excluded, FileMatchInfo) filled by
// marshaling the corresponding snapshot fields. The Results column uses the
// JobResultsEnvelope format. Scalar fields (ID, Status, etc.) are copied
// directly. Returns a non-nil error and nil *models.Job if any marshal fails.
func Encode(snapshot Snapshot) (*models.Job, error) {
	filesJSON, err := MarshalFn(snapshot.Files)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal files for job %s: %w", snapshot.ID, err)
	}

	envelope := JobResultsEnvelope{
		Domain:          snapshot.Results,
		Provenance:      snapshot.Provenance,
		CurrentPhase:    snapshot.CurrentPhase,
		ApplyGeneration: snapshot.ApplyGeneration,
	}
	resultsJSON, err := MarshalFn(envelope)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal results for job %s: %w", snapshot.ID, err)
	}

	excludedJSON, err := MarshalFn(snapshot.Excluded)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal excluded for job %s: %w", snapshot.ID, err)
	}

	fileMatchInfoJSON, err := MarshalFn(snapshot.FileMatchInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal file match info for job %s: %w", snapshot.ID, err)
	}

	var applyPlanJSON *string
	if snapshot.ApplyPlan != nil {
		normalized, normalizeErr := applyplan.Normalize(snapshot.ApplyPlan)
		if normalizeErr != nil {
			return nil, fmt.Errorf("invalid apply plan for job %s: %w", snapshot.ID, normalizeErr)
		}
		encoded, marshalErr := MarshalFn(normalized)
		if marshalErr != nil {
			return nil, fmt.Errorf("failed to marshal apply plan for job %s: %w", snapshot.ID, marshalErr)
		}
		value := string(encoded)
		applyPlanJSON = &value
	}

	return &models.Job{
		ID:                    snapshot.ID,
		PruneVersion:          snapshot.PruneVersion,
		EnvelopeGeneration:    snapshot.EnvelopeGeneration,
		Status:                snapshot.Status,
		TotalFiles:            snapshot.TotalFiles,
		Completed:             snapshot.Completed,
		Failed:                snapshot.Failed,
		Progress:              snapshot.Progress,
		Destination:           snapshot.Destination,
		TempDir:               snapshot.TempDir,
		OperationModeOverride: snapshot.OperationModeOverride,
		ApplyPlan:             applyPlanJSON,
		Files:                 string(filesJSON),
		Results:               string(resultsJSON),
		Excluded:              string(excludedJSON),
		FileMatchInfo:         string(fileMatchInfoJSON),
		StartedAt:             snapshot.StartedAt,
		CompletedAt:           snapshot.CompletedAt,
		OrganizedAt:           snapshot.OrganizedAt,
		RevertedAt:            snapshot.RevertedAt,
		Update:                snapshot.Update,
	}, nil
}

// Decode takes a database row and produces a Snapshot value by unmarshaling
// the JSON text columns. It handles the three legacy Results formats via
// ParseResultsJSON. Scalar DB fields are always populated regardless of JSON
// parse failures; non-fatal deserialization errors for individual columns are
// returned as a slice (one entry per failed column). The returned Snapshot is
// never nil — it is a value type.
func Decode(dbJob *models.Job) (Snapshot, []error) {
	var errs []error

	snapshot := Snapshot{
		ID:                    dbJob.ID,
		PruneVersion:          dbJob.PruneVersion,
		EnvelopeGeneration:    dbJob.EnvelopeGeneration,
		Status:                dbJob.Status,
		TotalFiles:            dbJob.TotalFiles,
		Completed:             dbJob.Completed,
		Failed:                dbJob.Failed,
		Progress:              dbJob.Progress,
		Destination:           dbJob.Destination,
		TempDir:               dbJob.TempDir,
		OperationModeOverride: dbJob.OperationModeOverride,
		StartedAt:             dbJob.StartedAt,
		CompletedAt:           dbJob.CompletedAt,
		OrganizedAt:           dbJob.OrganizedAt,
		RevertedAt:            dbJob.RevertedAt,
		Update:                dbJob.Update,
		ApplyPlan:             nil,
		Files:                 []string{},
		Results:               make(map[string]*resultstore.MovieResult),
		Provenance:            make(map[string]*resultstore.ProvenanceData),
		Excluded:              make(map[string]bool),
		FileMatchInfo:         make(map[string]models.FileMatchInfo),
	}

	if dbJob.ApplyPlan != nil && *dbJob.ApplyPlan != "" {
		var plan applyplan.Plan
		if err := json.Unmarshal([]byte(*dbJob.ApplyPlan), &plan); err != nil {
			errs = append(errs, fmt.Errorf("failed to parse apply plan for job %s: %w", dbJob.ID, err))
		} else if normalized, err := applyplan.Normalize(&plan); err != nil {
			errs = append(errs, fmt.Errorf("invalid apply plan for job %s: %w", dbJob.ID, err))
		} else {
			snapshot.ApplyPlan = normalized
		}
	}

	if dbJob.Files != "" {
		var files []string
		if err := json.Unmarshal([]byte(dbJob.Files), &files); err != nil {
			errs = append(errs, fmt.Errorf("failed to parse files for job %s: %w", dbJob.ID, err))
		} else {
			snapshot.Files = files
		}
	}

	if dbJob.Results != "" {
		parsed, err := ParseResultsJSON([]byte(dbJob.Results))
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to parse results for job %s: %w", dbJob.ID, err))
		} else {
			snapshot.Results = parsed.Results
			snapshot.Provenance = parsed.Provenance
			snapshot.CurrentPhase = parsed.CurrentPhase
			snapshot.ApplyGeneration = parsed.ApplyGeneration
		}
	}

	if dbJob.Excluded != "" {
		var excluded map[string]bool
		if err := json.Unmarshal([]byte(dbJob.Excluded), &excluded); err != nil {
			errs = append(errs, fmt.Errorf("failed to parse excluded for job %s: %w", dbJob.ID, err))
		} else {
			snapshot.Excluded = excluded
		}
	}

	if dbJob.FileMatchInfo != "" {
		var fileMatchInfo map[string]models.FileMatchInfo
		if err := json.Unmarshal([]byte(dbJob.FileMatchInfo), &fileMatchInfo); err != nil {
			errs = append(errs, fmt.Errorf("failed to parse file match info for job %s: %w", dbJob.ID, err))
		} else {
			snapshot.FileMatchInfo = fileMatchInfo
		}
	}

	return snapshot, errs
}
