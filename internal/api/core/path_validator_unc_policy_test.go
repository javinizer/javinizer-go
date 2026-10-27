package core

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPathValidatorUNCPolicy(t *testing.T) {
	// The policy is pure string validation. Use the existing serial OS seam,
	// not ValidateDir or a real share; cleanup restores the host's currentOS.
	setWindowsOSForTest(t)

	tests := []struct {
		name    string
		path    string
		allow   bool
		servers []string
		want    bool
	}{
		{"disabled even for listed server", `\\NAS\media`, false, []string{"NAS"}, false},
		{"listed server ignores case", `\\nAs\media\movies`, true, []string{"other", "NAS"}, true},
		{"unlisted server", `\\untrusted\media`, true, []string{"NAS"}, false},
		{"server name prefix is not a match", `\\NAS-extra\media`, true, []string{"NAS"}, false},
		{"empty server allowlist", `\\NAS\media`, true, nil, false},
		{"server without share separator", `\\NAS`, true, []string{"NAS"}, false},
		{"bare UNC prefix", `\\`, true, []string{"NAS"}, false},
		{"Win32 namespace listed", `\\?\uNc\nAs\media`, true, []string{"NAS"}, true},
		{"NT namespace listed", `\??\UnC\nAs\media`, true, []string{"NAS"}, true},
		{"device namespace listed", `\\.\unC\nAs\media`, true, []string{"NAS"}, true},
		{"Win32 namespace unlisted", `\\?\UNC\untrusted\media`, true, []string{"NAS"}, false},
		{"NT namespace unlisted", `\??\UNC\untrusted\media`, true, []string{"NAS"}, false},
		{"device namespace unlisted", `\\.\UNC\untrusted\media`, true, []string{"NAS"}, false},
		{"Win32 namespace disabled", `\\?\UNC\NAS\media`, false, []string{"NAS"}, false},
		{"NT namespace disabled", `\??\UNC\NAS\media`, false, []string{"NAS"}, false},
		{"device namespace disabled", `\\.\UNC\NAS\media`, false, []string{"NAS"}, false},
		{"local drive unaffected when disabled", `C:\media`, false, nil, true},
		{"relative path unaffected when disabled", `media\movies`, false, nil, true},
		{"local drive unaffected by empty server allowlist", `C:\media`, true, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// No filesystem is needed or provided: the public policy method
			// must not stat, canonicalize, or connect to any of these paths.
			validator := NewPathValidatorWithUNC(nil, nil, nil, tt.allow, tt.servers)
			require.Equal(t, tt.want, validator.IsUNCAllowed(tt.path))
		})
	}
}
