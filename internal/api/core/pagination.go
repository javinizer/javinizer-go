package core

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// ParsePagination extracts limit and offset query parameters from a gin.Context.
// defaultLimit is used when the limit query param is missing or invalid.
// maxLimit caps the maximum allowed limit value.
func ParsePagination(c *gin.Context, defaultLimit, maxLimit int) (limit, offset int) {
	limit = defaultLimit
	offset = 0

	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
			if limit > maxLimit {
				limit = maxLimit
			}
		}
	}

	if offsetStr := c.Query("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	return limit, offset
}

// PaginationBounds returns safe slice bounds for nonnegative length, limit, and offset.
// It clamps the offset first and limits the page to the remaining elements before
// adding, so even MaxInt pagination values cannot overflow.
func PaginationBounds(length, limit, offset int) (start, end int) {
	start = min(offset, length)
	end = start + min(limit, length-start)
	return start, end
}
