package apperrors

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorCode_Uniqueness(t *testing.T) {
	codes := []ErrorCode{
		CodeAllowedDirsEmpty,
		CodePathOutsideAllowed,
		CodePathInDenylist,
		CodePathNotExist,
		CodePathNotDir,
		CodePathInvalid,
		CodePathUnresolvable,
		CodeUNCPathBlocked,
		CodeReservedDeviceName,
	}

	seen := make(map[ErrorCode]bool)
	for _, code := range codes {
		assert.False(t, seen[code], "Duplicate error code: %s", code)
		seen[code] = true
	}
}

func TestPathError_ErrorInterface(t *testing.T) {
	err := ErrPathOutsideAllowed
	assert.Equal(t, err.Message, err.Error())
}

func TestPathError_HTTPStatus(t *testing.T) {
	tests := []struct {
		name     string
		err      *PathError
		expected int
	}{
		{"AllowedDirsEmpty", ErrAllowedDirsEmpty, http.StatusForbidden},
		{"PathOutsideAllowed", ErrPathOutsideAllowed, http.StatusForbidden},
		{"PathInDenylist", ErrPathInDenylist, http.StatusForbidden},
		{"PathNotExist", ErrPathNotExist, http.StatusBadRequest},
		{"PathNotDir", ErrPathNotDir, http.StatusBadRequest},
		{"PathInvalid", ErrPathInvalid, http.StatusBadRequest},
		{"PathUnresolvable", ErrPathUnresolvable, http.StatusBadRequest},
		{"UNCPathBlocked", ErrUNCPathBlocked, http.StatusForbidden},
		{"ReservedDeviceName", ErrReservedDeviceName, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.HTTPStatus)
		})
	}
}

func TestPathError_BackwardCompatibility(t *testing.T) {
	tests := []struct {
		name        string
		err         *PathError
		mustContain []string
	}{
		{
			name:        "ErrPathOutsideAllowed",
			err:         ErrPathOutsideAllowed,
			mustContain: []string{"access denied", "outside allowed directories"},
		},
		{
			name:        "ErrPathInDenylist",
			err:         ErrPathInDenylist,
			mustContain: []string{"access denied", "system directory"},
		},
		{
			name:        "ErrPathNotExist",
			err:         ErrPathNotExist,
			mustContain: []string{"does not exist"},
		},
		{
			name:        "ErrPathNotDir",
			err:         ErrPathNotDir,
			mustContain: []string{"not a directory"},
		},
		{
			name:        "ErrAllowedDirsEmpty",
			err:         ErrAllowedDirsEmpty,
			mustContain: []string{"access denied", "allowed directories"},
		},
		{
			name:        "ErrPathInvalid",
			err:         ErrPathInvalid,
			mustContain: []string{"cannot access path"},
		},
		{
			name:        "ErrPathUnresolvable",
			err:         ErrPathUnresolvable,
			mustContain: []string{"cannot resolve"},
		},
		{
			name:        "ErrUNCPathBlocked",
			err:         ErrUNCPathBlocked,
			mustContain: []string{"UNC paths are not allowed"},
		},
		{
			name:        "ErrReservedDeviceName",
			err:         ErrReservedDeviceName,
			mustContain: []string{"reserved device name"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, substr := range tt.mustContain {
				assert.Contains(t, tt.err.Error(), substr,
					"Error message must contain '%s' for backward compatibility", substr)
			}
		})
	}
}

func TestPathError_ErrorsIs(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		target   error
		expected bool
	}{
		{"ErrPathOutsideAllowed matches itself", ErrPathOutsideAllowed, ErrPathOutsideAllowed, true},
		{"ErrPathNotExist matches itself", ErrPathNotExist, ErrPathNotExist, true},
		{"ErrPathOutsideAllowed does not match ErrPathNotExist", ErrPathOutsideAllowed, ErrPathNotExist, false},
		{"NewPathError matches base error", NewPathError(ErrPathOutsideAllowed, "/test"), ErrPathOutsideAllowed, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, errors.Is(tt.err, tt.target))
		})
	}
}

func TestNewPathError(t *testing.T) {
	base := ErrPathOutsideAllowed
	path := "/some/test/path"

	err := NewPathError(base, path)

	assert.Equal(t, base.Code, err.Code)
	assert.Equal(t, base.Message, err.Message)
	assert.Equal(t, base.HTTPStatus, err.HTTPStatus)
	assert.Equal(t, path, err.Path)
	assert.True(t, errors.Is(err, base))
}

func TestPathError_Is_Method(t *testing.T) {
	tests := []struct {
		name     string
		err      *PathError
		target   error
		expected bool
	}{
		{
			name:     "same error code matches",
			err:      ErrPathOutsideAllowed,
			target:   ErrPathOutsideAllowed,
			expected: true,
		},
		{
			name:     "different error code does not match",
			err:      ErrPathOutsideAllowed,
			target:   ErrPathNotExist,
			expected: false,
		},
		{
			name:     "non-PathError target does not match",
			err:      ErrPathOutsideAllowed,
			target:   errors.New("generic error"),
			expected: false,
		},
		{
			name:     "NewPathError matches base error",
			err:      NewPathError(ErrPathNotExist, "/missing"),
			target:   ErrPathNotExist,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, errors.Is(tt.err, tt.target))
		})
	}
}

func TestPathError_OperatorMessage(t *testing.T) {
	tests := []struct {
		name                string
		err                 *PathError
		hasOperatorMessage  bool
		operatorMsgContains string
	}{
		{
			name:                "ErrAllowedDirsEmpty has operator guidance",
			err:                 ErrAllowedDirsEmpty,
			hasOperatorMessage:  true,
			operatorMsgContains: "configuration file",
		},
		{
			name:                "ErrPathOutsideAllowed has operator guidance",
			err:                 ErrPathOutsideAllowed,
			hasOperatorMessage:  true,
			operatorMsgContains: "allowed directory",
		},
		{
			name:                "ErrPathInDenylist has operator guidance",
			err:                 ErrPathInDenylist,
			hasOperatorMessage:  true,
			operatorMsgContains: "denylist",
		},
		{
			name:                "ErrPathNotExist has operator guidance",
			err:                 ErrPathNotExist,
			hasOperatorMessage:  true,
			operatorMsgContains: "Verify",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NotEmpty(t, tt.err.OperatorMessage, "OperatorMessage should not be empty")
			if tt.operatorMsgContains != "" {
				assert.Contains(t, tt.err.OperatorMessage, tt.operatorMsgContains)
			}
		})
	}
}

func TestPathError_DocsURL(t *testing.T) {
	tests := []struct {
		name       string
		err        *PathError
		hasDocsURL bool
		docsURL    string
	}{
		{
			name:       "ErrAllowedDirsEmpty has docs URL",
			err:        ErrAllowedDirsEmpty,
			hasDocsURL: true,
			docsURL:    "/docs/configuration#security",
		},
		{
			name:       "ErrPathOutsideAllowed has docs URL",
			err:        ErrPathOutsideAllowed,
			hasDocsURL: true,
			docsURL:    "/docs/configuration#security",
		},
		{
			name:       "ErrPathInDenylist has no docs URL",
			err:        ErrPathInDenylist,
			hasDocsURL: false,
			docsURL:    "",
		},
		{
			name:       "ErrPathNotExist has no docs URL",
			err:        ErrPathNotExist,
			hasDocsURL: false,
			docsURL:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.hasDocsURL {
				assert.NotEmpty(t, tt.err.DocsURL)
				assert.Equal(t, tt.docsURL, tt.err.DocsURL)
			} else {
				assert.Empty(t, tt.err.DocsURL)
			}
		})
	}
}

func TestNewPathError_PreservesAllFields(t *testing.T) {
	base := ErrAllowedDirsEmpty
	path := "/test/path"

	err := NewPathError(base, path)

	assert.Equal(t, base.Code, err.Code)
	assert.Equal(t, base.Message, err.Message)
	assert.Equal(t, base.OperatorMessage, err.OperatorMessage)
	assert.Equal(t, base.HTTPStatus, err.HTTPStatus)
	assert.Equal(t, base.DocsURL, err.DocsURL)
	assert.Equal(t, path, err.Path)
}

func TestErrorCode_String(t *testing.T) {
	tests := []struct {
		code     ErrorCode
		expected string
	}{
		{CodeAllowedDirsEmpty, "ALLOWED_DIRS_EMPTY"},
		{CodePathOutsideAllowed, "PATH_OUTSIDE_ALLOWED_DIRS"},
		{CodePathInDenylist, "PATH_IN_DENYLIST"},
		{CodePathNotExist, "PATH_NOT_EXIST"},
		{CodePathNotDir, "PATH_NOT_DIR"},
		{CodePathInvalid, "PATH_INVALID"},
		{CodePathUnresolvable, "PATH_UNRESOLVABLE"},
		{CodeUNCPathBlocked, "UNC_PATH_BLOCKED"},
		{CodeReservedDeviceName, "RESERVED_DEVICE_NAME"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.code))
		})
	}
}
