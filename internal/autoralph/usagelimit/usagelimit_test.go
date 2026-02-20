package usagelimit

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

func testState(t *testing.T) *State {
	t.Helper()
	return NewState(slog.Default())
}

func TestNewState_ReturnsInitializedState(t *testing.T) {
	s := testState(t)
	if s == nil {
		t.Fatal("NewState returned nil")
	}
}

func TestIsActive_ReturnsFalseWhenNoLimitSet(t *testing.T) {
	s := testState(t)
	if s.IsActive() {
		t.Error("IsActive should return false when no limit is set")
	}
}

func TestIsActive_ReturnsTrueWhenResetAtInFuture(t *testing.T) {
	s := testState(t)
	s.Set(time.Now().Add(1 * time.Hour))
	if !s.IsActive() {
		t.Error("IsActive should return true when resetAt is in the future")
	}
}

func TestIsActive_ReturnsFalseWhenResetAtInPast(t *testing.T) {
	s := testState(t)
	s.Set(time.Now().Add(-1 * time.Second))
	if s.IsActive() {
		t.Error("IsActive should return false when resetAt is in the past")
	}
}

func TestResetAt_ReturnsZeroValueWhenNotSet(t *testing.T) {
	s := testState(t)
	if !s.ResetAt().IsZero() {
		t.Errorf("ResetAt should return zero value, got %v", s.ResetAt())
	}
}

func TestResetAt_ReturnsStoredTime(t *testing.T) {
	s := testState(t)
	expected := time.Now().Add(5 * time.Minute)
	s.Set(expected)
	got := s.ResetAt()
	if !got.Equal(expected) {
		t.Errorf("ResetAt = %v, want %v", got, expected)
	}
}

func TestSet_OverwritesPreviousValue(t *testing.T) {
	s := testState(t)
	first := time.Now().Add(1 * time.Minute)
	second := time.Now().Add(10 * time.Minute)
	s.Set(first)
	s.Set(second)
	got := s.ResetAt()
	if !got.Equal(second) {
		t.Errorf("ResetAt = %v, want %v", got, second)
	}
}

func TestWait_ReturnsImmediatelyWhenInactive(t *testing.T) {
	s := testState(t)
	ctx := context.Background()

	start := time.Now()
	err := s.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Wait returned error: %v", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("Wait took %v, expected immediate return", elapsed)
	}
}

func TestWait_ReturnsImmediatelyWhenResetAtInPast(t *testing.T) {
	s := testState(t)
	s.Set(time.Now().Add(-1 * time.Second))
	ctx := context.Background()

	start := time.Now()
	err := s.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Wait returned error: %v", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("Wait took %v, expected immediate return", elapsed)
	}
}

func TestWait_BlocksUntilResetAt(t *testing.T) {
	s := testState(t)
	waitDuration := 200 * time.Millisecond
	s.Set(time.Now().Add(waitDuration))
	ctx := context.Background()

	start := time.Now()
	err := s.Wait(ctx)
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Wait returned error: %v", err)
	}
	if elapsed < waitDuration-20*time.Millisecond {
		t.Errorf("Wait returned too early: %v (expected >= %v)", elapsed, waitDuration)
	}
	if elapsed > waitDuration+200*time.Millisecond {
		t.Errorf("Wait took too long: %v", elapsed)
	}
}

func TestWait_ReturnsContextErrorOnCancel(t *testing.T) {
	s := testState(t)
	s.Set(time.Now().Add(10 * time.Second))
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- s.Wait(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Wait returned %v, want context.Canceled", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Wait did not return after context cancellation")
	}
}

func TestWait_ReturnsDeadlineExceededOnTimeout(t *testing.T) {
	s := testState(t)
	s.Set(time.Now().Add(10 * time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := s.Wait(ctx)
	if err != context.DeadlineExceeded {
		t.Errorf("Wait returned %v, want context.DeadlineExceeded", err)
	}
}

func TestConcurrentAccess_IsRaceDetectorClean(t *testing.T) {
	s := testState(t)
	var wg sync.WaitGroup

	// Writer goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s.Set(time.Now().Add(time.Duration(i) * time.Millisecond))
		}(i)
	}

	// Reader goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.IsActive()
			_ = s.ResetAt()
		}()
	}

	// Wait goroutines
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Wait(ctx)
		}()
	}

	wg.Wait()
}
