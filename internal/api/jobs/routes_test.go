package jobs

import (
	"context"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	protected := r.Group("/api/v1")

	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	t.Cleanup(func() { _ = db.Close() })

	repos := db.Repositories()
	deps := NewJobDeps(repos.JobRepo, repos.BatchFileOpRepo, nil, nil, nil, false)

	assert.NotPanics(t, func() {
		RegisterRoutes(protected, deps)
	})
}
