package batch

import (
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

type ServerDependencies = core.ServerDependencies

type ErrorResponse = contracts.ErrorResponse
type BatchScrapeRequest = contracts.BatchScrapeRequest
type BatchScrapeResponse = contracts.BatchScrapeResponse
type BatchFileResult = contracts.BatchFileResult
type BatchFileResultSlim = contracts.BatchFileResultSlim
type BatchJobResponse = contracts.BatchJobResponse
type BatchJobResponseSlim = contracts.BatchJobResponseSlim
type OrganizeRequest = contracts.OrganizeRequest
type OrganizePreviewRequest = contracts.OrganizePreviewRequest
type OrganizePreviewResponse = contracts.OrganizePreviewResponse
type UpdateMovieRequest = contracts.UpdateMovieRequest
type PosterCropRequest = contracts.PosterCropRequest
type PosterCropResponse = contracts.PosterCropResponse
type BatchRescrapeRequest = contracts.BatchRescrapeRequest
type BatchRescrapeResponse = contracts.BatchRescrapeResponse
type MovieResponse = contracts.MovieResponse
