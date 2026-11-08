package sched

import (
	"errors"
	"time"
)

// Kind decides how the scheduler reacts to a failed attempt.
type Kind string

const (
	// KindTransient is retried with backoff and burns an attempt.
	KindTransient Kind = "transient"
	// KindPermanent fails the job at once; retrying cannot help.
	KindPermanent Kind = "permanent"
	// KindNotReady means "the inputs are not ready yet": retry later without burning an attempt.
	KindNotReady Kind = "not_ready"
)

// TaskError is what handlers return to steer retries.
type TaskError struct {
	Kind  Kind
	Msg   string
	After time.Duration
}

func (e *TaskError) Error() string { return e.Msg }

func Transient(msg string) error { return &TaskError{Kind: KindTransient, Msg: msg} }
func Permanent(msg string) error { return &TaskError{Kind: KindPermanent, Msg: msg} }
func NotReady(msg string, after time.Duration) error {
	return &TaskError{Kind: KindNotReady, Msg: msg, After: after}
}

func classify(err error) (Kind, time.Duration) {
	var te *TaskError
	if errors.As(err, &te) {
		after := te.After
		if te.Kind == KindNotReady && after <= 0 {
			after = 90 * time.Second
		}
		return te.Kind, after
	}
	return KindTransient, 0
}
