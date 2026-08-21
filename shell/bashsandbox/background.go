package bashsandbox

import (
	"context"
	"sync"
)

// JobStatus is a background Job's lifecycle state.
type JobStatus string

const (
	JobRunning   JobStatus = "running"
	JobCompleted JobStatus = "completed" // settled without a Go-level error — regardless of exit code
	JobFailed    JobStatus = "failed"    // settled with a Go-level error (infrastructure, not exit code)
)

// Job is a background command's handle, returned by RunBackground. Poll
// Status or block on Wait to retrieve the settled RunResult — sandbox facts
// (mode, enforcement, a runner failure) are only knowable once the job
// actually settles, mirroring the TS original's own background-handle
// contract.
type Job struct {
	mu     sync.Mutex
	status JobStatus
	done   chan struct{}
	result RunResult
	err    error
}

// RunBackground starts opts.Command without blocking; call Wait (or poll
// Status) on the returned Job to retrieve its eventual RunResult. ctx
// governs the whole run exactly as it does for Run — cancelling it (or the
// command reaching opts.Timeout) still settles the job, just
// asynchronously. A non-zero exit code alone settles the job as
// JobCompleted, not JobFailed — matching Run's own "a non-zero exit is not
// a Go error" posture; JobFailed is reserved for the same infrastructure
// failures Run itself returns as an error.
func (e *Executor) RunBackground(ctx context.Context, opts RunOptions) *Job {
	j := &Job{status: JobRunning, done: make(chan struct{})}
	go func() {
		result, err := e.Run(ctx, opts)
		j.mu.Lock()
		j.result, j.err = result, err
		if err != nil {
			j.status = JobFailed
		} else {
			j.status = JobCompleted
		}
		j.mu.Unlock()
		close(j.done)
	}()
	return j
}

// Status reports the job's current lifecycle state without blocking.
func (j *Job) Status() JobStatus {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status
}

// Wait blocks until the job settles or waitCtx is cancelled, whichever
// comes first, and returns the settled result. Safe to call more than
// once, and from more than one goroutine — every waiter observes the same
// result, and the underlying run is never re-triggered or duplicated.
func (j *Job) Wait(waitCtx context.Context) (RunResult, error) {
	select {
	case <-j.done:
		j.mu.Lock()
		defer j.mu.Unlock()
		return j.result, j.err
	case <-waitCtx.Done():
		return RunResult{}, waitCtx.Err()
	}
}
