package template

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeFilename_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "All special characters",
			input: `/:*?"<>|\`,
			want:  "- -'()--",
		},
		{
			name:  "Unicode emoji",
			input: "Test Movie 🎬",
			want:  "Test Movie 🎬",
		},
		{
			name:  "Mixed Japanese and special chars",
			input: `映画タイトル: "テスト" <2023>`,
			want:  "映画タイトル - 'テスト' (2023)",
		},
		{
			name:  "Leading and trailing special chars",
			input: "...Test Movie...",
			want:  "Test Movie",
		},
		{
			name:  "Multiple colons",
			input: "Test: Movie: Title: 2023",
			want:  "Test - Movie - Title - 2023",
		},
		{
			name:  "Windows reserved characters",
			input: `CON|PRN|AUX|NUL`,
			want:  "CON-PRN-AUX-NUL",
		},
		{
			name:  "Tab and newline characters (non-printable removed)",
			input: "Test\tMovie\nTitle",
			want:  "TestMovieTitle",
		},
		{
			name:  "Only spaces and dots",
			input: " . . . ",
			want:  "",
		},
		{
			name:  "Very long filename with special chars",
			input: "This/Is\\A:Very*Long?Filename\"With<Many>Special|Characters",
			want:  "This-Is-A -VeryLongFilename'With(Many)Special-Characters",
		},
		{
			name:  "Null byte should be removed (non-printable)",
			input: "Test\x00Movie",
			want:  "TestMovie",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeFilename(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSanitizeFolderPath_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "Convert multiple forward slashes to underscores",
			input: "Test//Folder///Path",
			want:  "Test__Folder___Path",
		},
		{
			name:  "Complex Windows path",
			input: `D:\Movies\JAV\IPX-535: "Test Movie" <1080p>`,
			want:  `D -_Movies_JAV_IPX-535 - 'Test Movie' (1080p)`,
		},
		{
			name:  "Network path",
			input: `\\server\share\folder`,
			want:  `__server_share_folder`,
		},
		{
			name:  "Mixed slashes with trailing slash",
			input: `Test\Folder/SubFolder\`,
			want:  `Test_Folder_SubFolder_`,
		},
		{
			name:  "Root path only",
			input: "/",
			want:  "_",
		},
		{
			name:  "Backslash only",
			input: "\\",
			want:  "_",
		},
		{
			name:  "Trailing dots are removed for Windows compatibility",
			input: "Movie Title...",
			want:  "Movie Title",
		},
		{
			name:  "Trailing spaces and dots are trimmed",
			input: " . Movie Title . ",
			want:  " . Movie Title",
		},
		{
			name:  "Japanese folder names",
			input: `映画\テスト\フォルダ`,
			want:  `映画_テスト_フォルダ`,
		},
		{
			name:  "Path with query-like special chars",
			input: `Test/Folder?param=value&other=test`,
			want:  `Test_Folderparam=value&other=test`,
		},
		{
			name:  "Path with multiple colons",
			input: `C:\Test: Movie: 2023`,
			want:  `C -_Test - Movie - 2023`,
		},
		{
			name:  "Null byte in path (non-printable)",
			input: "Test\x00/Folder",
			want:  "Test_Folder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeFolderPath(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
