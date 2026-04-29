package configutil

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestUserAgentString_MarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		ua   UserAgentString
		want string
	}{
		{
			name: "plain string value returns quoted string",
			ua:   UserAgentString{Value: "Mozilla/5.0"},
			want: `"Mozilla/5.0"`,
		},
		{
			name: "empty value",
			ua:   UserAgentString{Value: ""},
			want: `""`,
		},
		{
			name: "complex user agent",
			ua:   UserAgentString{Value: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"},
			want: `"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.ua.MarshalJSON()
			assert.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func TestUserAgentString_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    UserAgentString
		wantErr bool
	}{
		{
			name:    "accepts plain string",
			data:    `"Mozilla/5.0"`,
			want:    UserAgentString{Value: "Mozilla/5.0"},
			wantErr: false,
		},
		{
			name:    "accepts object form with value field",
			data:    `{"value": "Mozilla/5.0"}`,
			want:    UserAgentString{Value: "Mozilla/5.0"},
			wantErr: false,
		},
		{
			name:    "rejects invalid JSON",
			data:    `{invalid}`,
			want:    UserAgentString{},
			wantErr: true,
		},
		{
			name:    "rejects array",
			data:    `[1, 2, 3]`,
			want:    UserAgentString{},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got UserAgentString
			err := got.UnmarshalJSON([]byte(tt.data))
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestUserAgentString_MarshalYAML(t *testing.T) {
	tests := []struct {
		name string
		ua   UserAgentString
		want interface{}
	}{
		{
			name: "returns plain string value",
			ua:   UserAgentString{Value: "Mozilla/5.0"},
			want: "Mozilla/5.0",
		},
		{
			name: "empty value",
			ua:   UserAgentString{Value: ""},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.ua.MarshalYAML()
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUserAgentString_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    UserAgentString
		wantErr bool
	}{
		{
			name:    "accepts plain string",
			data:    `"Mozilla/5.0"`,
			want:    UserAgentString{Value: "Mozilla/5.0"},
			wantErr: false,
		},
		{
			name:    "accepts plain string without quotes",
			data:    "Mozilla/5.0",
			want:    UserAgentString{Value: "Mozilla/5.0"},
			wantErr: false,
		},
		{
			name:    "accepts alphanumeric string",
			data:    "abc123",
			want:    UserAgentString{Value: "abc123"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got UserAgentString
			err := got.UnmarshalYAML(func(v interface{}) error {
				return yaml.Unmarshal([]byte(tt.data), v)
			})
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestUserAgentString_Scan(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		want    UserAgentString
		wantErr bool
	}{
		{
			name:    "scan string value",
			value:   "Mozilla/5.0",
			want:    UserAgentString{Value: "Mozilla/5.0"},
			wantErr: false,
		},
		{
			name:    "scan []byte value",
			value:   []byte("Mozilla/5.0"),
			want:    UserAgentString{Value: "Mozilla/5.0"},
			wantErr: false,
		},
		{
			name:    "scan nil returns empty string",
			value:   nil,
			want:    UserAgentString{Value: ""},
			wantErr: false,
		},
		{
			name:    "scan other type uses fmt.Sprintf",
			value:   12345,
			want:    UserAgentString{Value: "12345"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got UserAgentString
			err := got.Scan(tt.value)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestDefaultUserAgentConstants(t *testing.T) {
	assert.Equal(t, "Javinizer (+https://github.com/javinizer/Javinizer)", DefaultUserAgent)
	assert.Equal(t, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36", DefaultScraperUserAgent)
}

func TestUserAgentString_JSONRoundTrip(t *testing.T) {
	original := UserAgentString{Value: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"}

	// Marshal to JSON
	data, err := json.Marshal(original)
	assert.NoError(t, err)

	// Unmarshal back
	var got UserAgentString
	err = json.Unmarshal(data, &got)
	assert.NoError(t, err)
	assert.Equal(t, original, got)
}
