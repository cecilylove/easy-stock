package httpapi

import (
	"context"
	"errors"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/foundation"
)

const sourceProbeTimeout = 12 * time.Second
const sourceProbeBatchTimeout = 20 * time.Second

// Manual probes are representative direct requests, independent of the passive
// business observations. Their success must not clear a failed business request.
type SourceProbeResult struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	CheckedAt time.Time `json:"checked_at"`
	Scope     string    `json:"scope"`
	Message   string    `json:"message"`
	LatencyMS int64     `json:"latency_ms"`
}

type SourceProbeIndexProvider interface {
	MarketIndexes(context.Context, string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error)
}

type SourceProbeFuturesProvider interface {
	Trend(context.Context, string, int) (foundation.MarketFuturesPositionSeries, error)
}

type SourceProbePoolProvider interface {
	FetchLimitUpPool(context.Context) (foundation.LimitUpPoolSnapshot, error)
}

// All providers must request their named upstream directly, with no cache or
// alternate source. An explicit configuration replaces the entire probe set.
type SourceProbeProviders struct {
	Sina      RealtimeProvider
	EastMoney StockDirectoryProvider
	Tencent   SourceProbeIndexProvider
	CLS       NewsProvider
	THS       HotStockProvider
	CFFEX     SourceProbeFuturesProvider
	Kaipanla  SourceProbePoolProvider
}

type sourceProbe struct {
	id, scope string
	check     func(context.Context, time.Time) error
}

var errSourceProbeData = errors.New("invalid representative data")
var errSourceProbeUnavailable = errors.New("probe provider unavailable")

func validProbeMeta(meta foundation.SourceMeta, id string, started time.Time) bool {
	return sourceID(meta.Source) == id && !meta.Stale && !meta.CarryForward &&
		!meta.FetchedAt.Before(started)
}

func positiveProbePrice(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validProbeDate(date string) bool {
	_, err := time.Parse("2006-01-02", date)
	return err == nil
}

func sourceProbeDefinitions(p SourceProbeProviders) []sourceProbe {
	return []sourceProbe{
		{"duanxianxia", "开盘啦直接涨停池", func(ctx context.Context, started time.Time) error {
			if p.Kaipanla == nil {
				return errSourceProbeUnavailable
			}
			pool, err := p.Kaipanla.FetchLimitUpPool(ctx)
			if err != nil {
				return err
			}
			if pool.FetchedAt.Before(started) || !validProbeDate(pool.TradeDate) {
				return errSourceProbeData
			}
			for _, event := range pool.Events {
				if _, err := foundation.NormalizeSymbol(event.Symbol); err == nil && strings.TrimSpace(event.Name) != "" && validProbeMeta(event.Meta, "duanxianxia", started) && event.Meta.FallbackReason == "" {
					return nil
				}
			}
			return errSourceProbeData
		}},
		{"eastmoney", "股票目录与名称代表接口（保留能力；不检测已停用K线）", func(ctx context.Context, started time.Time) error {
			if p.EastMoney == nil {
				return errSourceProbeUnavailable
			}
			stocks, err := p.EastMoney.StockCatalog(ctx)
			if err != nil {
				return err
			}
			for _, stock := range stocks {
				if _, symbolErr := foundation.NormalizeSymbol(stock.Symbol); symbolErr == nil && strings.TrimSpace(stock.Name) != "" && validProbeMeta(stock.Meta, "eastmoney", started) && stock.Meta.FallbackReason == "" {
					return nil
				}
			}
			return errSourceProbeData
		}},
		{"sina", "平安银行代表行情（000001.SZ）", func(ctx context.Context, started time.Time) error {
			if p.Sina == nil {
				return errSourceProbeUnavailable
			}
			quotes, err := p.Sina.Realtime(ctx, []string{"000001.SZ"})
			if err != nil {
				return err
			}
			for _, quote := range quotes {
				if quote.Symbol == "000001.SZ" && strings.TrimSpace(quote.Name) != "" && positiveProbePrice(quote.Price) && validProbeMeta(quote.Meta, "sina", started) && quote.Meta.FallbackReason == "" {
					return nil
				}
			}
			return errSourceProbeData
		}},
		{"tencent", "上证指数代表行情", func(ctx context.Context, started time.Time) error {
			if p.Tencent == nil {
				return errSourceProbeUnavailable
			}
			indexes, meta, err := p.Tencent.MarketIndexes(ctx, "core")
			if err != nil {
				return err
			}
			if !validProbeMeta(meta, "tencent", started) || meta.FallbackReason != "" {
				return errSourceProbeData
			}
			for _, index := range indexes {
				if index.ID == "sse" && positiveProbePrice(index.Price) && validProbeMeta(index.Meta, "tencent", started) && index.Meta.FallbackReason == "" {
					return nil
				}
			}
			return errSourceProbeData
		}},
		{"cls", "财联社最新电报", func(ctx context.Context, started time.Time) error {
			if p.CLS == nil {
				return errSourceProbeUnavailable
			}
			news, err := p.CLS.LatestNews(ctx, 1)
			if err != nil {
				return err
			}
			for _, item := range news {
				if strings.TrimSpace(item.Title) != "" && validProbeMeta(item.Meta, "cls", started) && item.Meta.FallbackReason == "" {
					return nil
				}
			}
			return errSourceProbeData
		}},
		{"ths", "同花顺公开个股热榜", func(ctx context.Context, started time.Time) error {
			if p.THS == nil {
				return errSourceProbeUnavailable
			}
			for _, list := range p.THS.HotStockRanks(ctx, 1) {
				if list.Source != "ths" {
					continue
				}
				if list.Error != "" {
					return errSourceProbeUnavailable
				}
				if list.FetchedAt.Before(started) {
					return errSourceProbeData
				}
				for _, item := range list.Items {
					if _, err := foundation.NormalizeSymbol(item.Symbol); err == nil && strings.TrimSpace(item.Name) != "" && item.Rank > 0 {
						return nil
					}
				}
			}
			return errSourceProbeData
		}},
		{"cffex", "中金所IF最近交易日单日持仓快照", func(ctx context.Context, started time.Time) error {
			if p.CFFEX == nil {
				return errSourceProbeUnavailable
			}
			series, err := p.CFFEX.Trend(ctx, "IF", 1)
			if err != nil {
				return err
			}
			if series.Variety != "IF" || !validProbeMeta(series.Meta, "cffex", started) {
				return errSourceProbeData
			}
			// The exchange-only client's descriptive fallback reason denotes its
			// single-day scope. Older trading dates on holidays are valid data.
			for _, row := range series.Rows {
				if validProbeDate(row.TradeDate) && row.LongPosition > 0 && row.ShortPosition > 0 {
					return nil
				}
			}
			return errSourceProbeData
		}},
	}
}

type sourceProbeBatch struct {
	done      chan struct{}
	cancel    context.CancelFunc
	waiters   int
	results   []SourceProbeResult
	checkedAt time.Time
	err       error
}

type sourceProbeTracker struct {
	mu      sync.Mutex
	probes  []sourceProbe
	last    []SourceProbeResult
	active  *sourceProbeBatch
	closed  bool
	timeout time.Duration
}

func newSourceProbeTracker(providers SourceProbeProviders) *sourceProbeTracker {
	return newSourceProbeTrackerFor(registry.Default(), providers)
}
func newSourceProbeTrackerFor(sources *registry.Registry, providers SourceProbeProviders) *sourceProbeTracker {
	legacy := map[string]sourceProbe{}
	for _, p := range sourceProbeDefinitions(providers) {
		legacy[p.id] = p
	}
	probes := []sourceProbe{}
	for _, entry := range sources.Entries() {
		d := entry.Descriptor
		if !d.Enabled || !d.Implemented || d.ProbeScope == "" {
			continue
		}
		p := sourceProbe{id: d.ID, scope: d.ProbeScope, check: entry.Probe}
		if p.check == nil {
			if old, ok := legacy[d.ID]; ok {
				p.check = old.check
			} else {
				p.check = func(context.Context, time.Time) error { return errSourceProbeUnavailable }
			}
		}
		probes = append(probes, p)
	}
	return &sourceProbeTracker{probes: probes, timeout: sourceProbeTimeout}
}

func (t *sourceProbeTracker) snapshot() []SourceProbeResult {
	if t == nil {
		return []SourceProbeResult{}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]SourceProbeResult{}, t.last...)
}

func (t *sourceProbeTracker) check(ctx context.Context) ([]SourceProbeResult, time.Time, error) {
	t.mu.Lock()
	if t.closed || ctx.Err() != nil {
		t.mu.Unlock()
		return nil, time.Time{}, context.Canceled
	}
	batch := t.active
	if batch == nil {
		batchCtx, cancel := context.WithTimeout(context.Background(), sourceProbeBatchTimeout)
		batch = &sourceProbeBatch{done: make(chan struct{}), cancel: cancel}
		t.active = batch
		go t.run(batchCtx, batch)
	}
	batch.waiters++
	t.mu.Unlock()
	select {
	case <-ctx.Done():
		t.mu.Lock()
		batch.waiters--
		if batch.waiters == 0 && t.active == batch {
			t.active = nil
			batch.cancel()
		}
		t.mu.Unlock()
		return nil, time.Time{}, ctx.Err()
	case <-batch.done:
		return append([]SourceProbeResult{}, batch.results...), batch.checkedAt, batch.err
	}
}

func (t *sourceProbeTracker) run(ctx context.Context, batch *sourceProbeBatch) {
	defer batch.cancel()
	results := make([]SourceProbeResult, len(t.probes))
	var group sync.WaitGroup
	for i, probe := range t.probes {
		group.Add(1)
		go func(i int, probe sourceProbe) {
			defer group.Done()
			started := time.Now()
			probeCtx, cancel := context.WithTimeout(ctx, t.timeout)
			defer cancel()
			err := probe.check(probeCtx, started)
			if probeCtx.Err() != nil {
				err = probeCtx.Err()
			}
			result := SourceProbeResult{ID: probe.id, Scope: probe.scope, CheckedAt: time.Now(), LatencyMS: time.Since(started).Milliseconds(), Status: "available", Message: "代表接口返回有效数据"}
			if err != nil {
				result.Status = "unavailable"
				result.Message = sourceProbeErrorMessage(err)
			}
			results[i] = result
		}(i, probe)
	}
	group.Wait()
	t.mu.Lock()
	defer t.mu.Unlock()
	batch.results, batch.checkedAt, batch.err = results, time.Now(), ctx.Err()
	if t.active == batch {
		t.active = nil
		if batch.err == nil && !t.closed {
			t.last = append([]SourceProbeResult{}, results...)
		}
	}
	close(batch.done)
}

func sourceProbeErrorMessage(err error) string {
	var networkError net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()):
		return "检测超时，请稍后重试"
	case errors.Is(err, context.Canceled):
		return "检测已取消"
	case errors.Is(err, errSourceProbeData):
		return "代表接口未返回有效数据"
	default:
		return "代表接口请求失败，请稍后重试"
	}
}

func (t *sourceProbeTracker) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	if t.active != nil {
		t.active.cancel()
	}
}

func (s *Server) checkSources(w http.ResponseWriter, r *http.Request) {
	results, checkedAt, err := s.sourceProbes.check(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": sourceProbeErrorMessage(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sources": s.sourceHealth.snapshot(time.Now()), "probes": results, "checked_at": checkedAt, "catalog": s.sourceCatalog(),
	})
}
