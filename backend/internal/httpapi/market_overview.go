package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/datasource/service"
	"easy-stock/backend/internal/foundation"
)

type marketOverviewSnapshot struct {
	value     any
	meta      foundation.SourceMeta
	expiresAt time.Time
}

type marketOverviewCache struct {
	mu    sync.RWMutex
	ttl   time.Duration
	items map[string]marketOverviewSnapshot
}

func newMarketOverviewCache(ttl time.Duration) *marketOverviewCache {
	return &marketOverviewCache{ttl: ttl, items: map[string]marketOverviewSnapshot{}}
}

func (c *marketOverviewCache) fresh(key string) (marketOverviewSnapshot, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	item, ok := c.items[key]
	return item, ok && time.Now().Before(item.expiresAt)
}

func (c *marketOverviewCache) any(key string) (marketOverviewSnapshot, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	item, ok := c.items[key]
	return item, ok
}

func (c *marketOverviewCache) store(key string, value any, meta foundation.SourceMeta) {
	c.mu.Lock()
	c.items[key] = marketOverviewSnapshot{value: value, meta: meta, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()
}

func loadMarketOverview[T any](ctx context.Context, cache *marketOverviewCache, health *sourceHealthTracker, key string, loader func(context.Context) (T, foundation.SourceMeta, error), failureSource ...string) (T, foundation.SourceMeta, error) {
	var zero T
	if cached, ok := cache.fresh(key); ok {
		if value, typeOK := cached.value.(T); typeOK {
			return value, cached.meta, nil
		}
	}
	value, meta, err := loader(ctx)
	observe := shouldObserveFailure(ctx) && !errors.Is(err, context.Canceled) && contracts.Kind(err) != contracts.Unsupported
	if observe {
		for _, observation := range meta.Observations {
			health.observe(observation)
		}
	}
	if err == nil {
		// Only the loader ran an actual request. A cache hit must not renew a
		// previous fallback failure or make a stale snapshot look fresh.
		if observe && len(meta.Observations) == 0 {
			health.fallback(meta)
			health.success(meta)
		}
		cache.store(key, value, meta)
		return value, meta, nil
	}
	if cached, ok := cache.any(key); ok {
		if stale, typeOK := cached.value.(T); typeOK {
			cached.meta.Stale = true
			cached.meta.FallbackReason = "实时数据刷新失败，已返回最近一次成功快照：" + err.Error()
			if observe && len(meta.Observations) == 0 && len(failureSource) > 0 {
				health.cacheFailure(cached.meta, failureSource[0])
			}
			return stale, cached.meta, nil
		}
	}
	if observe && len(meta.Observations) == 0 && len(failureSource) > 0 {
		health.failure(failureSource[0], err)
	}
	return zero, foundation.SourceMeta{}, err
}

func (s *Server) marketIndexesHandler(w http.ResponseWriter, r *http.Request) {
	scope := firstNonEmpty(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("scope"))), "global")
	if scope != "global" && scope != "core" {
		writeError(w, http.StatusBadRequest, "scope must be global or core")
		return
	}
	marketOverviewList(s, w, r, "indexes:"+scope, func(ctx context.Context) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
		return s.marketOverview.MarketIndexes(ctx, scope)
	}, s.marketIndexSourceID)
}

func (s *Server) marketIndexSeriesHandler(w http.ResponseWriter, r *http.Request) {
	id := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("id")))
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	id = service.CanonicalIndexID(id)
	period := service.CanonicalIndexPeriod(r.URL.Query().Get("period"))
	if source, ok := s.marketOverview.(interface{ SupportsIndexSeries(string, string) bool }); ok {
		if !source.SupportsIndexSeries(id, period) {
			writeError(w, http.StatusBadRequest, "当前指数源不支持该标的或周期")
			return
		}
	}
	limit, err := marketLimitQuery(r, 120, 500)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	key := fmt.Sprintf("index-series:%s:%s:%d", id, period, limit)
	series, meta, err := loadMarketOverview(ctx, s.marketSnapshots, s.sourceHealth, key, func(loadCtx context.Context) (foundation.MarketIndexSeries, foundation.SourceMeta, error) {
		value, loadErr := s.marketOverview.MarketIndexSeries(loadCtx, id, period, limit)
		return value, value.Meta, loadErr
	}, s.marketIndexSourceID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	series.Meta = meta
	if series.Index.Meta.Source == "" {
		series.Index.Meta = meta
	} else {
		series.Index.Meta.Stale = meta.Stale
		series.Index.Meta.FallbackReason = meta.FallbackReason
	}
	if meta.Stale {
		// Cache reads copy the series struct, not its Lines backing array. Keep
		// per-request stale annotations off the shared immutable snapshot so
		// concurrent failed refreshes cannot mutate or race on cached bars.
		series.Lines = append([]foundation.KLine(nil), series.Lines...)
		for i := range series.Lines {
			series.Lines[i].Meta.Stale = true
			series.Lines[i].Meta.FallbackReason = meta.FallbackReason
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": series, "meta": meta})
}

func (s *Server) marketIndustriesHandler(w http.ResponseWriter, r *http.Request) {
	limit, err := marketLimitQuery(r, 50, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	marketOverviewList(s, w, r, fmt.Sprintf("industries:%d", limit), func(ctx context.Context) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error) {
		return s.marketOverview.IndustryMomentum(ctx, limit)
	}, s.marketIndustrySourceID)
}

func (s *Server) marketFlowsHandler(w http.ResponseWriter, r *http.Request) {
	dimension := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("dimension")))
	if dimension != "industry" && dimension != "theme" && dimension != "stock" {
		writeError(w, http.StatusBadRequest, "dimension must be industry, theme, or stock")
		return
	}
	sortKey := firstNonEmpty(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort"))), "net")
	if sortKey != "net" && sortKey != "change" && sortKey != "ratio" {
		writeError(w, http.StatusBadRequest, "sort must be net, change, or ratio")
		return
	}
	limit, err := marketLimitQuery(r, 50, 200)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	marketOverviewList(s, w, r, fmt.Sprintf("flows:%s:%s:%d", dimension, sortKey, limit), func(ctx context.Context) ([]foundation.MarketFundFlow, foundation.SourceMeta, error) {
		return s.marketOverview.MarketFundFlows(ctx, dimension, sortKey, limit)
	}, s.marketFlowSourceID)
}

func (s *Server) marketMarginBalanceHandler(w http.ResponseWriter, r *http.Request) {
	limit, err := marketLimitQuery(r, 120, 500)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	marketOverviewList(s, w, r, fmt.Sprintf("margin-balance:%d", limit), func(ctx context.Context) ([]foundation.MarketMarginPoint, foundation.SourceMeta, error) {
		return s.marketOverview.MarketMarginSeries(ctx, limit)
	}, s.marketFailureSourceID)
}

func (s *Server) marketBillboardHandler(w http.ResponseWriter, r *http.Request) {
	tradeDate := strings.TrimSpace(r.URL.Query().Get("trade_date"))
	if tradeDate != "" {
		if _, err := time.Parse("2006-01-02", tradeDate); err != nil {
			writeError(w, http.StatusBadRequest, "trade_date must use YYYY-MM-DD")
			return
		}
	}
	limit, err := marketLimitQuery(r, 50, 200)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	marketOverviewList(s, w, r, fmt.Sprintf("billboard:%s:%d", tradeDate, limit), func(ctx context.Context) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error) {
		return s.marketOverview.MarketBillboard(ctx, tradeDate, limit)
	}, s.marketFailureSourceID)
}

func (s *Server) marketBillboardDetailHandler(w http.ResponseWriter, r *http.Request) {
	symbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
	if symbol == "" {
		writeError(w, http.StatusBadRequest, "symbol is required")
		return
	}
	tradeDate := strings.TrimSpace(r.URL.Query().Get("trade_date"))
	if _, err := time.Parse("2006-01-02", tradeDate); err != nil {
		writeError(w, http.StatusBadRequest, "trade_date must use YYYY-MM-DD")
		return
	}
	reason := strings.TrimSpace(r.URL.Query().Get("reason"))
	if reason == "" {
		writeError(w, http.StatusBadRequest, "reason is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	key := fmt.Sprintf("billboard-detail:%s:%s:%s", symbol, tradeDate, reason)
	detail, meta, err := loadMarketOverview(ctx, s.marketSnapshots, s.sourceHealth, key, func(loadCtx context.Context) (foundation.MarketBillboardDetail, foundation.SourceMeta, error) {
		return s.marketOverview.MarketBillboardDetail(loadCtx, symbol, tradeDate, reason)
	}, s.marketFailureSourceID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	detail.Meta = meta
	writeJSON(w, http.StatusOK, map[string]any{"data": detail, "meta": meta})
}

func (s *Server) marketFuturesPositionHandler(w http.ResponseWriter, r *http.Request) {
	if s.futuresPosition == nil {
		writeError(w, http.StatusServiceUnavailable, "futures position provider is unavailable")
		return
	}
	variety := strings.ToUpper(firstNonEmpty(strings.TrimSpace(r.URL.Query().Get("variety")), "IF"))
	limit, err := marketLimitQuery(r, 60, 250)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	key := fmt.Sprintf("futures-position:%s:%d", variety, limit)
	series, meta, err := loadMarketOverview(ctx, s.marketSnapshots, s.sourceHealth, key, func(loadCtx context.Context) (foundation.MarketFuturesPositionSeries, foundation.SourceMeta, error) {
		value, loadErr := s.futuresPosition.Trend(loadCtx, variety, limit)
		return value, value.Meta, loadErr
	}, s.futuresSourceID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	series.Meta = meta
	writeJSON(w, http.StatusOK, map[string]any{"data": series, "meta": meta})
}

func (s *Server) marketFuturesMembersHandler(w http.ResponseWriter, r *http.Request) {
	if s.futuresPosition == nil {
		writeError(w, http.StatusServiceUnavailable, "futures position provider is unavailable")
		return
	}
	contract := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("contract")))
	tradeDate := strings.TrimSpace(r.URL.Query().Get("trade_date"))
	if contract == "" || tradeDate == "" {
		writeError(w, http.StatusBadRequest, "contract and trade_date are required")
		return
	}
	if _, err := time.Parse("2006-01-02", tradeDate); err != nil {
		writeError(w, http.StatusBadRequest, "trade_date must use YYYY-MM-DD")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	key := fmt.Sprintf("futures-members:%s:%s", contract, tradeDate)
	data, meta, err := loadMarketOverview(ctx, s.marketSnapshots, s.sourceHealth, key, func(loadCtx context.Context) (foundation.MarketFuturesMembers, foundation.SourceMeta, error) {
		value, loadErr := s.futuresPosition.Members(loadCtx, contract, tradeDate)
		return value, value.Meta, loadErr
	}, s.futuresMembersSourceID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	data.Meta = meta
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": meta})
}

func (s *Server) marketFuturesConsensusHandler(w http.ResponseWriter, r *http.Request) {
	if s.futuresPosition == nil {
		writeError(w, http.StatusServiceUnavailable, "futures position provider is unavailable")
		return
	}
	tradeDate := strings.TrimSpace(r.URL.Query().Get("trade_date"))
	if tradeDate != "" {
		if _, err := time.Parse("2006-01-02", tradeDate); err != nil {
			writeError(w, http.StatusBadRequest, "trade_date must use YYYY-MM-DD")
			return
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	key := "futures-consensus:" + tradeDate
	data, meta, err := loadMarketOverview(ctx, s.marketSnapshots, s.sourceHealth, key, func(loadCtx context.Context) (foundation.MarketFuturesConsensus, foundation.SourceMeta, error) {
		value, loadErr := s.futuresPosition.Consensus(loadCtx, tradeDate)
		return value, value.Meta, loadErr
	}, s.futuresConsensusSourceID)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	data.Meta = meta
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": meta})
}

func (s *Server) marketAnnouncementsHandler(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	symbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
	category := firstNonEmpty(strings.TrimSpace(r.URL.Query().Get("category")), "all")
	limit, err := marketLimitQuery(r, 50, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	marketOverviewList(s, w, r, fmt.Sprintf("announcements:%s:%s:%s:%d", query, symbol, category, limit), func(ctx context.Context) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
		return s.marketOverview.MarketAnnouncements(ctx, query, symbol, category, limit)
	}, s.marketFailureSourceID)
}

func (s *Server) marketInstitutionReportsHandler(w http.ResponseWriter, r *http.Request) {
	s.marketReportsHandler(w, r, "stock")
}

func (s *Server) marketIndustryResearchHandler(w http.ResponseWriter, r *http.Request) {
	s.marketReportsHandler(w, r, "industry")
}

func (s *Server) marketReportsHandler(w http.ResponseWriter, r *http.Request, kind string) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	symbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
	industry := strings.TrimSpace(r.URL.Query().Get("industry"))
	limit, err := marketLimitQuery(r, 50, 100)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	marketOverviewList(s, w, r, fmt.Sprintf("reports:%s:%s:%s:%s:%d", kind, query, symbol, industry, limit), func(ctx context.Context) ([]foundation.MarketResearchItem, foundation.SourceMeta, error) {
		return s.marketOverview.MarketReports(ctx, kind, query, symbol, industry, limit)
	}, s.marketFailureSourceID)
}

func marketOverviewList[T any](s *Server, w http.ResponseWriter, r *http.Request, key string, loader func(context.Context) (T, foundation.SourceMeta, error), failureSource ...string) {
	if s.marketOverview == nil {
		writeError(w, http.StatusServiceUnavailable, "market overview provider is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	data, meta, err := loadMarketOverview(ctx, s.marketSnapshots, s.sourceHealth, key, loader, failureSource...)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "meta": meta})
}

func marketLimitQuery(r *http.Request, fallback int, maximum int) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("limit"))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > maximum {
		return 0, fmt.Errorf("limit must be between 1 and %d", maximum)
	}
	return value, nil
}
