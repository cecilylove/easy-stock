package contracts

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type ErrorKind string

const (
	Unsupported     ErrorKind = "unsupported"
	NoData          ErrorKind = "no_data"
	InvalidResponse ErrorKind = "invalid_response"
	RateLimited     ErrorKind = "rate_limited"
	UpstreamFailure ErrorKind = "upstream_failure"
	Unauthorized    ErrorKind = "unauthorized"
	Canceled        ErrorKind = "canceled"
)

type Error struct {
	Kind                 ErrorKind
	SourceID, Capability string
	Cause                error
}

func (e *Error) Error() string { return fmt.Sprintf("%s %s: %s", e.SourceID, e.Capability, e.Kind) }
func (e *Error) Unwrap() error { return e.Cause }

// A typed cancellation retains context cancellation semantics even when an
// adapter has no underlying transport error to attach.
func (e *Error) Is(target error) bool { return e.Kind == Canceled && target == context.Canceled }
func Kind(err error) ErrorKind {
	if errors.Is(err, context.Canceled) {
		return Canceled
	}
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Kind
	}
	if err == nil {
		return ""
	}
	return UpstreamFailure
}

// Execution records work performed, separately from whether the requested
// capability produced complete valid data. Cache reads are not fresh attempts.
type Execution struct {
	Attempted, FromCache, Partial, Stale bool
	StartedAt, FetchedAt                 time.Time
}
type Issue struct {
	Kind    ErrorKind
	Field   string
	Message string
}
type Result[T any] struct {
	Data      T
	Execution Execution
	Issues    []Issue
}
