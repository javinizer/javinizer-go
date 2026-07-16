package progress

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromContext_ReturnsNoopProgress_WhenNotSet(t *testing.T) {
	reporter := FromContext(context.Background())
	assert.NotNil(t, reporter)
	assert.Equal(t, NoopProgress, reporter)
}

func TestFromContext_ReturnsInjectedReporter(t *testing.T) {
	var calls int
	injected := ReporterFunc(func(step ProgressStep, pct float64, msg string) {
		calls++
	})
	ctx := WithReporter(context.Background(), injected)
	FromContext(ctx).Report(ProgressStepScrape, 0.5, "test")
	assert.Equal(t, 1, calls, "injected reporter should be called")
}

func TestWithReporter_NilReporter_ReturnsOriginalContext(t *testing.T) {
	original := context.Background()
	result := WithReporter(original, nil)
	assert.Equal(t, original, result)
}

func TestReporterFunc_Report_DelegatesToFunction(t *testing.T) {
	var calledStep ProgressStep
	var calledPct float64
	var calledMsg string
	fn := ReporterFunc(func(step ProgressStep, pct float64, msg string) {
		calledStep = step
		calledPct = pct
		calledMsg = msg
	})
	fn.Report(ProgressStepScrape, 0.5, "halfway")
	assert.Equal(t, ProgressStepScrape, calledStep)
	assert.Equal(t, 0.5, calledPct)
	assert.Equal(t, "halfway", calledMsg)
}

func TestNoopProgress_Report_IsNoOp(t *testing.T) {
	assert.NotPanics(t, func() {
		NoopProgress.Report(ProgressStepScrape, 1.0, "done")
	})
}

func TestProgressStep_Constants(t *testing.T) {
	assert.Equal(t, ProgressStep("scrape"), ProgressStepScrape)
	assert.Equal(t, ProgressStep("organize"), ProgressStepOrganize)
	assert.Equal(t, ProgressStep("download"), ProgressStepDownload)
	assert.Equal(t, ProgressStep("nfo"), ProgressStepNFO)
	assert.Equal(t, ProgressStep("apply"), ProgressStepApply)
}

func TestFromContext_Panics_OnNilContext(t *testing.T) {
	assert.Panics(t, func() {
		FromContext(nil)
	})
}
