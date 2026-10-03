package runtime_test

import (
	"context"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/runtime"
)

func TestPrimaryBudgetReservesFallbackWithinParent(t *testing.T) {
	for _, duration := range []time.Duration{300 * time.Millisecond, 10 * time.Second} {
		parent, cancel := context.WithTimeout(context.Background(), duration)
		primary, stop := runtime.PrimaryBudget(parent, 7*time.Second, 3*time.Second)
		parentDeadline, _ := parent.Deadline()
		primaryDeadline, _ := primary.Deadline()
		reserved := parentDeadline.Sub(primaryDeadline)
		if reserved < duration/4 || reserved > 3*time.Second+time.Millisecond {
			t.Fatalf("duration=%v reserved=%v", duration, reserved)
		}
		if primaryDeadline.After(parentDeadline) {
			t.Fatal("primary extended parent deadline")
		}
		cancel()
		if primary.Err() != context.Canceled {
			t.Fatalf("cancellation did not propagate: %v", primary.Err())
		}
		stop()
	}
}

func TestPrimaryBudgetHonorsMaximumWithoutDeadline(t *testing.T) {
	start := time.Now()
	primary, stop := runtime.PrimaryBudget(context.Background(), time.Second, time.Second)
	defer stop()
	deadline, ok := primary.Deadline()
	if !ok || deadline.Sub(start) > time.Second+10*time.Millisecond {
		t.Fatalf("missing bounded primary: %v %v", deadline, ok)
	}
	parent, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	primary, stop = runtime.PrimaryBudget(parent, time.Second, time.Second)
	defer stop()
	if primary.Err() != context.DeadlineExceeded {
		t.Fatalf("expired parent regained budget: %v", primary.Err())
	}
}
