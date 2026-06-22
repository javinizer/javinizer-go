package actress

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeActressRequest_Uncovered(t *testing.T) {
	tests := []struct {
		name     string
		input    actressRequest
		expected actressRequest
	}{
		{
			name: "trims whitespace from all fields",
			input: actressRequest{
				FirstName:    "  Jane  ",
				LastName:     "  Doe  ",
				JapaneseName: "  ジェーン  ",
				ThumbURL:     "  https://example.com/img.jpg  ",
				Aliases:      "  alias1, alias2  ",
			},
			expected: actressRequest{
				FirstName:    "Jane",
				LastName:     "Doe",
				JapaneseName: "ジェーン",
				ThumbURL:     "https://example.com/img.jpg",
				Aliases:      "alias1, alias2",
			},
		},
		{
			name: "empty strings remain empty",
			input: actressRequest{
				FirstName:    "",
				LastName:     "",
				JapaneseName: "",
				ThumbURL:     "",
				Aliases:      "",
			},
			expected: actressRequest{
				FirstName:    "",
				LastName:     "",
				JapaneseName: "",
				ThumbURL:     "",
				Aliases:      "",
			},
		},
		{
			name: "whitespace-only becomes empty",
			input: actressRequest{
				FirstName:    "   ",
				JapaneseName: "   ",
			},
			expected: actressRequest{
				FirstName:    "",
				JapaneseName: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.input
			normalizeActressRequest(&req)
			assert.Equal(t, tt.expected, req)
		})
	}
}

func TestValidateActressRequest_Uncovered(t *testing.T) {
	tests := []struct {
		name    string
		input   actressRequest
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid with first name only",
			input: actressRequest{
				FirstName: "Jane",
			},
			wantErr: false,
		},
		{
			name: "valid with japanese name only",
			input: actressRequest{
				JapaneseName: "ジェーン",
			},
			wantErr: false,
		},
		{
			name: "valid with both names",
			input: actressRequest{
				FirstName:    "Jane",
				JapaneseName: "ジェーン",
			},
			wantErr: false,
		},
		{
			name: "invalid - no names provided",
			input: actressRequest{
				DMMID: 123,
			},
			wantErr: true,
			errMsg:  "either first_name or japanese_name is required",
		},
		{
			name: "invalid - negative DMM ID",
			input: actressRequest{
				DMMID:     -1,
				FirstName: "Jane",
			},
			wantErr: true,
			errMsg:  "dmm_id must be greater than or equal to 0",
		},
		{
			name: "valid - zero DMM ID",
			input: actressRequest{
				DMMID:     0,
				FirstName: "Jane",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateActressRequest(&tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseSort_Uncovered(t *testing.T) {
	tests := []struct {
		name        string
		sortBy      string
		sortOrder   string
		expectSort  string
		expectOrder string
		wantErr     bool
		errContains string
	}{
		{
			name:        "defaults to name asc",
			sortBy:      "",
			sortOrder:   "",
			expectSort:  "name",
			expectOrder: "asc",
			wantErr:     false,
		},
		{
			name:        "explicit sort by id desc",
			sortBy:      "id",
			sortOrder:   "desc",
			expectSort:  "id",
			expectOrder: "desc",
			wantErr:     false,
		},
		{
			name:        "case insensitive",
			sortBy:      "DMM_ID",
			sortOrder:   "DESC",
			expectSort:  "dmm_id",
			expectOrder: "desc",
			wantErr:     false,
		},
		{
			name:        "invalid sort column",
			sortBy:      "invalid",
			sortOrder:   "asc",
			wantErr:     true,
			errContains: "invalid sort_by value",
		},
		{
			name:        "invalid sort order",
			sortBy:      "name",
			sortOrder:   "random",
			wantErr:     true,
			errContains: "invalid sort_order value",
		},
		{
			name:        "japanese_name column",
			sortBy:      "japanese_name",
			sortOrder:   "asc",
			expectSort:  "japanese_name",
			expectOrder: "asc",
			wantErr:     false,
		},
		{
			name:        "created_at column",
			sortBy:      "created_at",
			sortOrder:   "desc",
			expectSort:  "created_at",
			expectOrder: "desc",
			wantErr:     false,
		},
		{
			name:        "whitespace trimmed",
			sortBy:      "%20name%20",
			sortOrder:   "%20asc%20",
			expectSort:  "name",
			expectOrder: "asc",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a minimal gin context for the test
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/?sort_by="+tt.sortBy+"&sort_order="+tt.sortOrder, nil)

			sortBy, sortOrder, err := parseSort(c)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectSort, sortBy)
				assert.Equal(t, tt.expectOrder, sortOrder)
			}
		})
	}
}
