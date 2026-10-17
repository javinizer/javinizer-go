package workflow

import (
	"context"
	"fmt"
	"github.com/javinizer/javinizer-go/internal/aggregator"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

type aliasEvidenceSource struct {
	result *models.ScraperResult
	calls  int
}

func (s *aliasEvidenceSource) Name() string    { return "refresh-local" }
func (s *aliasEvidenceSource) IsEnabled() bool { return true }
func (s *aliasEvidenceSource) Config() *models.ScraperSettings {
	return &models.ScraperSettings{Enabled: true}
}
func (s *aliasEvidenceSource) Close() error                                   { return nil }
func (s *aliasEvidenceSource) GetURL(context.Context, string) (string, error) { return "", nil }
func (s *aliasEvidenceSource) Search(context.Context, string) (*models.ScraperResult, error) {
	s.calls++
	return s.result, nil
}

func TestAliasCreditEvidence(t *testing.T) {
	for _, useCredited := range []bool{false, true} {
		for _, convert := range []bool{false, true} {
			for _, dmm := range []int{0, 98201} {
				t.Run(fmt.Sprintf("credited_%t_convert_%t_dmm_%d", useCredited, convert, dmm), func(t *testing.T) {
					db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "alias.db"), LogLevel: "silent"})
					require.NoError(t, err)
					t.Cleanup(func() { _ = db.Close() })
					require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
					aliasRepo := database.NewActressAliasRepository(db)
					require.NoError(t, aliasRepo.Create(t.Context(), &models.ActressAlias{AliasName: "Reported Person", CanonicalName: "Canonical Person"}))
					source := &aliasEvidenceSource{result: &models.ScraperResult{ID: "ALIAS-101", ContentID: "alias00101", Title: "Alias evidence", Source: "refresh-local", Actresses: []models.ActressInfo{{DMMID: dmm, FirstName: "Person", LastName: "Reported", ThumbURL: "https://fixture.invalid/reported.jpg"}}}}
					registry := scraperutil.NewScraperRegistry()
					registry.RegisterInstance(source)
					app := config.DefaultConfig(nil, nil)
					app.Metadata.ActressDatabase.Enabled = true
					app.Metadata.ActressDatabase.ConvertAlias = convert
					metadata := aggregator.MetadataConfigFromApp(&app.Metadata)
					agg := aggregator.New(&aggregator.Config{Metadata: metadata, ScrapersPriority: []string{source.Name()}}, nil, nil, aggregator.NewAliasResolver(metadata, aliasRepo))
					beforeInfo := source.result.Actresses[0]
					aggregated, _, aggregateErr := agg.Aggregate([]*models.ScraperResult{source.result})
					require.NoError(t, aggregateErr)
					require.Equal(t, beforeInfo, source.result.Actresses[0], "aggregation must preserve original evidence")
					wantCanonical := "Reported Person"
					if convert {
						wantCanonical = "Canonical Person"
					}
					require.Equal(t, wantCanonical, aggregated.Actresses[0].FullName())
					repo := database.NewMovieRepository(db)
					engine := scrape.New(registry, agg, database.NewActressRepository(db), repo, nil, &scrape.Config{ScrapersPriority: []string{source.Name()}, ActressDBEnabled: true, ScrapeActress: true}, nil, nil)
					wf := &Workflow{scrape: newScrapeOrchestrator(engine, repo, "<ACTORS>", template.NewEngine(), nfo.NFONameConfig{UseCreditedName: useCredited}, nil)}
					result, meta, err := wf.Scrape(t.Context(), scrape.ScrapeCmd{MovieID: "ALIAS-101"})
					require.NoError(t, err)
					require.True(t, meta.Persisted)
					require.Len(t, result.Movie.Credits, 1)
					credit := result.Movie.Credits[0]
					t.Logf("converted=%t DMM=%d canonical=%q credited=%q source=%q title=%q", convert, dmm, credit.Actress.FullName(), credit.CreditedName, credit.Source, result.Movie.DisplayTitle)
					assert.Equal(t, "Reported Person", credit.CreditedName, "reported credit should survive optional alias display conversion")
					assert.Equal(t, source.Name(), credit.Source, "credit should retain its known source")
					wantTitle := wantCanonical
					if useCredited {
						wantTitle = "Reported Person"
					}
					assert.Equal(t, wantTitle, result.Movie.DisplayTitle, "display preference must not destroy underlying credit evidence")
					require.Equal(t, beforeInfo, source.result.Actresses[0], "pipeline must preserve entire original source actor")
					assert.Equal(t, "https://fixture.invalid/reported.jpg", credit.ReportedThumbURL)
					// Raw source control verifies evidence was present and the source was really called.
					require.Equal(t, 1, source.calls)
					require.Equal(t, "Reported", result.ScraperResults[0].Actresses[0].LastName)
					loaded, err := repo.FindByID(t.Context(), "ALIAS-101")
					require.NoError(t, err)
					require.Equal(t, credit.CreditedName, loaded.Credits[0].CreditedName)
					require.Equal(t, credit.Source, loaded.Credits[0].Source)
					transientSource, _, _ := loaded.Credits[0].Actress.ScrapeEvidence()
					require.Empty(t, transientSource, "raw evidence must not persist on canonical identities")
					forced := loaded.Credits[0]
					forced.DisplayForceCanonical = true
					require.Equal(t, wantCanonical, forced.RenderName(true, forced.Actress.FullName()))
					forced.UserOverride, forced.OverrideName = true, "Chosen Credit"
					require.Equal(t, "Chosen Credit", forced.RenderName(useCredited, forced.Actress.FullName()))
				})
			}
		}
	}

}
