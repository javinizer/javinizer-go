package models

import (
	"testing"
)

// TestActressFullName tests the Actress.FullName() method
func TestActressFullName(t *testing.T) {
	tests := []struct {
		name     string
		actress  Actress
		expected string
	}{
		{
			name: "both first and last names",
			actress: Actress{
				FirstName: "Yui",
				LastName:  "Hatano",
			},
			expected: "Hatano Yui",
		},
		{
			name: "first name only",
			actress: Actress{
				FirstName:    "Yui",
				JapaneseName: "波多野結衣",
			},
			expected: "Yui",
		},
		{
			name: "japanese name only",
			actress: Actress{
				JapaneseName: "波多野結衣",
			},
			expected: "波多野結衣",
		},
		{
			name: "all three names",
			actress: Actress{
				FirstName:    "Yui",
				LastName:     "Hatano",
				JapaneseName: "波多野結衣",
			},
			expected: "Hatano Yui",
		},
		{
			name:     "empty actress",
			actress:  Actress{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.actress.FullName()
			if result != tt.expected {
				t.Errorf("FullName() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestActressInfoFullName tests the ActressInfo.FullName() method
func TestActressInfoFullName(t *testing.T) {
	tests := []struct {
		name     string
		actress  ActressInfo
		expected string
	}{
		{
			name: "both first and last names",
			actress: ActressInfo{
				FirstName: "Ai",
				LastName:  "Sayama",
			},
			expected: "Sayama Ai",
		},
		{
			name: "first name only",
			actress: ActressInfo{
				FirstName:    "Ai",
				JapaneseName: "佐山愛",
			},
			expected: "Ai",
		},
		{
			name: "japanese name only",
			actress: ActressInfo{
				JapaneseName: "佐山愛",
			},
			expected: "佐山愛",
		},
		{
			name: "all three names",
			actress: ActressInfo{
				FirstName:    "Ai",
				LastName:     "Sayama",
				JapaneseName: "佐山愛",
			},
			expected: "Sayama Ai",
		},
		{
			name:     "empty actress info",
			actress:  ActressInfo{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.actress.FullName()
			if result != tt.expected {
				t.Errorf("FullName() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestTableName tests the TableName() methods
func TestTableName(t *testing.T) {
	tests := []struct {
		name     string
		model    interface{ TableName() string }
		expected string
	}{
		{"Movie", Movie{}, "movies"},
		{"MovieTranslation", MovieTranslation{}, "movie_translations"},
		{"Actress", Actress{}, "actresses"},
		{"Genre", Genre{}, "genres"},
		{"MovieTag", MovieTag{}, "movie_tags"},
		{"History", History{}, "history"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.model.TableName()
			if result != tt.expected {
				t.Errorf("%s.TableName() = %q, want %q", tt.name, result, tt.expected)
			}
		})
	}
}
