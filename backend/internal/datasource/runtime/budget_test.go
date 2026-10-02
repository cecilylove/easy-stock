package runtime_test

import (
	"context"
	"easy-stock/backend/internal/datasource/runtime"
	"testing"
	"time"
)

func TestNestedBudgetCannotExtendCallerOrRunAfterCancellation(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	deadline, _ := parent.Deadline()
	child, stop := runtime.Budget(parent, 10*time.Second)
	defer stop()
	actual, _ := child.Deadline()
	if actual.After(deadline) {
		t.Fatal("budget extended caller deadline")
	}
	cancel()
	called := false
	_, err := runtime.Call(parent, time.Second, func(context.Context) (int, error) { called = true; return 1, nil })
	if err != context.Canceled || called {
		t.Fatal("canceled request still performed work", err)
	}
}
