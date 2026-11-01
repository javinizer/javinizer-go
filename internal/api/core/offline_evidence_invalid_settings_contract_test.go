package core_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scrapeevidence"
	"github.com/stretchr/testify/require"
)

// Manually wired runtimes initialize their read-only service lazily. The public
// boundary must reject invalid startup settings even without full bootstrap.
func TestOfflineEvidenceInvalidStartupSettingsFailClosedWithoutIO(t *testing.T) {
	for _, invalid := range []string{"relative directory", "invalid maximum age"} {
		t.Run(invalid, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			artifact := filepath.Join(root, "unprovisioned")
			cfg := config.DefaultConfig(nil, nil)
			cfg.Metadata.OfflineScrapeEvidence = config.OfflineScrapeEvidenceConfig{Enabled: true, ArtifactDirectory: artifact, MaxAgeHours: 168}
			if invalid == "relative directory" {
				cfg.Metadata.OfflineScrapeEvidence.ArtifactDirectory = "unprovisioned-relative-evidence"
			} else {
				cfg.Metadata.OfflineScrapeEvidence.MaxAgeHours = 0
			}
			require.Error(t, config.ValidateOfflineScrapeEvidence(cfg.Metadata.OfflineScrapeEvidence))
			before := cfg.Clone()
			rt := core.NewAPIRuntime(&core.APIDeps{})
			rt.SetConfig(cfg)
			t.Cleanup(rt.Shutdown)
			require.Nil(t, rt.OfflineScrapeEvidence())
			ctx, release, err := rt.BeginOfflineScrapeEvidence(t.Context(), true)
			require.NoError(t, err)
			t.Cleanup(release)
			require.Equal(t, scrapeevidence.Unavailable, scrapeevidence.Resolve(ctx, nil, false).Outcome)
			release()
			release()
			require.True(t, rt.WaitBackgroundTasks(time.Second), "failed initialization must not retain admitted readers")
			require.Equal(t, before, cfg, "startup inspection must not rewrite caller configuration")
			entries, err := os.ReadDir(root)
			require.NoError(t, err)
			require.Empty(t, entries, "invalid startup must not provision evidence artifacts")

			// Startup settings are documented as pinned. Validate recovery on a
			// fresh runtime rather than prescribing failed-once reload semantics.
			valid := before.Clone()
			valid.Metadata.OfflineScrapeEvidence = config.OfflineScrapeEvidenceConfig{Enabled: true, ArtifactDirectory: artifact, MaxAgeHours: 168}
			require.NoError(t, config.ValidateOfflineScrapeEvidence(valid.Metadata.OfflineScrapeEvidence))
			fresh := core.NewAPIRuntime(&core.APIDeps{})
			fresh.SetConfig(valid)
			t.Cleanup(fresh.Shutdown)
			require.NotNil(t, fresh.OfflineScrapeEvidence())
			require.NoDirExists(t, artifact, "valid lazy initialization also performs no artifact I/O")
			live, done, err := fresh.BeginOfflineScrapeEvidence(t.Context(), true)
			require.NoError(t, err)
			t.Cleanup(done)
			require.Equal(t, scrapeevidence.Unavailable, scrapeevidence.Resolve(live, nil, false).Outcome)
			done()
			require.True(t, fresh.WaitBackgroundTasks(time.Second))
			require.NoDirExists(t, artifact, "read-only access must not create a missing store")
		})
	}
}
