package tui

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
)

// sortService is the concrete implementation of SortService.
// It wraps the processingCoordinator and worker.BatchJobFactory,
// translating between TUI-local types (SortEvent) and worker types (JobEvent).
//
// This is the ONLY file in the tui package that imports worker and workflow.
// All other TUI files depend on the SortService interface only.
type sortService struct {
	processor     *processingCoordinator
	forwardCh     chan SortEvent
	workerEventCh chan worker.JobEvent
}

// NewSortService creates a SortService backed by the given processing infrastructure.
// The factory, registry, and processorCfg parameters are the same values that
// were previously passed directly to NewProcessingCoordinator from the TUI command.
// Returns the SortService and a channel of SortEvents for the Model to consume.
func NewSortService(
	runner backgroundRunner,
	factory worker.BatchJobFactoryInterface,
	registry scrape.ScraperInstanceResolver,
	processorCfg TUIProcessorConfig,
	destPath string,
	moveFiles bool,
) (SortService, <-chan SortEvent, error) {
	if factory == nil {
		return nil, nil, fmt.Errorf("batch job factory must not be nil")
	}

	// Internal channel for SortEvent forwarding (replaces the old raw worker.JobEvent channel)
	forwardCh := make(chan SortEvent, 64)

	// Wrap the forward channel as a worker.JobEvent channel for the processingCoordinator
	workerEventCh := make(chan worker.JobEvent, 64)

	processor, err := NewProcessingCoordinator(
		runner,
		workerEventCh,
		factory,
		registry,
		processorCfg,
		destPath,
		moveFiles,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create processing coordinator: %w", err)
	}

	// Start a goroutine that translates worker.JobEvent → SortEvent
	go translateEvents(workerEventCh, forwardCh)

	return &sortService{
		processor:     processor,
		forwardCh:     forwardCh,
		workerEventCh: workerEventCh,
	}, forwardCh, nil
}

// translateEvents reads worker.JobEvents from the worker channel,
// converts them to TUI-local SortEvents, and forwards them.
// Stops when the input channel is closed.
func translateEvents(src <-chan worker.JobEvent, dst chan<- SortEvent) {
	for evt := range src {
		dst <- SortEvent{
			JobID:     string(evt.JobID),
			MovieID:   evt.MovieID,
			Phase:     sortEventPhaseFromWorker(evt.Phase),
			Step:      sortEventStepFromWorker(evt.Step),
			Progress:  evt.Progress,
			Message:   evt.Message,
			Timestamp: evt.Timestamp,
		}
	}
	close(dst)
}

func sortEventPhaseFromWorker(p worker.JobEventPhase) SortEventPhase {
	return SortEventPhase(string(p))
}

func sortEventStepFromWorker(s worker.JobEventStep) SortEventStep {
	return SortEventStep(string(s))
}

// ProcessFiles starts asynchronous processing of matched files.
func (s *sortService) ProcessFiles(ctx context.Context, files []fileItem, matches map[string]models.FileMatchInfo) error {
	return s.processor.ProcessFiles(ctx, files, matches)
}

// Wait blocks until all submitted tasks have completed.
func (s *sortService) Wait() error {
	return s.processor.Wait()
}

// Stop cancels all running tasks and waits for them to finish, then closes the
// worker event channel so the translateEvents goroutine can drain and close the
// SortEvent forward channel (avoids leaking the forwarding goroutine).
func (s *sortService) Stop() {
	s.processor.Stop()
	if s.workerEventCh != nil {
		close(s.workerEventCh)
	}
}

func (s *sortService) SetOptions(opts ProcessorOptions)         { s.processor.SetOptions(opts) }
func (s *sortService) LoadOptions() ProcessorOptions            { return s.processor.LoadOptions() }
func (s *sortService) SetConfig(cfg TUIProcessorConfig)         { s.processor.SetConfig(cfg) }
func (s *sortService) SetCustomScrapers(scrapers []string)      { s.processor.SetCustomScrapers(scrapers) }
func (s *sortService) GetCustomScrapers() []string              { return s.processor.GetCustomScrapers() }
func (s *sortService) Registry() scrape.ScraperInstanceResolver { return s.processor.Registry() }

// --- ScanService implementation ---

// workflowScanService adapts a workflow.WorkflowInterface to the ScanService interface.
// This is the ONLY place that imports workflow for scan-related operations.
type workflowScanService struct {
	wf        workflow.WorkflowInterface
	recursive bool
}

// NewWorkflowScanService creates a ScanService backed by a workflow.WorkflowInterface.
func NewWorkflowScanService(wf workflow.WorkflowInterface, recursive bool) ScanService {
	return &workflowScanService{wf: wf, recursive: recursive}
}

// ScanAndMatch scans a directory for video files and matches JAV IDs.
func (s *workflowScanService) ScanAndMatch(ctx context.Context, directory string, recursive bool) (*ScanResult, error) {
	result, err := s.wf.ScanAndMatch(ctx, workflow.ScanAndMatchCmd{
		Directory: directory,
		Recursive: recursive,
	})
	if err != nil {
		return nil, err
	}
	return &ScanResult{
		Files:   result.Files,
		Skipped: result.Skipped,
	}, nil
}

// channelSortEventSubscriber adapts a SortEvent channel to the SortEventSubscriber interface.
type channelSortEventSubscriber struct {
	ch   <-chan SortEvent
	done chan struct{}
}

// NewChannelSortEventSubscriber creates a SortEventSubscriber from a receive-only channel.
func NewChannelSortEventSubscriber(ch <-chan SortEvent) SortEventSubscriber {
	return &channelSortEventSubscriber{ch: ch, done: make(chan struct{})}
}

func (s *channelSortEventSubscriber) Events() <-chan SortEvent { return s.ch }
func (s *channelSortEventSubscriber) Close() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}
func (s *channelSortEventSubscriber) Done() <-chan struct{} { return s.done }

// Compile-time assertions
var _ SortService = (*sortService)(nil)
var _ ScanService = (*workflowScanService)(nil)
