package nfo

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

// TestParseActorToActress_EdgeCases tests various combinations of name fields
func TestParseActorToActress_EdgeCases(t *testing.T) {
	tests := []struct {
		name                 string
		actor                Actor
		expectedFirstName    string
		expectedLastName     string
		expectedJapaneseName string
		description          string
	}{
		{
			name: "Standard JAV NFO: Name=Japanese, AltName=Romanized",
			actor: Actor{
				Name:    "涼森れむ",
				AltName: "Suzumori Remu",
				Role:    "Actress",
			},
			expectedFirstName:    "Suzumori",
			expectedLastName:     "Remu",
			expectedJapaneseName: "涼森れむ",
			description:          "Most common format - use AltName for romanized, Name for Japanese",
		},
		{
			name: "Name=Romanized, Role=Japanese (no AltName)",
			actor: Actor{
				Name: "Yui Hatano",
				Role: "波多野結衣",
			},
			expectedFirstName:    "Yui",
			expectedLastName:     "Hatano",
			expectedJapaneseName: "波多野結衣",
			description:          "Use romanized Name for FirstName/LastName when AltName empty",
		},
		{
			name: "Name=Romanized, AltName=Romanized (different order), Role=Japanese",
			actor: Actor{
				Name:    "Yui Hatano",
				AltName: "Hatano Yui",
				Role:    "波多野結衣",
			},
			expectedFirstName:    "Hatano",
			expectedLastName:     "Yui",
			expectedJapaneseName: "波多野結衣",
			description:          "Prefer AltName over Name for romanized when both present",
		},
		{
			name: "Name=Japanese only (no AltName, no Role)",
			actor: Actor{
				Name: "涼森れむ",
			},
			expectedFirstName:    "",
			expectedLastName:     "",
			expectedJapaneseName: "涼森れむ",
			description:          "Japanese Name not used for FirstName/LastName (only for JapaneseName)",
		},
		{
			name: "Name=Romanized only (no AltName, no Role)",
			actor: Actor{
				Name: "Yui Hatano",
			},
			expectedFirstName:    "Yui",
			expectedLastName:     "Hatano",
			expectedJapaneseName: "",
			description:          "Romanized Name used for FirstName/LastName when no AltName",
		},
		{
			name: "AltName and Role both present, Name=Japanese",
			actor: Actor{
				Name:    "涼森れむ",
				AltName: "Remu Suzumori",
				Role:    "涼森 れむ", // With space
			},
			expectedFirstName:    "Remu",
			expectedLastName:     "Suzumori",
			expectedJapaneseName: "涼森 れむ", // Role takes priority over Name
			description:          "Role takes priority over Name for JapaneseName",
		},
		{
			name: "Empty AltName, Name=Romanized with trailing space",
			actor: Actor{
				Name: "Yui Hatano ",
				Role: "波多野結衣",
			},
			expectedFirstName:    "Yui",
			expectedLastName:     "Hatano",
			expectedJapaneseName: "波多野結衣",
			description:          "Whitespace should be handled correctly",
		},
		{
			name: "Single romanized name (no last name)",
			actor: Actor{
				Name: "Madonna",
				Role: "マドンナ",
			},
			expectedFirstName:    "Madonna",
			expectedLastName:     "",
			expectedJapaneseName: "マドンナ",
			description:          "Single name should work (FirstName only)",
		},
		{
			name: "REVERSE FORMAT: Name=Romanized, AltName=Japanese",
			actor: Actor{
				Name:    "Yui Hatano",
				AltName: "波多野結衣",
			},
			expectedFirstName:    "Yui",
			expectedLastName:     "Hatano",
			expectedJapaneseName: "波多野結衣",
			description:          "Handle reverse format where AltName has Japanese",
		},
		{
			name: "REVERSE FORMAT: Name=Romanized, AltName=Japanese, Role=Japanese",
			actor: Actor{
				Name:    "Remu Suzumori",
				AltName: "涼森れむ",
				Role:    "涼森 れむ", // Role takes priority
			},
			expectedFirstName:    "Remu",
			expectedLastName:     "Suzumori",
			expectedJapaneseName: "涼森 れむ", // Role has priority over AltName
			description:          "Role takes priority even when AltName is Japanese",
		},
		{
			name: "Both Name and AltName are Japanese",
			actor: Actor{
				Name:    "涼森れむ",
				AltName: "涼森 れむ", // With space
			},
			expectedFirstName:    "",
			expectedLastName:     "",
			expectedJapaneseName: "涼森 れむ", // AltName takes priority in our logic
			description:          "When both Japanese, AltName used for JapaneseName, neither for romanized",
		},
		{
			name: "Both Name and AltName are Romanized",
			actor: Actor{
				Name:    "Yui Hatano",
				AltName: "Hatano Yui",
			},
			expectedFirstName:    "Hatano",
			expectedLastName:     "Yui",
			expectedJapaneseName: "",
			description:          "AltName preferred when both romanized (neither for Japanese)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actress := parseActorToActress(tt.actor)

			assert.Equal(t, tt.expectedFirstName, actress.FirstName,
				"FirstName mismatch: %s", tt.description)
			assert.Equal(t, tt.expectedLastName, actress.LastName,
				"LastName mismatch: %s", tt.description)
			assert.Equal(t, tt.expectedJapaneseName, actress.JapaneseName,
				"JapaneseName mismatch: %s", tt.description)
		})
	}
}

// TestActressDeduplication_EdgeCases tests deduplication with various name combinations
func TestActressDeduplication_EdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		scraped      models.Actress
		nfo          models.Actress
		shouldDedupe bool
		description  string
	}{
		{
			name: "Same JapaneseName, different romanizations",
			scraped: models.Actress{
				FirstName:    "Remu",
				LastName:     "Suzumori",
				JapaneseName: "涼森れむ",
				DMMID:        1051912,
			},
			nfo: models.Actress{
				FirstName:    "Suzumori",
				LastName:     "Remu",
				JapaneseName: "涼森れむ",
			},
			shouldDedupe: true,
			description:  "JapaneseName priority ensures deduplication despite name order",
		},
		{
			name: "No JapaneseName, same DMMID",
			scraped: models.Actress{
				FirstName: "Yui",
				LastName:  "Hatano",
				DMMID:     123456,
			},
			nfo: models.Actress{
				FirstName: "Hatano",
				LastName:  "Yui",
				DMMID:     123456,
			},
			shouldDedupe: true,
			description:  "DMMID deduplication works without JapaneseName",
		},
		{
			name: "No JapaneseName, no DMMID, same romanized name",
			scraped: models.Actress{
				FirstName: "Yui",
				LastName:  "Hatano",
			},
			nfo: models.Actress{
				FirstName: "Yui",
				LastName:  "Hatano",
			},
			shouldDedupe: true,
			description:  "Romanized name deduplication (exact match)",
		},
		{
			name: "No JapaneseName, no DMMID, different romanized order",
			scraped: models.Actress{
				FirstName: "Yui",
				LastName:  "Hatano",
			},
			nfo: models.Actress{
				FirstName: "Hatano",
				LastName:  "Yui",
			},
			shouldDedupe: false,
			description:  "Without JapaneseName/DMMID, different name order creates duplicate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key1 := actressKey(tt.scraped)
			key2 := actressKey(tt.nfo)

			if tt.shouldDedupe {
				assert.Equal(t, key1, key2, "Keys should match for deduplication: %s", tt.description)
			} else {
				assert.NotEqual(t, key1, key2, "Keys should differ (no deduplication): %s", tt.description)
			}
		})
	}
}
