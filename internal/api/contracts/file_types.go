package contracts

// ScanRequest represents a directory scan request
type ScanRequest struct {
	Path      string `json:"path" binding:"required" example:"/path/to/videos"`
	Recursive bool   `json:"recursive" example:"true"`
	Filter    string `json:"filter,omitempty" example:"STSK"` // Filter folder/file names (case-insensitive substring match)
}

// ScanResponse represents scan results
type ScanResponse struct {
	Files   []FileInfo `json:"files"`
	Count   int        `json:"count" example:"10"`
	Skipped []string   `json:"skipped,omitempty"`
}

// FileInfo represents file or directory information
type FileInfo struct {
	Name        string `json:"name" example:"video.mp4"`
	Path        string `json:"path" example:"/path/to/video.mp4"`
	IsDir       bool   `json:"is_dir" example:"false"`
	Size        int64  `json:"size" example:"1024000000"`
	ModTime     string `json:"mod_time" example:"2024-01-15T10:30:00Z"`
	MovieID     string `json:"movie_id,omitempty" example:"IPX-535"`
	Matched     bool   `json:"matched" example:"true"`
	IsMultiPart bool   `json:"is_multi_part,omitempty" example:"true"`
	PartNumber  int    `json:"part_number,omitempty" example:"1"`
	PartSuffix  string `json:"part_suffix,omitempty" example:"-pt1"`
}

// BrowseRequest represents a browse request
type BrowseRequest struct {
	Path string `json:"path" example:"/path/to/directory"`
}

// BrowseResponse represents browse results
type BrowseResponse struct {
	CurrentPath string     `json:"current_path" example:"/path/to/directory"`
	ParentPath  string     `json:"parent_path,omitempty" example:"/path/to"`
	Items       []FileInfo `json:"items"`
}

// PathAutocompleteRequest represents a partial path autocomplete request.
type PathAutocompleteRequest struct {
	Path  string `json:"path" binding:"required" example:"/path/to/vid"`
	Limit int    `json:"limit,omitempty" example:"10"`
}

// PathAutocompleteSuggestion represents a single autocomplete suggestion.
type PathAutocompleteSuggestion struct {
	Name  string `json:"name" example:"videos"`
	Path  string `json:"path" example:"/path/to/videos"`
	IsDir bool   `json:"is_dir" example:"true"`
}

// PathAutocompleteResponse represents directory suggestions for a partial path.
type PathAutocompleteResponse struct {
	InputPath   string                       `json:"input_path" example:"/path/to/vid"`
	BasePath    string                       `json:"base_path" example:"/path/to"`
	Suggestions []PathAutocompleteSuggestion `json:"suggestions"`
}
