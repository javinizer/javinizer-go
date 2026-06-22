package actress

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/stretchr/testify/assert"
)

func TestWriteActressMergeError_Uncovered(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		err            error
		expectedStatus int
	}{
		{
			name:           "invalid ID",
			err:            database.ErrActressMergeInvalidID,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "same ID",
			err:            database.ErrActressMergeSameID,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid field",
			err:            database.ErrActressMergeInvalidField,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "invalid decision",
			err:            database.ErrActressMergeInvalidDecision,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "not found",
			err:            database.ErrNotFound,
			expectedStatus: http.StatusNotFound,
		},
		{
			name:           "unique constraint",
			err:            database.ErrActressMergeUniqueConstraint,
			expectedStatus: http.StatusConflict,
		},
		{
			name:           "generic error",
			err:            errors.New("something went wrong"),
			expectedStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			writeActressMergeError(c, tt.err)
			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}
