package jobs

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestJobHandlersRepositoryErrorsPR260(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		route  string
		build  func(JobDeps) gin.HandlerFunc
	}{
		{name: "get", method: http.MethodGet, path: "/jobs/job", route: "/jobs/:id", build: getJob},
		{name: "operations", method: http.MethodGet, path: "/jobs/job/operations", route: "/jobs/:id/operations", build: listOperations},
		{name: "batch revert", method: http.MethodPost, path: "/jobs/job/revert", route: "/jobs/:id/revert", build: revertBatch},
		{name: "operation revert", method: http.MethodPost, path: "/jobs/job/operations/movie/revert", route: "/jobs/:id/operations/:movieId/revert", build: revertOperation},
		{name: "revert check", method: http.MethodGet, path: "/jobs/job/revert-check", route: "/jobs/:id/revert-check", build: revertCheck},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := mocks.NewMockJobRepositoryInterface(t)
			repo.EXPECT().FindByID(mock.Anything, "job").Return(nil, errors.New("database unavailable"))
			deps := JobDeps{JobRepo: repo, AllowRevert: true}
			router := gin.New()
			router.Handle(test.method, test.route, test.build(deps))
			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, http.StatusInternalServerError, response.Code)
		})
	}
}
