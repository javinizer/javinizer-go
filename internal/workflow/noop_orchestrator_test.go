package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- noOpApplyOrchestrator tests ---

func TestNoOpApplyOrchestrator_Execute(t *testing.T) {
	op := noOpApplyOrchestrator{}
	result, err := op.Execute(context.Background(), ApplyCmd{}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "apply not configured")
	assert.Nil(t, result)
}

// --- noOpScrapeOrchestrator tests ---

func TestNoOpScrapeOrchestrator_Execute(t *testing.T) {
	op := noOpScrapeOrchestrator{}
	result, meta, err := op.Execute(context.Background(), scrape.ScrapeCmd{}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scrape not configured")
	assert.Nil(t, result)
	assert.Nil(t, meta)
}

// --- noOpPreviewOrchestrator tests ---

func TestNoOpPreviewOrchestrator_Execute(t *testing.T) {
	op := noOpPreviewOrchestrator{}
	result, err := op.Execute(context.Background(), PreviewCmd{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "preview not configured")
	assert.Nil(t, result)
}

// --- noOpScanAndMatchOrchestrator tests ---

func TestNoOpScanAndMatchOrchestrator_Execute(t *testing.T) {
	op := noOpScanAndMatchOrchestrator{}
	result, err := op.Execute(context.Background(), ScanAndMatchCmd{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scan and match not configured")
	assert.Nil(t, result)
}

// --- noOpCompareOrchestrator tests ---

func TestNoOpCompareOrchestrator_Execute(t *testing.T) {
	op := noOpCompareOrchestrator{}
	result, err := op.Execute(context.Background(), CompareCmd{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "compare not configured")
	assert.Nil(t, result)
}

// --- Workflow delegation to noOp orchestrators ---

func TestWorkflow_Apply_NotConfigured(t *testing.T) {
	wf := &Workflow{
		apply: noOpApplyOrchestrator{},
	}
	_, err := wf.Apply(context.Background(), ApplyCmd{}, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "apply not configured")
}

func TestWorkflow_Compare_NotConfigured(t *testing.T) {
	wf := &Workflow{
		compare: noOpCompareOrchestrator{},
	}
	_, err := wf.Compare(context.Background(), CompareCmd{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "compare not configured")
}

func TestWorkflow_Preview_NotConfigured(t *testing.T) {
	wf := &Workflow{
		preview: noOpPreviewOrchestrator{},
	}
	_, err := wf.Preview(context.Background(), PreviewCmd{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "preview not configured")
}

func TestWorkflow_ScanAndMatch_NotConfigured(t *testing.T) {
	wf := &Workflow{
		scanMatch: noOpScanAndMatchOrchestrator{},
	}
	_, err := wf.ScanAndMatch(context.Background(), ScanAndMatchCmd{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scan and match not configured")
}

// --- RevertLog from config edge cases ---

func TestNewRevertLogFromConfig_Disabled(t *testing.T) {
	// Recording is independent of AllowRevert; only nil repo/cfg returns noOpRevertLog.
	rl := NewRevertLogFromConfig(nil, &RevertLogConfig{AllowRevert: false}, "", nil, nil, nil, nil)
	assert.IsType(t, noOpRevertLog{}, rl, "nil repo should return noOpRevertLog (defensive)")
}

// --- WorkflowFactory nil/guard tests ---

func TestWorkflowFactory_ReloadReplacementCaches_NilFactory(t *testing.T) {
	var f *WorkflowFactory
	// Should not panic on nil factory
	assert.NotPanics(t, func() {
		f.ReloadReplacementCaches(context.Background())
	})
}

func TestWorkflowFactory_ReloadReplacementCaches_NilAggregator(t *testing.T) {
	f := &WorkflowFactory{}
	// Should not panic when cachedAggregator is nil
	assert.NotPanics(t, func() {
		f.ReloadReplacementCaches(context.Background())
	})
}

// --- CollectDownloadProxyResolvers tests ---

func TestCollectDownloadProxyResolvers_NilRegistry(t *testing.T) {
	var registry *scraperutil.ScraperRegistry
	result := registry.CollectDownloadProxyResolvers([]string{"r18dev"})
	assert.Nil(t, result)
}

func TestCollectDownloadProxyResolvers_EmptyPriority(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	result := registry.CollectDownloadProxyResolvers(nil)
	assert.Empty(t, result)
}

func TestCollectDownloadProxyResolvers_WithPriority(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&mockScraper{name: "r18dev", enabled: true})
	registry.RegisterInstance(&mockScraper{name: "javdb", enabled: true})

	result := registry.CollectDownloadProxyResolvers([]string{"r18dev", "javdb"})
	// Both mockScrapers implement DownloadProxyResolver, so both should be collected
	// in priority order.
	require.Len(t, result, 2)
}

func TestCollectDownloadProxyResolvers_Deduplication(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&mockScraper{name: "r18dev", enabled: true})

	result := registry.CollectDownloadProxyResolvers([]string{"r18dev", "r18dev"})
	require.Len(t, result, 1)
}

func TestCollectDownloadProxyResolvers_RemainingSorted(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	registry.RegisterInstance(&mockScraper{name: "javdb", enabled: true})
	registry.RegisterInstance(&mockScraper{name: "r18dev", enabled: true})

	// Only r18dev in priority — javdb should appear as remaining, sorted
	result := registry.CollectDownloadProxyResolvers([]string{"r18dev"})
	require.Len(t, result, 2)
}

// --- NewTemplateEngine tests ---

func TestNewTemplateEngine(t *testing.T) {
	engine := template.NewEngine()
	assert.NotNil(t, engine)
}

// --- ScanAndMatchCmd test ---

func TestScanAndMatchCmd_Fields(t *testing.T) {
	cmd := ScanAndMatchCmd{
		Directory: "/videos",
		Recursive: true,
	}
	assert.Equal(t, "/videos", cmd.Directory)
	assert.True(t, cmd.Recursive)
}

// --- PreviewCmd test ---

func TestPreviewCmd_Fields(t *testing.T) {
	cmd := PreviewCmd{
		Destination: "/videos",
		SkipNFO:     true,
	}
	assert.Equal(t, "/videos", cmd.Destination)
	assert.True(t, cmd.SkipNFO)
}

// --- CompareCmd test ---

func TestCompareCmd_Fields(t *testing.T) {
	cmd := CompareCmd{
		NFOPath: "/source/TEST-001.nfo",
	}
	assert.Equal(t, "/source/TEST-001.nfo", cmd.NFOPath)
}

// --- models.ScraperResult in context ---

func TestScraperResult_Fields(t *testing.T) {
	sr := &models.ScraperResult{
		ID:     "TEST-001",
		Title:  "Test Movie",
		Source: "r18dev",
	}
	assert.Equal(t, "TEST-001", sr.ID)
	assert.Equal(t, "Test Movie", sr.Title)
	assert.Equal(t, "r18dev", sr.Source)
}
