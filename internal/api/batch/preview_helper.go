package batch

import (
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

func previewResultToResponse(result *workflow.PreviewResult) contracts.OrganizePreviewResponse {
	return contracts.OrganizePreviewResponse{
		FolderName:      result.FolderName,
		FileName:        result.FileName,
		SubfolderPath:   result.SubfolderPath,
		FullPath:        result.FullPath,
		VideoFiles:      result.VideoFiles,
		NFOPath:         result.NFOPath,
		NFOPaths:        result.NFOPaths,
		PosterPath:      result.PosterPath,
		FanartPath:      result.FanartPath,
		ExtrafanartPath: result.ExtrafanartPath,
		Screenshots:     result.Screenshots,
		TrailerPath:     result.TrailerPath,
		SourcePath:      result.SourcePath,
		OperationMode:   result.OperationMode,
	}
}
