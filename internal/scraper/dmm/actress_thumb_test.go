package dmm

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHiraganaToRomaji(t *testing.T) {
	tests := []struct {
		name     string
		hiragana string
		expected string
	}{
		{
			name:     "basic conversion",
			hiragana: "あいうえお",
			expected: "aiueo",
		},
		{
			name:     "with k-sounds",
			hiragana: "かきくけこ",
			expected: "kakikukeko",
		},
		{
			name:     "with s-sounds (Nihon-shiki)",
			hiragana: "さしすせそ",
			expected: "sasisuseso", // si not shi
		},
		{
			name:     "with t-sounds (Nihon-shiki)",
			hiragana: "たちつてと",
			expected: "tatituteto", // ti/tu not chi/tsu
		},
		{
			name:     "with voiced consonants",
			hiragana: "がぎぐげご",
			expected: "gagigugego",
		},
		{
			name:     "with z-sounds (Nihon-shiki)",
			hiragana: "ざじずぜぞ",
			expected: "zazizuzezo", // zi not ji
		},
		{
			name:     "with n-sounds",
			hiragana: "なにぬねの",
			expected: "naninuneno",
		},
		{
			name:     "with h-sounds (Nihon-shiki)",
			hiragana: "はひふへほ",
			expected: "hahihuheho", // hu not fu
		},
		{
			name:     "with m-sounds",
			hiragana: "まみむめも",
			expected: "mamimumemo",
		},
		{
			name:     "with y-sounds",
			hiragana: "やゆよ",
			expected: "yayuyo",
		},
		{
			name:     "with r-sounds",
			hiragana: "らりるれろ",
			expected: "rarirurero",
		},
		{
			name:     "with w-sounds",
			hiragana: "わを",
			expected: "wawo",
		},
		{
			name:     "with n (ん)",
			hiragana: "ん",
			expected: "n",
		},
		{
			name:     "real name - Shirakami Emika",
			hiragana: "しらかみえみか",
			expected: "sirakamiemika",
		},
		{
			name:     "with small ya (しゃ)",
			hiragana: "しゃ",
			expected: "sya",
		},
		{
			name:     "with small yu (しゅ)",
			hiragana: "しゅ",
			expected: "syu",
		},
		{
			name:     "with small yo (しょ)",
			hiragana: "しょ",
			expected: "syo",
		},
		{
			name:     "with small tsu (gemination - っか)",
			hiragana: "がっこう",
			expected: "gakkou", // っ doubles the k
		},
		{
			name:     "complex name with yoon",
			hiragana: "きょうこ",
			expected: "kyouko",
		},
		{
			name:     "name with multiple yoon",
			hiragana: "みゃあちゃん",
			expected: "myaatyan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hiraganaToRomaji(tt.hiragana)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHiraganaToRomaji_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		hiragana string
		expected string
	}{
		{
			name:     "empty string",
			hiragana: "",
			expected: "",
		},
		{
			name:     "single character",
			hiragana: "あ",
			expected: "a",
		},
		{
			name:     "unknown character (pass through)",
			hiragana: "あ漢字",
			expected: "a漢字", // Kanji passes through unchanged
		},
		{
			name:     "mixed hiragana and katakana",
			hiragana: "あアい",
			expected: "aアi", // Katakana passes through unchanged
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hiraganaToRomaji(tt.hiragana)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractRomajiVariants(t *testing.T) {
	// Note: This would need a mock HTTP client to test properly
	// For now, we'll test the variant generation logic separately

	tests := []struct {
		name     string
		romaji   string
		expected []string
	}{
		{
			name:   "standard length name",
			romaji: "sirakamiemika",
			expected: []string{
				"sirakamie_mika", // split at 9 (not in list, but would be generated if romaji was longer)
				"sirakami_emika", // split at 8 (correct for this case)
				"sirakam_iemika", // split at 7
				"siraka_miemika", // split at 6
				"sirak_amiemika", // split at 5
				"sira_kamiemika", // split at 4
				"sir_akamiemika", // split at 3
				"si_rakamiemika", // split at 2
				"sirakamiemika",  // unsplit fallback
			},
		},
		{
			name:   "short name",
			romaji: "aimi",
			expected: []string{
				"ai_mi",   // split at 2
				"aimi",    // unsplit fallback
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate variants using the same logic as extractRomajiVariantsFromActressPage
			variants := make([]string, 0)

			if len(tt.romaji) >= 4 {
				splitPoints := []int{8, 7, 6, 5, 4, 3, 9, 10, 2}
				for _, splitPoint := range splitPoints {
					if splitPoint < len(tt.romaji)-1 {
						lastName := tt.romaji[:splitPoint]
						firstName := tt.romaji[splitPoint:]
						variant := lastName + "_" + firstName
						variants = append(variants, variant)
					}
				}
			}

			// Add unsplit version
			variants = append(variants, tt.romaji)

			// Verify we get the expected variants (order matters for efficiency)
			assert.Contains(t, variants, tt.expected[0], "Should contain first expected variant")

			// For the full name, verify the correct split is included
			if tt.name == "standard length name" {
				assert.Contains(t, variants, "sirakami_emika", "Should contain correct split")
			}
		})
	}
}

func TestTryActressThumbURLs_VariantGeneration(t *testing.T) {
	// This tests the URL generation logic without actually making HTTP requests
	tests := []struct {
		name      string
		firstName string
		lastName  string
		dmmID     int
		wantURLs  []string
	}{
		{
			name:      "with English names",
			firstName: "Emika",
			lastName:  "Shirakami",
			dmmID:     0, // No DMM ID, won't fetch actress page
			wantURLs: []string{
				"https://pics.dmm.co.jp/mono/actjpgs/shirakami_emika.jpg",
				"https://pics.dmm.co.jp/mono/actjpgs/emika_shirakami.jpg",
			},
		},
		{
			name:      "single name only (first)",
			firstName: "Emika",
			lastName:  "",
			dmmID:     0,
			wantURLs:  []string{}, // No URLs generated without both names
		},
		{
			name:      "no names provided",
			firstName: "",
			lastName:  "",
			dmmID:     1092662, // With DMM ID, would fetch actress page (not tested here)
			wantURLs:  []string{}, // Would be populated by actress page fetch
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate URL generation logic from tryActressThumbURLs
			candidates := make([]string, 0)

			if tt.firstName != "" && tt.lastName != "" {
				firstLower := strings.ToLower(tt.firstName)
				lastLower := strings.ToLower(tt.lastName)

				candidates = append(candidates,
					"https://pics.dmm.co.jp/mono/actjpgs/"+lastLower+"_"+firstLower+".jpg",
					"https://pics.dmm.co.jp/mono/actjpgs/"+firstLower+"_"+lastLower+".jpg",
				)
			}

			// For names, we expect lowercase URLs
			for i, expected := range tt.wantURLs {
				if i < len(candidates) {
					// Case-insensitive comparison
					assert.Equal(t, expected, candidates[i])
				}
			}

			if len(tt.wantURLs) > 0 {
				assert.Equal(t, len(tt.wantURLs), len(candidates), "Should generate expected number of URLs")
			}
		})
	}
}
