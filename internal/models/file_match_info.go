package models

import "time"

// FileMatchInfo is the canonical type that crosses all seam boundaries.
// It carries file match metadata from scan+match through apply, organize,
// and preview. Adding a new multipart field requires updating only this
// struct — no bridge functions or duplicate types needed.
type FileMatchInfo struct {
	Path        string    `json:"path"`
	MovieID     string    `json:"movie_id"`
	IsMultiPart bool      `json:"is_multi_part"`
	PartNumber  int       `json:"part_number"`
	PartSuffix  string    `json:"part_suffix"`
	Name        string    `json:"name"`
	Extension   string    `json:"extension"`
	Size        int64     `json:"size"`
	ModTime     time.Time `json:"mod_time"`
}

// FirstValidFileResult returns the first FileMatchInfo with a non-empty Path,
// or nil if none found. This is a utility used by the preview orchestrator
// and downloader to extract multipart info from file results.
func FirstValidFileResult(fileResults []FileMatchInfo) *FileMatchInfo {
	for i := range fileResults {
		if fileResults[i].Path != "" {
			return &fileResults[i]
		}
	}
	return nil
}
