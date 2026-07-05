package apperrors

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
)

type apiErrorResponse struct {
	Error  string   `json:"error"`
	Code   string   `json:"code,omitempty"`
	Docs   string   `json:"docs,omitempty"`
	Errors []string `json:"errors,omitempty"`
}

// WriteAPIError writes an error to the gin response as a structured API error, unwrapping PathError details.
func WriteAPIError(c *gin.Context, err error) {
	var pathErr *PathError
	if errors.As(err, &pathErr) {
		if pathErr.OperatorMessage != "" {
			logOperatorGuidance(c, pathErr)
		}

		c.JSON(pathErr.HTTPStatus, apiErrorResponse{
			Error: pathErr.Message,
			Code:  string(pathErr.Code),
			Docs:  pathErr.DocsURL,
		})
		return
	}

	c.JSON(http.StatusBadRequest, apiErrorResponse{
		Error: err.Error(),
	})
}

func logOperatorGuidance(c *gin.Context, err *PathError) {
	pathInfo := ""
	if err.Path != "" {
		pathInfo = fmt.Sprintf(" (path: %s)", err.Path)
	}
	logging.Infof("Path validation error: %s%s", operatorMessageFor(err, requestContext(c)), pathInfo)
}
