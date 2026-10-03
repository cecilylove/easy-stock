package eastmoney

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
)

func TestJSONRetry503SuccessAndThreeAttemptLimit(t *testing.T) {
	for _, succeeds := range []bool{true, false} {
		t.Run(map[bool]string{true: "eventual-success", false: "limit"}[succeeds], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if n := calls.Add(1); !succeeds || n < 3 {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_, _ = w.Write([]byte(`{"value":42}`))
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			var payload struct{ Value int }
			err := NewClient().getJSONWithRetry(ctx, server.URL, &payload)
			if calls.Load() != 3 {
				t.Fatalf("attempt count=%d err=%v", calls.Load(), err)
			}
			if succeeds {
				if err != nil || payload.Value != 42 {
					t.Fatalf("retry did not succeed: %+v %v", payload, err)
				}
			} else {
				var typed *contracts.Error
				if !errors.As(err, &typed) || typed.HTTPStatus != 503 || typed.Kind != contracts.UpstreamFailure {
					t.Fatalf("lost final HTTP status: %v", err)
				}
			}
		})
	}
}

func TestJSONRetryPermanentHTTPAndMalformedJSONOnlyOnce(t *testing.T) {
	for _, status := range []int{400, 401, 403, 200} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"broken":`))
			}))
			defer server.Close()
			var payload any
			err := NewClient().getJSONWithRetry(context.Background(), server.URL, &payload)
			var typed *contracts.Error
			if !errors.As(err, &typed) || calls.Load() != 1 || typed.HTTPStatus != status {
				t.Fatalf("permanent failure retried/lost status: calls=%d err=%v", calls.Load(), err)
			}
			want := contracts.InvalidResponse
			if status == 401 || status == 403 {
				want = contracts.Unauthorized
			}
			if typed.Kind != want || typed.Cause == nil {
				t.Fatalf("kind/cause=%+v", typed)
			}
		})
	}
}

func TestJSONRetry429WaitHonorsCancellationAndOriginCooldown(t *testing.T) {
	var calls atomic.Int32
	first := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(first)
		}
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := NewClient()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { var payload any; result <- client.getJSONWithRetry(ctx, server.URL, &payload) }()
	<-first
	// The handler signals before the response is decoded. Wait for the retained
	// cooldown under its mutex rather than relying on arbitrary sleeps.
	deadline := time.Now().Add(time.Second)
	for {
		client.cooldownMu.Lock()
		until := client.cooldowns[jsonOrigin(server.URL)]
		client.cooldownMu.Unlock()
		if !until.IsZero() {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("429 cooldown was not retained")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case err := <-result:
		var typed *contracts.Error
		if !errors.Is(err, context.Canceled) || contracts.Kind(err) != contracts.Canceled || !errors.As(err, &typed) {
			t.Fatalf("waiting cancellation lost: %v", err)
		}
		var limited *contracts.Error
		if !errors.As(typed.Cause, &limited) || limited.HTTPStatus != 429 || limited.RetryAfter != 2*time.Second {
			t.Fatalf("429 cause/status lost on cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("429 wait ignored cancellation")
	}
	ctx, stop := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer stop()
	var payload any
	err := client.getJSONWithRetry(ctx, server.URL+"/another-path", &payload)
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("next request ignored origin cooldown: calls=%d err=%v", calls.Load(), err)
	}
}

func TestJSONRetryAfterIsMinimumDelay(t *testing.T) {
	var calls atomic.Int32
	attemptTimes := make(chan time.Time, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptTimes <- time.Now()
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var payload any
	if err := NewClient().getJSONWithRetry(ctx, server.URL, &payload); err != nil {
		t.Fatal(err)
	}
	first, second := <-attemptTimes, <-attemptTimes
	if calls.Load() != 2 || second.Sub(first) < time.Second {
		t.Fatalf("Retry-After was shortened: calls=%d wait=%v", calls.Load(), second.Sub(first))
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for value, want := range map[string]time.Duration{"2": 2 * time.Second, now.Add(3 * time.Second).Format(http.TimeFormat): 3 * time.Second, "-2": 0, "bad": 0, now.Add(-time.Second).Format(http.TimeFormat): 0} {
		if got := parseRetryAfter(value, now); got != want {
			t.Errorf("%q: got=%v want=%v", value, got, want)
		}
	}
}
