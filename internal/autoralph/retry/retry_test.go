package retry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestDo_SucceedsFirstAttempt(t *testing.T) {
	calls := 0
	err := Do(context.Background(), func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestDo_SucceedsAfterRetries(t *testing.T) {
	calls := 0
	err := Do(context.Background(), func() error {
		calls++
		if calls < 3 {
			return fmt.Errorf("transient error")
		}
		return nil
	}, WithBackoff(time.Millisecond, time.Millisecond))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
}

func TestDo_ExhaustsRetries(t *testing.T) {
	calls := 0
	err := Do(context.Background(), func() error {
		calls++
		return fmt.Errorf("persistent error")
	}, WithBackoff(time.Millisecond, time.Millisecond))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls, got %d", calls)
	}
	if err.Error() != "persistent error" {
		t.Fatalf("expected original error, got: %v", err)
	}
}

func TestDo_RespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := Do(ctx, func() error {
		calls++
		return fmt.Errorf("fail")
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (no retry after context cancel), got %d", calls)
	}
}

func TestDo_CustomOptions(t *testing.T) {
	calls := 0
	err := Do(context.Background(), func() error {
		calls++
		return fmt.Errorf("fail")
	}, WithMaxAttempts(5), WithBackoff(time.Millisecond, time.Millisecond, time.Millisecond, time.Millisecond))

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 5 {
		t.Fatalf("expected 5 calls, got %d", calls)
	}
}

func TestDo_PermanentError(t *testing.T) {
	calls := 0
	err := Do(context.Background(), func() error {
		calls++
		return Permanent(fmt.Errorf("bad request"))
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (no retry for permanent), got %d", calls)
	}
	if err.Error() != "bad request" {
		t.Fatalf("expected unwrapped error, got: %v", err)
	}
}

func TestDoVal_ReturnsValue(t *testing.T) {
	calls := 0
	val, err := DoVal(context.Background(), func() (string, error) {
		calls++
		if calls < 2 {
			return "", fmt.Errorf("transient")
		}
		return "hello", nil
	}, WithBackoff(time.Millisecond))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "hello" {
		t.Fatalf("expected 'hello', got %q", val)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}

func TestPermanentError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("inner")
	perm := Permanent(inner)
	if !errors.Is(perm, inner) {
		t.Fatal("Permanent error should unwrap to inner error")
	}
	var pe *permanentError
	if !errors.As(perm, &pe) {
		t.Fatal("should be detectable as permanentError")
	}
}

func TestDo_BackoffDelays(t *testing.T) {
	// Verify that retries actually wait (at least minimally)
	start := time.Now()
	calls := 0
	Do(context.Background(), func() error {
		calls++
		return fmt.Errorf("fail")
	}, WithMaxAttempts(3), WithBackoff(10*time.Millisecond, 20*time.Millisecond))

	elapsed := time.Since(start)
	// Should have waited at least 30ms (10ms + 20ms between 3 attempts)
	if elapsed < 25*time.Millisecond {
		t.Fatalf("expected delays, but elapsed only %v", elapsed)
	}
}
