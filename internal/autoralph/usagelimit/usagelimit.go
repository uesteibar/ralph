package usagelimit

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// State tracks a global usage limit with a thread-safe reset time.
// When active (resetAt is in the future), Wait blocks until the limit expires
// or the context is cancelled.
type State struct {
	mu      sync.RWMutex
	resetAt time.Time
	logger  *slog.Logger
}

// NewState creates a new State instance.
func NewState(logger *slog.Logger) *State {
	return &State{logger: logger}
}

// Set stores the reset time and logs when the limit is set.
func (s *State) Set(resetAt time.Time) {
	s.mu.Lock()
	s.resetAt = resetAt
	s.mu.Unlock()
	s.logger.Info("usage limit set", "reset_at", resetAt)
}

// IsActive returns true when the stored resetAt is in the future.
func (s *State) IsActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.resetAt.IsZero() && time.Now().Before(s.resetAt)
}

// ResetAt returns the stored reset time (zero value if none).
func (s *State) ResetAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.resetAt
}

// Wait blocks until the usage limit expires or the context is cancelled.
// Returns nil immediately when no limit is active.
// Returns ctx.Err() when the context is cancelled during wait.
func (s *State) Wait(ctx context.Context) error {
	s.mu.RLock()
	resetAt := s.resetAt
	s.mu.RUnlock()

	wait := time.Until(resetAt)
	if wait <= 0 {
		return nil
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
