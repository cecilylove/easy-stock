package contracts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDeadlineAndCancellationKindsRemainDistinct(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, &Error{Kind: TimedOut}, &Error{Kind: UpstreamFailure, Cause: context.DeadlineExceeded}} {
		wrapped := fmt.Errorf("provider: %w", err)
		if Kind(wrapped) != TimedOut || !errors.Is(wrapped, context.DeadlineExceeded) || errors.Is(wrapped, context.Canceled) {
			t.Fatalf("deadline semantics lost: %v", wrapped)
		}
	}
	cause := errors.New("original transport reason")
	err := &Error{Kind: RateLimited, SourceID: "eastmoney", Cause: cause, HTTPStatus: 429, RetryAfter: time.Second}
	if !errors.Is(err, cause) || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), cause.Error()) {
		t.Fatalf("structured transport detail lost: %v", err)
	}
}

func TestTypedCancellationWithoutCauseRetainsContextSemantics(t *testing.T) {
	err := &Error{Kind: Canceled, SourceID: "fixture"}
	if !errors.Is(err, context.Canceled) || !errors.Is(fmt.Errorf("adapter: %w", err), context.Canceled) || Kind(err) != Canceled {
		t.Fatalf("typed cancellation lost context semantics: %v", err)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(&Error{Kind: Unsupported}, context.Canceled) {
		t.Fatal("non-cancellation classified as context cancellation")
	}
	cause := errors.New("transport detail")
	if !errors.Is(&Error{Kind: UpstreamFailure, Cause: cause}, cause) {
		t.Fatal("typed error lost its original cause")
	}
}
