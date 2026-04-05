package apperrors

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/logging"
)

type APIErrorResponse struct {
	Error  string   `json:"error"`
	Code   string   `json:"code,omitempty"`
	Docs   string   `json:"docs,omitempty"`
	Errors []string `json:"errors,omitempty"`
}

func WriteAPIError(c *gin.Context, err error) {
	var pathErr *PathError
	if errors.As(err, &pathErr) {
		if pathErr.OperatorMessage != "" {
			logOperatorGuidance(c, pathErr)
		}

		c.JSON(pathErr.HTTPStatus, APIErrorResponse{
			Error: pathErr.Message,
			Code:  string(pathErr.Code),
			Docs:  pathErr.DocsURL,
		})
		return
	}

	c.JSON(http.StatusBadRequest, APIErrorResponse{
		Error: err.Error(),
	})
}

func logOperatorGuidance(c *gin.Context, err *PathError) {
	pathInfo := ""
	if err.Path != "" {
		pathInfo = fmt.Sprintf(" (path: %s)", err.Path)
	}
	logging.Infof("Path validation error: %s%s", err.OperatorMessage, pathInfo)
}
