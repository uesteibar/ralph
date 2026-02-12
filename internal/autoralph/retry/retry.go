package retry

import (
	"context"
	"errors"
	"time"
)

// DefaultBackoff is the default set of delays between retry attempts.
var DefaultBackoff = []time.Duration{1 * time.Second, 5 * time.Second, 15 * time.Second}

// permanentError wraps an error that should not be retried.
type permanentError struct {
	err error
}

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

// Permanent wraps an error to signal that it should not be retried.
func Permanent(err error) error {
	return &permanentError{err: err}
}

type options struct {
	maxAttempts int
	backoff     []time.Duration
}

// Option configures retry behavior.
type Option func(*options)

// WithMaxAttempts sets the maximum number of attempts (including first try).
func WithMaxAttempts(n int) Option {
	return func(o *options) { o.maxAttempts = n }
}

// WithBackoff sets the delays between attempts. The number of delays should be
// maxAttempts-1. If fewer delays are provided, the last delay is reused.
func WithBackoff(delays ...time.Duration) Option {
	return func(o *options) { o.backoff = delays }
}

func resolveOptions(opts []Option) options {
	o := options{
		maxAttempts: 3,
		backoff:     DefaultBackoff,
	}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// Do executes fn, retrying on failure with exponential backoff.
// It stops retrying when fn returns nil, a permanent error, or the context
// is cancelled. Returns the last error on exhaustion.
func Do(ctx context.Context, fn func() error, opts ...Option) error {
	o := resolveOptions(opts)

	var lastErr error
	for attempt := range o.maxAttempts {
		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		var pe *permanentError
		if errors.As(lastErr, &pe) {
			return pe.err
		}

		// Don't sleep after the last attempt.
		if attempt < o.maxAttempts-1 {
			delay := backoffDelay(o.backoff, attempt)
			select {
			case <-ctx.Done():
				return lastErr
			case <-time.After(delay):
			}
		}
	}
	return lastErr
}

// DoVal is like Do but for functions that return a value and an error.
func DoVal[T any](ctx context.Context, fn func() (T, error), opts ...Option) (T, error) {
	o := resolveOptions(opts)

	var lastErr error
	var zero T
	for attempt := range o.maxAttempts {
		val, err := fn()
		if err == nil {
			return val, nil
		}
		lastErr = err

		var pe *permanentError
		if errors.As(lastErr, &pe) {
			return zero, pe.err
		}

		if attempt < o.maxAttempts-1 {
			delay := backoffDelay(o.backoff, attempt)
			select {
			case <-ctx.Done():
				return zero, lastErr
			case <-time.After(delay):
			}
		}
	}
	return zero, lastErr
}

// backoffDelay returns the delay for the given attempt index. If the index
// exceeds the backoff slice, the last delay is reused.
func backoffDelay(backoff []time.Duration, attempt int) time.Duration {
	if attempt < len(backoff) {
		return backoff[attempt]
	}
	return backoff[len(backoff)-1]
}
