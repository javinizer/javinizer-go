package movie

import (
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

type ServerDependencies = core.ServerDependencies

type ErrorResponse = contracts.ErrorResponse
type ScrapeRequest = contracts.ScrapeRequest
type ScrapeResponse = contracts.ScrapeResponse
type MovieResponse = contracts.MovieResponse
type MoviesResponse = contracts.MoviesResponse
type RescrapeRequest = contracts.RescrapeRequest
type NFOComparisonRequest = contracts.NFOComparisonRequest
type NFOComparisonResponse = contracts.NFOComparisonResponse
type DataSource = contracts.DataSource
type MergeStatistics = contracts.MergeStatistics
type FieldDifference = contracts.FieldDifference
