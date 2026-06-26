package core

import (
	"net/http"

	"github.com/gin-gonic/gin"
	contracts "github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/logging"
)

// RespondInternalError logs the underlying error server-side and writes a
// generic 500 response so internal/repository/DB details are not leaked to API
// clients. Use this for unexpected failures on public endpoints instead of
// forwarding err.Error() in the response body.
func RespondInternalError(c *gin.Context, err error) {
	if err != nil {
		logging.Errorf("internal server error: %v", err)
	}
	c.JSON(http.StatusInternalServerError, contracts.ErrorResponse{Error: "internal server error"})
}
