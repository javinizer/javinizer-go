package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/poster"
)

// ValidateJobID is a Gin middleware that validates the ":id" path parameter
// as a safe job identifier. It rejects requests where the job ID is empty,
// ".", "..", or contains path separators — values that could cause path
// traversal when the ID is used in filepath.Join.
//
// Apply this middleware to any route group where c.Param("id") is used as
// a batch job ID and may flow into filesystem operations (poster cleanup,
// temp dir deletion, etc.).
func ValidateJobID() gin.HandlerFunc {
	return func(c *gin.Context) {
		jobID := c.Param("id")
		if err := poster.ValidateJobID(jobID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			c.Abort()
			return
		}
		c.Next()
	}
}
