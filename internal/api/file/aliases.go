package file

import (
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

type ServerDependencies = core.ServerDependencies

type ErrorResponse = contracts.ErrorResponse
type ScanRequest = contracts.ScanRequest
type ScanResponse = contracts.ScanResponse
type FileInfo = contracts.FileInfo
type BrowseRequest = contracts.BrowseRequest
type BrowseResponse = contracts.BrowseResponse
type PathAutocompleteRequest = contracts.PathAutocompleteRequest
type PathAutocompleteSuggestion = contracts.PathAutocompleteSuggestion
type PathAutocompleteResponse = contracts.PathAutocompleteResponse
