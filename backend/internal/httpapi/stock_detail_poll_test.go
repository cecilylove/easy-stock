package httpapi

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestDetailPollCacheSharesFlightAndThrottlesAcrossTabs(t *testing.T) {
	cache := newDetailPollCache[int]()
	clock := time.Date(2026, 9, 30, 9, 35, 0, 0, time.FixedZone("CST", 8*3600))
	cache.now = func() time.Time { return clock }
	var calls atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			value, stale, err := cache.load(context.Background(), "sz000001:quote:2026-09-30", func(context.Context) (int, error) {
				calls.Add(1)
				time.Sleep(20 * time.Millisecond)
				return 42, nil
			})
			if err != nil || stale || value != 42 {
				t.Errorf("value=%d stale=%t err=%v", value, stale, err)
			}
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != 1 {
		t.Fatalf("upstream calls=%d want 1", got)
	}
	value, stale, err := cache.load(context.Background(), "sz000001:quote:2026-09-30", func(context.Context) (int, error) { calls.Add(1); return 7, nil })
	if err != nil || stale || value != 42 || calls.Load() != 1 {
		t.Fatalf("cached value=%d stale=%t calls=%d err=%v", value, stale, calls.Load(), err)
	}
}

func TestDetailPollCacheFallsBackToMarkedStaleAndBacksOff(t *testing.T) {
	cache := newDetailPollCache[[]foundation.Quote]()
	clock := time.Date(2026, 9, 30, 9, 35, 0, 0, time.FixedZone("CST", 8*3600))
	cache.now = func() time.Time { return clock }
	key := detailPollKey("000001.SZ", "quote", clock)
	calls := 0
	fetch := func(context.Context) ([]foundation.Quote, error) {
		calls++
		if calls == 1 {
			return []foundation.Quote{{Price: 11.42, Meta: foundation.SourceMeta{Source: "sina", FetchedAt: clock}}}, nil
		}
		return nil, errors.New("429 upstream")
	}
	if _, stale, err := cache.load(context.Background(), key, fetch); err != nil || stale {
		t.Fatalf("initial fetch stale=%t err=%v", stale, err)
	}
	clock = clock.Add(6 * time.Second)
	cached, stale, err := cache.load(context.Background(), key, fetch)
	if err != nil || !stale || len(cached) != 1 || calls != 2 {
		t.Fatalf("fallback %+v stale=%t calls=%d err=%v", cached, stale, calls, err)
	}
	marked := cloneDetailQuotesAsStale(cached)
	if !marked[0].Meta.Stale || cached[0].Meta.Stale || marked[0].Meta.FetchedAt != cached[0].Meta.FetchedAt {
		t.Fatalf("metadata alias or stale source time: %+v %+v", marked, cached)
	}
	clock = clock.Add(10 * time.Second)
	_, stale, err = cache.load(context.Background(), key, fetch)
	if err != nil || !stale || calls != 2 {
		t.Fatalf("failed to back off: stale=%t calls=%d err=%v", stale, calls, err)
	}
	clock = clock.Add(30 * time.Second)
	_, stale, err = cache.load(context.Background(), key, fetch)
	if err != nil || !stale || calls != 3 {
		t.Fatalf("missing backoff expiry: stale=%t calls=%d err=%v", stale, calls, err)
	}
}

func TestDetailPollCacheCancelsFlightAfterLastViewerLeaves(t *testing.T) {
	cache := newDetailPollCache[int]()
	started := make(chan struct{})
	upstreamCancelled := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := cache.load(ctx, "000001.SZ:1m:2026-09-30", func(fetchCtx context.Context) (int, error) {
			close(started)
			<-fetchCtx.Done()
			close(upstreamCancelled)
			return 0, fetchCtx.Err()
		})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("flight did not start")
	}
	cancel()
	select {
	case <-upstreamCancelled:
	case <-time.After(time.Second):
		t.Fatal("orphaned flight still occupies upstream")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("viewer err=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("viewer did not settle")
	}
}

func TestDetailPollCacheAbandonedFlightCannotReturnZeroSuccess(t *testing.T) {
	cache := newDetailPollCache[int]()
	started, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, _, err := cache.load(ctx, "same-stock", func(context.Context) (int, error) {
			close(started)
			<-release // Simulate a provider that returns after cancellation.
			return 11, nil
		})
		firstDone <- err
	}()
	select { case <-started: case <-time.After(time.Second): t.Fatal("first source did not start") }
	cancel()
	select { case err := <-firstDone: if !errors.Is(err, context.Canceled) { t.Fatalf("first viewer err=%v", err) }; case <-time.After(time.Second): t.Fatal("cancelled viewer stayed pending") }
	secondDone := make(chan struct { value int; err error }, 1)
	go func() {
		value, _, err := cache.load(context.Background(), "same-stock", func(context.Context) (int, error) { return 99, nil })
		secondDone <- struct { value int; err error }{value, err}
	}()
	close(release)
	select {
	case result := <-secondDone:
		if result.err == nil && result.value == 0 { t.Fatal("cancelled flight returned zero-value success") }
		if result.err == nil && result.value != 99 { t.Fatalf("unexpected late value=%d", result.value) }
	case <-time.After(time.Second): t.Fatal("second viewer stalled")
	}
}

func TestDetailPollCacheCapsDifferentSymbolFlights(t *testing.T) {
	cache := newDetailPollCache[int]()
	started := make(chan struct{}, 8)
	contexts := make([]context.CancelFunc, 8)
	settled := make(chan struct{}, 8)
	for index := range 8 {
		ctx, cancel := context.WithCancel(context.Background())
		contexts[index] = cancel
		go func(index int) {
			defer func() { settled <- struct{}{} }()
			_, _, _ = cache.load(ctx, fmt.Sprintf("symbol-%d", index), func(upstream context.Context) (int, error) {
				started <- struct{}{}
				<-upstream.Done()
				return 0, upstream.Err()
			})
		}(index)
	}
	for range 8 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("flight capacity was not reached")
		}
	}
	calls := 0
	_, _, err := cache.load(context.Background(), "ninth-symbol", func(context.Context) (int, error) { calls++; return 1, nil })
	if !errors.Is(err, errDetailPollCoolingDown) || calls != 0 {
		t.Fatalf("ninth request bypassed capacity: calls=%d err=%v", calls, err)
	}
	for _, cancel := range contexts {
		cancel()
	}
	for range 8 {
		select {
		case <-settled:
		case <-time.After(time.Second):
			t.Fatal("cancelled viewer stayed alive")
		}
	}
}

func TestDetailPollCacheColdFailureAndDailyKeyIsolation(t *testing.T) {
	cache := newDetailPollCache[int]()
	clock := time.Date(2026, 9, 30, 9, 15, 0, 0, time.FixedZone("CST", 8*3600))
	cache.now = func() time.Time { return clock }
	calls := 0
	fetch := func(context.Context) (int, error) { calls++; return 0, errors.New("source EOF") }
	key := detailPollKey("600519.SH", "auction", clock)
	if _, _, err := cache.load(context.Background(), key, fetch); !errors.Is(err, errDetailPollCoolingDown) {
		t.Fatalf("cold error=%v", err)
	}
	if _, _, err := cache.load(context.Background(), key, fetch); !errors.Is(err, errDetailPollCoolingDown) || calls != 1 {
		t.Fatalf("repeated failed source calls=%d err=%v", calls, err)
	}
	newDay := clock.Add(24 * time.Hour)
	if other := detailPollKey("600519.SH", "auction", newDay); other == key {
		t.Fatalf("cache crossed trading date %s", other)
	}
}
