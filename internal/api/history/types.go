package history

// HistoryRecord represents a single history record in API responses
type HistoryRecord struct {
	ID           uint   `json:"id"`
	MovieID      string `json:"movie_id"`
	Operation    string `json:"operation"`
	OriginalPath string `json:"original_path"`
	NewPath      string `json:"new_path"`
	Status       string `json:"status"`
	ErrorMessage string `json:"error_message"`
	Metadata     string `json:"metadata"`
	DryRun       bool   `json:"dry_run"`
	CreatedAt    string `json:"created_at"`
}

// HistoryListResponse is the response for listing history records
type HistoryListResponse struct {
	Records []HistoryRecord `json:"records"`
	Total   int64           `json:"total"`
	Limit   int             `json:"limit"`
	Offset  int             `json:"offset"`
}

// HistoryStats represents aggregated history statistics
type HistoryStats struct {
	Total       int64            `json:"total"`
	Success     int64            `json:"success"`
	Failed      int64            `json:"failed"`
	Reverted    int64            `json:"reverted"`
	ByOperation map[string]int64 `json:"by_operation"`
}

// DeleteHistoryBulkResponse is the response for bulk deletion
type DeleteHistoryBulkResponse struct {
	Deleted int64 `json:"deleted"`
}
