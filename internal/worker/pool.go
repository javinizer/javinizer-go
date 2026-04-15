package worker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

// Pool manages a pool of workers that execute tasks concurrently
type Pool struct {
	sem        *semaphore.Weighted
	ctx        context.Context
	cancel     context.CancelFunc
	maxWorkers int64
	timeout    time.Duration
	progress   *ProgressTracker
	wg         sync.WaitGroup
	mu         sync.Mutex
	errors     []error
}

// New creates a new worker pool
func NewPool(maxWorkers int, timeout time.Duration, progress *ProgressTracker) *Pool {
	return NewPoolWithContext(context.Background(), maxWorkers, timeout, progress)
}

// NewPoolWithContext creates a new worker pool with a parent context
// The pool will be cancelled when the parent context is cancelled
func NewPoolWithContext(parentCtx context.Context, maxWorkers int, timeout time.Duration, progress *ProgressTracker) *Pool {
	ctx, cancel := context.WithCancel(parentCtx)

	return &Pool{
		sem:        semaphore.NewWeighted(int64(maxWorkers)),
		ctx:        ctx,
		cancel:     cancel,
		maxWorkers: int64(maxWorkers),
		timeout:    timeout,
		progress:   progress,
		errors:     make([]error, 0),
	}
}

// Submit submits a task to the pool for execution
// Blocks if the pool is at capacity
func (p *Pool) Submit(task Task) error {
	// Check if context is canceled
	if p.ctx.Err() != nil {
		return p.ctx.Err()
	}

	// Acquire semaphore (blocks if at capacity)
	if err := p.sem.Acquire(p.ctx, 1); err != nil {
		return fmt.Errorf("failed to acquire worker: %w", err)
	}

	// Increment wait group
	p.wg.Add(1)

	// Execute task in goroutine
	go p.executeTask(task)

	return nil
}

// executeTask executes a single task
func (p *Pool) executeTask(task Task) {
	defer p.wg.Done()
	defer p.sem.Release(1)

	taskID := task.ID()
	taskType := task.Type()

	// Start tracking
	if p.progress != nil {
		p.progress.Start(taskID, taskType, task.Description())
	}

	// Create task context with timeout
	taskCtx := p.ctx
	if p.timeout > 0 {
		var taskCancel context.CancelFunc
		taskCtx, taskCancel = context.WithTimeout(p.ctx, p.timeout)
		defer taskCancel()
	}

	// Execute task
	startTime := time.Now()
	err := task.Execute(taskCtx)
	duration := time.Since(startTime)

	// Update progress
	if p.progress != nil {
		if err != nil {
			p.progress.Fail(taskID, err)
		} else {
			p.progress.Complete(taskID, fmt.Sprintf("Completed in %v", duration))
		}
	}

	// Store error
	if err != nil {
		p.mu.Lock()
		p.errors = append(p.errors, fmt.Errorf("task %s failed: %w", taskID, err))
		p.mu.Unlock()
	}
}

// Wait waits for all tasks to complete
func (p *Pool) Wait() error {
	p.wg.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.errors) == 0 {
		return nil
	}

	if len(p.errors) == 1 {
		return fmt.Errorf("%d task failed: %w", len(p.errors), p.errors[0])
	}

	joined := errors.Join(p.errors...)
	return fmt.Errorf("%d tasks failed: %w", len(p.errors), joined)
}

// Stop cancels all running tasks and waits for them to finish
func (p *Pool) Stop() {
	p.cancel()
	p.wg.Wait()
}

// Errors returns all errors encountered during task execution
func (p *Pool) Errors() []error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Return copy
	errors := make([]error, len(p.errors))
	copy(errors, p.errors)
	return errors
}

// ActiveWorkers returns the number of currently active workers
// Note: This is an approximation based on running tasks in progress tracker
func (p *Pool) ActiveWorkers() int {
	if p.progress == nil {
		return 0
	}
	stats := p.progress.Stats()
	return stats.Running
}

// Stats returns statistics about the pool
func (p *Pool) Stats() *PoolStats {
	stats := &PoolStats{
		MaxWorkers: int(p.maxWorkers),
		Timeout:    p.timeout,
	}

	p.mu.Lock()
	stats.Errors = len(p.errors)
	p.mu.Unlock()

	if p.progress != nil {
		progressStats := p.progress.Stats()
		stats.TotalTasks = progressStats.Total
		stats.Pending = progressStats.Pending
		stats.Running = progressStats.Running
		stats.Success = progressStats.Success
		stats.Failed = progressStats.Failed
		stats.Canceled = progressStats.Canceled
		stats.OverallProgress = progressStats.OverallProgress
	}

	return stats
}

// PoolStats holds statistics about the worker pool
type PoolStats struct {
	MaxWorkers      int
	Timeout         time.Duration
	TotalTasks      int
	Pending         int
	Running         int
	Success         int
	Failed          int
	Canceled        int
	Errors          int
	OverallProgress float64
}
