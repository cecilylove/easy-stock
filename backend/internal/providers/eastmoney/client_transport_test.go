package eastmoney

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
)

type jsonRoundTripper func(*http.Request) (*http.Response, error)

func (f jsonRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestJSONTransportPreservesCauseAndUsesTypedRetry(t *testing.T) {
	var calls int
	client := NewClient(WithHTTPClient(&http.Client{Transport: jsonRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, io.EOF
	})}))
	var payload any
	err := client.getJSONWithRetry(context.Background(), "http://local.test", &payload)
	var typed *contracts.Error
	var original *url.Error
	if calls != 3 || !errors.As(err, &typed) || typed.Kind != contracts.UpstreamFailure || !errors.As(err, &original) || !errors.Is(err, io.EOF) {
		t.Fatalf("lost transport cause/retries: calls=%d err=%v", calls, err)
	}
	// English keywords are not transport types, even when the error sounds transient.
	calls = 0
	sentinel := errors.New("temporary EOF timeout wording")
	client = NewClient(WithHTTPClient(&http.Client{Transport: jsonRoundTripper(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, sentinel
	})}))
	err = client.getJSONWithRetry(context.Background(), "http://local.test", &payload)
	if calls != 1 || !errors.Is(err, sentinel) {
		t.Fatalf("error text caused blind retries: calls=%d err=%v", calls, err)
	}
}

func TestJSONTransportCancellationAndDeadline(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 70*time.Millisecond)
	defer cancel()
	var payload any
	start := time.Now()
	err := NewClient().getJSONWithRetry(ctx, server.URL, &payload)
	var typed *contracts.Error
	if !errors.As(err, &typed) || !typed.Timeout || contracts.Kind(err) != contracts.TimedOut || !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("deadline semantics/retry count: calls=%d err=%v", calls.Load(), err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("retry exceeded parent budget")
	}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	err = NewClient().getJSONWithRetry(ctx, server.URL, &payload)
	if !errors.Is(err, context.Canceled) || contracts.Kind(err) != contracts.Canceled || calls.Load() != 1 {
		t.Fatalf("canceled caller performed work: calls=%d err=%v", calls.Load(), err)
	}
}

func TestJSONTransportInvalidJSONRetainsDecoderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{invalid}`))
	}))
	defer server.Close()
	var payload any
	err := NewClient().getJSON(context.Background(), server.URL, &payload)
	var typed *contracts.Error
	var syntax *json.SyntaxError
	if !errors.As(err, &typed) || !errors.As(err, &syntax) || typed.Kind != contracts.InvalidResponse || typed.HTTPStatus != 200 || isTransient(err) {
		t.Fatalf("decoder error cause/classification lost: %v", err)
	}
}

func TestJSONBodyTimeoutRetainsHTTPStatusAndDoesNotBlindlyRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	client := NewClient(WithHTTPClient(&http.Client{Timeout: 30 * time.Millisecond}))
	var payload any
	err := client.getJSONWithRetry(context.Background(), server.URL, &payload)
	var typed *contracts.Error
	if !errors.As(err, &typed) || typed.Kind != contracts.TimedOut || !typed.Timeout || typed.HTTPStatus != 200 || calls.Load() != 1 || typed.Cause == nil {
		t.Fatalf("body timeout lost classification/status: calls=%d err=%v", calls.Load(), err)
	}
}

func TestJSONClientTimeoutRemainsTypedAndBounded(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-r.Context().Done()
	}))
	defer server.Close()
	client := NewClient(WithHTTPClient(&http.Client{Timeout: 20 * time.Millisecond}))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var payload any
	err := client.getJSONWithRetry(ctx, server.URL, &payload)
	var typed *contracts.Error
	if !errors.As(err, &typed) || !typed.Timeout || typed.Kind != contracts.TimedOut || calls.Load() != 3 || ctx.Err() != nil {
		t.Fatalf("client timeout retry classification: calls=%d err=%v", calls.Load(), err)
	}
}
