package httpapi

import (
	"context"
	"errors"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

// detailPollCache limits requests from one or many open detail tabs without
// changing the contract or cache policy of the shared quote/K-line endpoints.
// Keys include the Shanghai calendar day; cached values cannot cross dates.
type detailPollCache[T any] struct {
	mu      sync.Mutex
	entries map[string]*detailPollEntry[T]
	now     func() time.Time
	active  int
}

type detailPollEntry[T any] struct {
	value      T
	hasValue   bool
	expiresAt  time.Time
	retryAt    time.Time
	lastAccess time.Time
	failures   int
	flight     *detailPollFlight[T]
}

type detailPollFlight[T any] struct {
	done      chan struct{}
	cancel    context.CancelFunc
	waiters   int
	abandoned bool
}

func newDetailPollCache[T any]() *detailPollCache[T] {
	return &detailPollCache[T]{entries: make(map[string]*detailPollEntry[T]), now: time.Now}
}

var errDetailPollCoolingDown = errors.New("source is temporarily unavailable; retry after cooldown")

func (c *detailPollCache[T]) load(ctx context.Context, key string, fetch func(context.Context) (T, error)) (T, bool, error) {
	var zero T
	for {
		now := c.now()
		c.mu.Lock()
		entry := c.entries[key]
		if entry == nil {
			if len(c.entries) >= 64 {
				// Evict only the least recently used settled entry.
				var oldestKey string
				var oldestAt time.Time
				for oldKey, oldEntry := range c.entries {
					if oldEntry.flight == nil && (oldestKey == "" || oldEntry.lastAccess.Before(oldestAt)) {
						oldestKey, oldestAt = oldKey, oldEntry.lastAccess
					}
				}
				if oldestKey == "" {
					c.mu.Unlock()
					return zero, false, errDetailPollCoolingDown
				}
				delete(c.entries, oldestKey)
			}
			entry = &detailPollEntry[T]{}
			c.entries[key] = entry
		}
		entry.lastAccess = now
		if entry.hasValue && now.Before(entry.expiresAt) {
			value := entry.value
			c.mu.Unlock()
			return value, false, nil
		}
		if now.Before(entry.retryAt) {
			value, ok := entry.value, entry.hasValue
			c.mu.Unlock()
			if ok {
				return value, true, nil
			}
			return zero, false, errDetailPollCoolingDown
		}
		if entry.flight != nil {
			flight := entry.flight
			registered := !flight.abandoned
			if registered {
				flight.waiters++
			}
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				if registered {
					c.cancelWaiter(entry, flight)
				}
				return zero, false, ctx.Err()
			case <-flight.done:
				continue
			}
		}
		if c.active >= 8 {
			value, ok := entry.value, entry.hasValue
			c.mu.Unlock()
			if ok {
				return value, true, nil
			}
			return zero, false, errDetailPollCoolingDown
		}
		c.active++
		fetchCtx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		flight := &detailPollFlight[T]{done: make(chan struct{}), cancel: cancel, waiters: 1}
		entry.flight = flight
		c.mu.Unlock()

		// The shared attempt lives while at least one tab is waiting.
		go func() {
			defer cancel()
			value, err := fetch(fetchCtx)
			completed := c.now()
			c.mu.Lock()
			if flight.abandoned {
				// An abandoned attempt cannot publish a late result into a new viewer's cache.
			} else if err == nil {
				entry.value, entry.hasValue = value, true
				entry.expiresAt = completed.Add(5 * time.Second)
				entry.retryAt = time.Time{}
				entry.failures = 0
			} else if errors.Is(err, context.Canceled) && flight.waiters == 0 {
				// All viewers left; do not punish the next viewer with a source cooldown.
			} else {
				entry.failures++
				cooldown := 30 * time.Second
				if entry.failures >= 3 {
					cooldown = time.Minute
				}
				if entry.failures >= 5 {
					cooldown = 2 * time.Minute
				}
				entry.retryAt = completed.Add(cooldown)
			}
			entry.flight = nil
			c.active--
			close(flight.done)
			c.mu.Unlock()
		}()
		select {
		case <-ctx.Done():
			c.cancelWaiter(entry, flight)
			return zero, false, ctx.Err()
		case <-flight.done:
			c.mu.Lock()
			value, ok := entry.value, entry.hasValue
			err := !entry.retryAt.IsZero()
			c.mu.Unlock()
			if !ok {
				return zero, false, errDetailPollCoolingDown
			}
			return value, err, nil
		}
	}
}

func (c *detailPollCache[T]) cancelWaiter(entry *detailPollEntry[T], flight *detailPollFlight[T]) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry.flight != flight {
		return
	}
	flight.waiters--
	if flight.waiters == 0 {
		flight.abandoned = true
		flight.cancel()
	}
}

func markHistoricalDetailQuotes(items []foundation.Quote, now time.Time) []foundation.Quote {
	day := detailTradingDay(now)
	copyOf := append([]foundation.Quote(nil), items...)
	for i := range copyOf {
		if copyOf[i].TradeTime.IsZero() || detailTradingDay(copyOf[i].TradeTime) != day {
			copyOf[i].Meta.Stale = true
			copyOf[i].Meta.FallbackReason = "行情时间不属于当前上海交易日，显示历史快照"
		}
	}
	return copyOf
}

func markHistoricalDetailKLines(items []foundation.KLine, now time.Time) []foundation.KLine {
	day := detailTradingDay(now)
	copyOf := append([]foundation.KLine(nil), items...)
	latest := time.Time{}
	for _, item := range copyOf {
		if item.Time.After(latest) {
			latest = item.Time
		}
	}
	if latest.IsZero() || detailTradingDay(latest) != day {
		for i := range copyOf {
			copyOf[i].Meta.Stale = true
			copyOf[i].Meta.FallbackReason = "分钟线不属于当前上海交易日，显示历史快照"
		}
	}
	return copyOf
}

func detailTradingDay(now time.Time) string {
	return now.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02")
}

func cloneDetailQuotesAsStale(items []foundation.Quote) []foundation.Quote {
	copyOf := append([]foundation.Quote(nil), items...)
	for index := range copyOf {
		copyOf[index].Meta.Stale = true
		if copyOf[index].Meta.FallbackReason == "" {
			copyOf[index].Meta.FallbackReason = "本机暂用最近一次可用快照，请以源行情时间为准"
		}
	}
	return copyOf
}

func cloneDetailKLinesAsStale(items []foundation.KLine) []foundation.KLine {
	copyOf := append([]foundation.KLine(nil), items...)
	for index := range copyOf {
		copyOf[index].Meta.Stale = true
		if copyOf[index].Meta.FallbackReason == "" {
			copyOf[index].Meta.FallbackReason = "本机暂用最近一次可用快照，请以源行情时间为准"
		}
	}
	return copyOf
}

func detailPollKey(symbol string, kind string, now time.Time) string {
	return symbol + ":" + kind + ":" + now.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02")
}
