package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/eastmoney"
	"easy-stock/backend/internal/providers/sina"
)

type klineProviderFunc func(context.Context, string, string, int) ([]foundation.KLine, error)

func (f klineProviderFunc) KLine(ctx context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	return f(ctx, symbol, period, limit)
}

func fallbackTestServer(primary, fallback KLineProvider) *Server {
	return &Server{kLinePrimary: primary, kLineFallback: fallback, kLinePrimarySourceID: "eastmoney", kLineFallbackSourceID: "sina", sourceHealth: newSourceHealthTracker()}
}

func fallbackTestBars() []foundation.KLine {
	return []foundation.KLine{{Symbol: "000002.SZ", Time: time.Now(), Close: 4.26, Meta: foundation.SourceMeta{Source: "sina", FetchedAt: time.Now()}}}
}

func TestKLineEmptyPrimaryUsesHealthyFallback(t *testing.T) {
	for _, body := range []string{`{"rc":0,"data":null}`, `{"rc":0,"data":{"klines":[]}}`} {
		t.Run(body, func(t *testing.T) {
			primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer primary.Close()
			calls := 0
			server := fallbackTestServer(eastmoney.NewClient(eastmoney.WithBaseURL(primary.URL)), klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
				calls++
				return fallbackTestBars(), nil
			}))
			lines, err := server.loadKLine(context.Background(), "000002.SZ", "month", 60)
			if err != nil || len(lines) != 1 || calls != 1 || lines[0].Close != 4.26 || lines[0].Meta.FallbackReason == "" {
				t.Fatalf("lines=%+v err=%v fallback calls=%d", lines, err, calls)
			}
			items := server.sourceHealth.snapshot(time.Now())
			if sourceByID(t, items, "eastmoney").Status != "degraded" || sourceByID(t, items, "sina").Status != "available" {
				t.Fatalf("actual empty-primary failure and fallback success not observed: %+v", items)
			}
		})
	}
}

func TestKLineRejectsBothEmptyProviders(t *testing.T) {
	empty := klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) { return nil, nil })
	server := fallbackTestServer(empty, empty)
	if lines, err := server.loadKLine(context.Background(), "000002.SZ", "day", 60); err == nil || len(lines) != 0 {
		t.Fatalf("two empty sources were treated as usable: lines=%v err=%v", lines, err)
	}
}

func TestKLineSlowPrimaryLeavesBudgetForActualSinaRequest(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer primary.Close()
	var fallbackCalls atomic.Int32
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fallbackCalls.Add(1)
		_, _ = w.Write([]byte(`callback([{"day":"2026-09-30","open":"4","high":"5","low":"3","close":"4.26","volume":"123"}]);`))
	}))
	defer fallback.Close()
	server := fallbackTestServer(eastmoney.NewClient(eastmoney.WithBaseURL(primary.URL)), sina.NewClient(sina.WithKLineBaseURL(fallback.URL)))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	lines, err := server.loadKLine(ctx, "000002.SZ", "day", 60)
	if err != nil || len(lines) != 1 || fallbackCalls.Load() != 1 || ctx.Err() != nil {
		t.Fatalf("slow primary starved healthy fallback: lines=%v err=%v calls=%d parent=%v", lines, err, fallbackCalls.Load(), ctx.Err())
	}
	items := server.sourceHealth.snapshot(time.Now())
	if sourceByID(t, items, "eastmoney").Status != "degraded" || sourceByID(t, items, "sina").Status != "available" {
		t.Fatalf("slow primary/fallback observations incorrect: %+v", items)
	}
}

func TestKLineCancellationDoesNotAttemptOrFailFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := fallbackTestServer(klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		cancel()
		return nil, context.Canceled
	}), klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		t.Fatal("fallback called after caller cancellation")
		return nil, nil
	}))
	if _, err := server.loadKLine(ctx, "000002.SZ", "day", 60); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	items := server.sourceHealth.snapshot(time.Now())
	for _, id := range []string{"eastmoney", "sina"} {
		if sourceByID(t, items, id).CheckedAt != nil {
			t.Fatalf("caller cancellation attributed to %s", id)
		}
	}
}

func TestKLineExpiredParentDoesNotObserveUnattemptedFallback(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	server := fallbackTestServer(klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		// Simulate a provider finishing only after its caller's total deadline.
		<-ctx.Done()
		return nil, ctx.Err()
	}), klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		t.Fatal("fallback called with an expired parent")
		return nil, nil
	}))
	if _, err := server.loadKLine(ctx, "000002.SZ", "day", 60); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline lost: %v", err)
	}
	items := server.sourceHealth.snapshot(time.Now())
	if sourceByID(t, items, "eastmoney").Status != "degraded" || sourceByID(t, items, "sina").CheckedAt != nil {
		t.Fatalf("an unattempted fallback was declared failed: %+v", items)
	}
}
