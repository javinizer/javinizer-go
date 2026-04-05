package template

import (
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTemplateConcurrentRendering(t *testing.T) {
	engine := NewEngineWithOptions(EngineOptions{
		DefaultLanguage:   "en",
		FallbackLanguages: []string{"ja", "zh"},
	})

	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)

	ctx := &Context{
		ID:          "IPX-535",
		ContentID:   "ipx00535",
		Title:       "Base Title",
		ReleaseDate: &releaseDate,
		Runtime:     120,
		Director:    "Base Director",
		Maker:       "Base Maker",
		Label:       "Base Label",
		Series:      "Base Series",
		Actresses:   []string{"Actress1", "Actress2"},
		Genres:      []string{"Genre1", "Genre2"},
		Translations: map[string]models.MovieTranslation{
			"en": {Language: "en", Title: "English Title", Director: "English Director", Maker: "English Studio"},
			"ja": {Language: "ja", Title: "Japanese Title", Director: "Japanese Director", Maker: "Japanese Studio"},
			"zh": {Language: "zh", Title: "Chinese Title"},
		},
	}

	templates := []string{
		"<ID> - <TITLE>",
		"<ID> [<MAKER>] - <TITLE> (<YEAR>)",
		"<TITLE:ja> / <TITLE:en>",
		"<IF:SERIES><SERIES>: </IF><TITLE>",
		"<ID> - <TITLE:50>",
		"<DIRECTOR:en> presents <TITLE>",
		"<ID> [<STUDIO>] - <TITLE> (<RUNTIME>min)",
	}

	const iterations = 100
	const goroutines = 10

	var wg sync.WaitGroup
	errChan := make(chan error, goroutines*iterations)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				templateIdx := (goroutineID + i) % len(templates)
				_, err := engine.Execute(templates[templateIdx], ctx)
				if err != nil {
					errChan <- err
				}
			}
		}(g)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("Concurrent execution error: %v", err)
	}
}

func TestTemplateConcurrentRenderingDifferentContexts(t *testing.T) {
	engine := NewEngineWithOptions(EngineOptions{
		DefaultLanguage:   "en",
		FallbackLanguages: []string{"ja"},
	})

	template := "<ID> [<TITLE:en>] - <TITLE> (<YEAR>)"

	const goroutines = 20
	const iterations = 50

	var wg sync.WaitGroup
	results := make([]map[string]string, goroutines)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		results[g] = make(map[string]string)
		go func(goroutineID int, resultMap map[string]string) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				releaseDate := time.Date(2020+goroutineID, time.Month(1+i%12), 1, 0, 0, 0, 0, time.UTC)
				ctx := &Context{
					ID:          "TEST-" + string(rune('A'+goroutineID%26)) + "-" + string(rune('0'+i%10)),
					Title:       "Base Title " + string(rune('A'+goroutineID)),
					ReleaseDate: &releaseDate,
					Translations: map[string]models.MovieTranslation{
						"en": {Language: "en", Title: "English Title " + string(rune('A'+goroutineID))},
						"ja": {Language: "ja", Title: "Japanese Title " + string(rune('A'+goroutineID))},
					},
				}
				result, err := engine.Execute(template, ctx)
				if err != nil {
					t.Errorf("Error in goroutine %d iteration %d: %v", goroutineID, i, err)
					continue
				}
				resultMap[string(rune('0'+i%10))] = result
			}
		}(g, results[g])
	}

	wg.Wait()

	for g, resultMap := range results {
		assert.NotEmpty(t, resultMap, "Goroutine %d should have results", g)
	}
}

func TestContextCloneConcurrentSafety(t *testing.T) {
	original := &Context{
		ID:        "IPX-535",
		Title:     "Original Title",
		Director:  "Original Director",
		Maker:     "Original Maker",
		Actresses: []string{"A1", "A2", "A3"},
		Genres:    []string{"G1", "G2"},
		Translations: map[string]models.MovieTranslation{
			"en": {Language: "en", Title: "English Title"},
			"ja": {Language: "ja", Title: "Japanese Title"},
		},
		DefaultLanguage: "en",
	}

	const goroutines = 50
	const iterations = 20

	var wg sync.WaitGroup

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				clone := original.Clone()

				assert.Equal(t, original.ID, clone.ID)
				assert.Equal(t, original.Title, clone.Title)
				assert.Equal(t, original.Director, clone.Director)
				assert.Equal(t, original.Maker, clone.Maker)
				assert.Equal(t, original.DefaultLanguage, clone.DefaultLanguage)
				assert.Equal(t, len(original.Actresses), len(clone.Actresses))
				assert.Equal(t, len(original.Genres), len(clone.Genres))
				assert.Equal(t, len(original.Translations), len(clone.Translations))

				clone.Title = "Modified Title " + string(rune('A'+goroutineID))
				clone.DefaultLanguage = "ja"
				if len(clone.Actresses) > 0 {
					clone.Actresses[0] = "Modified Actress"
				}
				if len(clone.Genres) > 0 {
					clone.Genres[0] = "Modified Genre"
				}
				clone.Translations["new"] = models.MovieTranslation{Language: "new", Title: "New Title"}
				clone.Translations["en"] = models.MovieTranslation{Language: "en", Title: "Modified English"}

				assert.Equal(t, "Original Title", original.Title, "Original Title should not be modified")
				assert.Equal(t, "en", original.DefaultLanguage, "Original DefaultLanguage should not be modified")
				assert.Equal(t, "A1", original.Actresses[0], "Original Actresses[0] should not be modified")
				assert.Equal(t, "G1", original.Genres[0], "Original Genres[0] should not be modified")
				assert.Equal(t, 2, len(original.Translations), "Original Translations length should not change")
				assert.Equal(t, "English Title", original.Translations["en"].Title, "Original Translations[en] should not be modified")
				assert.NotContains(t, original.Translations, "new", "Original Translations should not contain 'new'")
			}
		}(g)
	}

	wg.Wait()
}

func TestConcurrentEngineWithOptions(t *testing.T) {
	const goroutines = 10

	var wg sync.WaitGroup
	engines := make([]*Engine, goroutines)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			opts := EngineOptions{
				DefaultLanguage:   "en",
				FallbackLanguages: []string{"ja", "zh"},
				MaxTemplateBytes:  1024 * 1024,
				MaxOutputBytes:    10 * 1024 * 1024,
			}
			engines[id] = NewEngineWithOptions(opts)
		}(g)
	}

	wg.Wait()

	for _, engine := range engines {
		assert.NotNil(t, engine)
	}
}

func TestConcurrentTranslationsMapAccess(t *testing.T) {
	ctx := &Context{
		Title: "Base Title",
		Translations: map[string]models.MovieTranslation{
			"en": {Language: "en", Title: "English Title"},
			"ja": {Language: "ja", Title: "Japanese Title"},
			"zh": {Language: "zh", Title: "Chinese Title"},
			"ko": {Language: "ko", Title: "Korean Title"},
			"de": {Language: "de", Title: "German Title"},
			"fr": {Language: "fr", Title: "French Title"},
		},
	}

	engine := NewEngineWithOptions(EngineOptions{
		DefaultLanguage:   "en",
		FallbackLanguages: []string{"ja", "zh"},
	})

	const goroutines = 30

	var wg sync.WaitGroup
	results := make([]string, goroutines)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			langs := []string{"en", "ja", "zh", "ko", "de", "fr"}
			langIdx := id % len(langs)
			results[id] = engine.translationFieldValue("TITLE", langs[langIdx], ctx)
		}(g)
	}

	wg.Wait()

	expectedResults := map[int]string{
		0: "English Title",
		1: "Japanese Title",
		2: "Chinese Title",
		3: "Korean Title",
		4: "German Title",
		5: "French Title",
	}

	for id, expected := range expectedResults {
		for g := id; g < goroutines; g += 6 {
			assert.Equal(t, expected, results[g], "Goroutine %d should get correct translation", g)
		}
	}
}

func TestConcurrentResolveTranslatedTag(t *testing.T) {
	ctx := &Context{
		Title:           "Base Title",
		DefaultLanguage: "ja",
		Translations: map[string]models.MovieTranslation{
			"en": {Language: "en", Title: "English Title"},
			"ja": {Language: "ja", Title: "Japanese Title"},
			"zh": {Language: "zh", Title: "Chinese Title"},
		},
	}

	engine := NewEngineWithOptions(EngineOptions{
		DefaultLanguage:   "en",
		FallbackLanguages: []string{"zh", "ko"},
	})

	const goroutines = 20

	var wg sync.WaitGroup
	results := make([]string, goroutines)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			explicitLangs := []string{"", "en", "ja", "zh", "ko", "de"}
			langIdx := id % len(explicitLangs)
			results[id] = engine.resolveTranslatedTag("TITLE", explicitLangs[langIdx], ctx)
		}(g)
	}

	wg.Wait()

	expectedResults := map[int]string{
		0: "Japanese Title",
		1: "English Title",
		2: "Japanese Title",
		3: "Chinese Title",
		4: "Japanese Title",
		5: "Japanese Title",
	}

	for id, expected := range expectedResults {
		for g := id; g < goroutines; g += 6 {
			assert.Equal(t, expected, results[g], "Goroutine %d should get correct resolved tag", g)
		}
	}
}

func TestRaceDetectorConcurrentExecution(t *testing.T) {
	t.Run("Engine Execute concurrent", func(t *testing.T) {
		engine := NewEngineWithOptions(EngineOptions{
			DefaultLanguage:   "en",
			FallbackLanguages: []string{"ja"},
		})

		ctx := &Context{
			Title: "Base Title",
			Translations: map[string]models.MovieTranslation{
				"en": {Language: "en", Title: "English Title"},
				"ja": {Language: "ja", Title: "Japanese Title"},
			},
		}

		const n = 100
		var wg sync.WaitGroup
		wg.Add(n)

		for i := 0; i < n; i++ {
			go func() {
				defer wg.Done()
				_, err := engine.Execute("<TITLE>", ctx)
				require.NoError(t, err)
			}()
		}

		wg.Wait()
	})

	t.Run("Context Clone concurrent", func(t *testing.T) {
		ctx := &Context{
			Title:     "Base Title",
			Actresses: []string{"A1", "A2"},
			Translations: map[string]models.MovieTranslation{
				"en": {Language: "en", Title: "English Title"},
			},
		}

		const n = 100
		var wg sync.WaitGroup
		wg.Add(n)

		for i := 0; i < n; i++ {
			go func() {
				defer wg.Done()
				clone := ctx.Clone()
				clone.Title = "Modified"
				clone.Actresses[0] = "Modified"
				clone.Translations["en"] = models.MovieTranslation{Language: "en", Title: "Modified"}
			}()
		}

		wg.Wait()

		assert.Equal(t, "Base Title", ctx.Title)
		assert.Equal(t, "A1", ctx.Actresses[0])
		assert.Equal(t, "English Title", ctx.Translations["en"].Title)
	})

	t.Run("languageCandidates concurrent", func(t *testing.T) {
		engine := NewEngineWithOptions(EngineOptions{
			DefaultLanguage:   "en",
			FallbackLanguages: []string{"ja", "zh"},
		})

		ctx := &Context{
			DefaultLanguage: "ja",
		}

		const n = 100
		var wg sync.WaitGroup
		wg.Add(n)

		for i := 0; i < n; i++ {
			go func(id int) {
				defer wg.Done()
				langs := []string{"", "en", "ja", "zh"}
				candidates := engine.languageCandidates(langs[id%4], ctx)
				assert.NotEmpty(t, candidates)
			}(i)
		}

		wg.Wait()
	})

	t.Run("translationFieldValue concurrent", func(t *testing.T) {
		engine := NewEngine()

		ctx := &Context{
			Translations: map[string]models.MovieTranslation{
				"en": {Language: "en", Title: "English"},
				"ja": {Language: "ja", Title: "Japanese"},
			},
		}

		const n = 100
		var wg sync.WaitGroup
		wg.Add(n)

		for i := 0; i < n; i++ {
			go func(id int) {
				defer wg.Done()
				langs := []string{"en", "ja"}
				value := engine.translationFieldValue("TITLE", langs[id%2], ctx)
				assert.NotEmpty(t, value)
			}(i)
		}

		wg.Wait()
	})
}

func BenchmarkConcurrentTemplateExecution(b *testing.B) {
	engine := NewEngineWithOptions(EngineOptions{
		DefaultLanguage:   "en",
		FallbackLanguages: []string{"ja"},
	})

	ctx := &Context{
		ID:    "IPX-535",
		Title: "Base Title",
		Translations: map[string]models.MovieTranslation{
			"en": {Language: "en", Title: "English Title"},
			"ja": {Language: "ja", Title: "Japanese Title"},
		},
	}

	template := "<ID> - <TITLE> [<TITLE:ja>]"

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := engine.Execute(template, ctx)
			if err != nil {
				b.Fatalf("Execute failed: %v", err)
			}
		}
	})
}

func BenchmarkConcurrentContextClone(b *testing.B) {
	ctx := &Context{
		Title:     "Base Title",
		Actresses: []string{"A1", "A2", "A3"},
		Genres:    []string{"G1", "G2"},
		Translations: map[string]models.MovieTranslation{
			"en": {Language: "en", Title: "English Title"},
			"ja": {Language: "ja", Title: "Japanese Title"},
		},
	}

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			clone := ctx.Clone()
			clone.Title = "Modified"
			if len(clone.Actresses) > 0 {
				clone.Actresses[0] = "Modified"
			}
		}
	})
}
