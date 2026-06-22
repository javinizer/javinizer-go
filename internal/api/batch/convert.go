package batch

import (
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/worker"
)

// batchJobBaseResponse holds the fields shared between full and slim batch job
// API responses. Extracted as a named struct to avoid the 13-unnamed-return
// pattern from the previous batchJobBaseFields function.
type batchJobBaseResponse struct {
	ID                    string
	Status                models.JobStatus
	TotalFiles            int
	Completed             int
	Failed                int
	Excluded              map[string]bool
	Progress              float64
	Destination           string
	StartedAt             string
	CompletedAt           *string
	OperationModeOverride operationmode.OperationMode
	Update                bool
	PersistError          string
}

// toBaseResponse extracts the fields shared between full and slim batch job
// responses into a named struct, eliminating the previous 13-unnamed-return
// pattern and keeping mapping local to the data.
func toBaseResponse(job *worker.BatchJobStatus) batchJobBaseResponse {
	return batchJobBaseResponse{
		ID:                    job.ID.String(),
		Status:                job.Status,
		TotalFiles:            job.TotalFiles,
		Completed:             job.Completed,
		Failed:                job.Failed,
		Excluded:              job.Excluded,
		Progress:              job.Progress,
		Destination:           job.Destination,
		StartedAt:             contracts.FormatTime(job.StartedAt),
		CompletedAt:           contracts.FormatTimePtr(job.CompletedAt),
		OperationModeOverride: job.OperationModeOverride,
		Update:                job.Update,
		PersistError:          job.PersistError,
	}
}

// movieResultToResponse converts a MovieResult into a full API response with provenance.
func movieResultToResponse(mr *worker.MovieResult, prov *worker.ProvenanceData) *contracts.BatchFileResult {
	if mr == nil {
		return nil
	}
	result := &contracts.BatchFileResult{
		ResultID:    mr.ResultID,
		FilePath:    mr.FileMatchInfo.Path,
		MovieID:     mr.FileMatchInfo.MovieID,
		IsMultiPart: mr.FileMatchInfo.IsMultiPart,
		PartNumber:  mr.FileMatchInfo.PartNumber,
		PartSuffix:  mr.FileMatchInfo.PartSuffix,
		Status:      mr.Status,
		Error:       mr.Error,
		Movie:       contracts.MovieViewFromModel(mr.Movie),
		StartedAt:   contracts.FormatTime(mr.StartedAt),
		EndedAt:     contracts.FormatTimePtr(mr.EndedAt),
	}
	if prov != nil {
		result.FieldSources = prov.FieldSources
		result.ActressSources = prov.ActressSources
	}
	return result
}

// movieResultToSlimResponse converts a MovieResult into a lightweight API response
// without movie data.
func movieResultToSlimResponse(mr *worker.MovieResult, prov *worker.ProvenanceData) *contracts.BatchFileResultSlim {
	if mr == nil {
		return nil
	}
	result := &contracts.BatchFileResultSlim{
		ResultID:    mr.ResultID,
		FilePath:    mr.FileMatchInfo.Path,
		MovieID:     mr.FileMatchInfo.MovieID,
		IsMultiPart: mr.FileMatchInfo.IsMultiPart,
		PartNumber:  mr.FileMatchInfo.PartNumber,
		PartSuffix:  mr.FileMatchInfo.PartSuffix,
		Status:      mr.Status,
		Error:       mr.Error,
		StartedAt:   contracts.FormatTime(mr.StartedAt),
		EndedAt:     contracts.FormatTimePtr(mr.EndedAt),
	}
	if prov != nil {
		result.FieldSources = prov.FieldSources
		result.ActressSources = prov.ActressSources
	}
	return result
}

// buildBatchJobResponse converts a BatchJobStatus into a full API response with
// movie data, provenance, and all metadata fields.
func buildBatchJobResponse(job *worker.BatchJobStatus) *contracts.BatchJobResponse {
	results := make(map[string]*contracts.BatchFileResult, len(job.Results))
	for filePath, fileResult := range job.Results {
		var prov *worker.ProvenanceData
		if job.Provenance != nil {
			prov = job.Provenance[filePath]
		}
		results[filePath] = movieResultToResponse(fileResult, prov)
	}

	base := toBaseResponse(job)

	return &contracts.BatchJobResponse{
		ID:                    base.ID,
		Status:                base.Status,
		TotalFiles:            base.TotalFiles,
		Completed:             base.Completed,
		Failed:                base.Failed,
		Excluded:              base.Excluded,
		Progress:              base.Progress,
		Destination:           base.Destination,
		Files:                 job.Files,
		Results:               results,
		StartedAt:             base.StartedAt,
		CompletedAt:           base.CompletedAt,
		OperationModeOverride: base.OperationModeOverride,
		Update:                base.Update,
		PersistError:          base.PersistError,
	}
}

// buildBatchJobSlimResponse converts a BatchJobStatus into a slim API response
// without full movie data, suitable for polling/progress endpoints.
func buildBatchJobSlimResponse(job *worker.BatchJobStatus) *contracts.BatchJobResponseSlim {
	results := make(map[string]*contracts.BatchFileResultSlim, len(job.Results))
	for filePath, fileResult := range job.Results {
		var prov *worker.ProvenanceData
		if job.Provenance != nil {
			prov = job.Provenance[filePath]
		}
		results[filePath] = movieResultToSlimResponse(fileResult, prov)
	}

	base := toBaseResponse(job)

	return &contracts.BatchJobResponseSlim{
		ID:                    base.ID,
		Status:                base.Status,
		TotalFiles:            base.TotalFiles,
		Completed:             base.Completed,
		Failed:                base.Failed,
		Excluded:              base.Excluded,
		Progress:              base.Progress,
		Destination:           base.Destination,
		Results:               results,
		StartedAt:             base.StartedAt,
		CompletedAt:           base.CompletedAt,
		OperationModeOverride: base.OperationModeOverride,
		Update:                base.Update,
		PersistError:          base.PersistError,
	}
}
