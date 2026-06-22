package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

// --- applyOrchImpl: nil filesystem returns error ---

func TestApplyOrchImpl_Miss_NilFS(t *testing.T) {
	orch := newApplyOrchestrator(
		nil, // nil filesystem
		nil, // organizer
		nil, // downloader
		nil, // nfoGen
		nil, // nfo
		ApplyConfig{},
		nil, // templateEngine
		noOpRevertLog{},
		nil, // tagRepo
		nil, // logger
	)

	_, err := orch.Execute(context.Background(), ApplyCmd{
		Movie: &models.Movie{ID: "NILFS-001"},
	}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "filesystem is nil")
}

// --- applyOrchImpl: nil movie returns error ---

func TestApplyOrchImpl_Miss_NilMovie(t *testing.T) {
	fs := afero.NewMemMapFs()
	orch := newApplyOrchestrator(
		fs,
		nil, // organizer
		nil, // downloader
		nil, // nfoGen
		nil, // nfo
		ApplyConfig{},
		nil, // templateEngine
		noOpRevertLog{},
		nil, // tagRepo
		nil, // logger
	)

	_, err := orch.Execute(context.Background(), ApplyCmd{
		Movie: nil,
	}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "movie is nil")
}
