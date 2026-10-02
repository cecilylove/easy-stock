package contracts

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

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
