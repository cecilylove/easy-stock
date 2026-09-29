package httpapi

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

// Source status is passive: querying the catalog must never contact an upstream.
// A successful API response is not evidence of a healthy provider when it used
// a cached snapshot or a different provider's fallback.
const sourceObservationTTL = 10 * time.Minute

// A request cancelled by its caller is not evidence against an upstream.
// A service-imposed deadline is an observed failure, even when its child
// context is already done by the time the provider returns.
func shouldObserveFailure(ctx context.Context) bool {
	return ctx == nil || !errors.Is(ctx.Err(), context.Canceled)
}

type sourceObservation struct {
	checkedAt   time.Time
	lastSuccess time.Time
	lastFailure time.Time
	failed      bool
	message     string
}

type sourceHealthTracker struct {
	mu    sync.RWMutex
	items map[string]sourceObservation
}

func newSourceHealthTracker() *sourceHealthTracker {
	return &sourceHealthTracker{items: make(map[string]sourceObservation)}
}

func (t *sourceHealthTracker) success(meta foundation.SourceMeta) {
	if t == nil || meta.Stale || meta.FetchedAt.IsZero() {
		return
	}
	id := sourceID(meta.Source)
	if id == "" {
		return
	}
	// Cached responses retain their original fetch time. Never make an old
	// snapshot look like a fresh probe just because a page requested it again.
	t.mu.Lock()
	defer t.mu.Unlock()
	entry := t.items[id]
	if !meta.FetchedAt.After(entry.checkedAt) {
		return
	}
	entry.checkedAt = meta.FetchedAt
	entry.lastSuccess = meta.FetchedAt
	entry.failed = false
	entry.message = ""
	t.items[id] = entry
}

func (t *sourceHealthTracker) observe(event foundation.SourceObservation) {
	if event.Failed {
		t.markFailureAt(event.SourceID, "最近一次请求失败，请查看对应功能或运行日志", event.AttemptAt)
		return
	}
	t.fallback(event.Meta)
	t.success(event.Meta)
}

func (t *sourceHealthTracker) cacheFailure(meta foundation.SourceMeta, source string) {
	// A composed provider's previous source says nothing about which upstream
	// failed in the current refresh. Only a known single-provider route may
	// attribute the failure to that same source.
	if source != "" && sourceID(meta.Source) == source {
		t.markFailure(source, "实时更新失败，已回退到缓存快照")
	}
}

func (t *sourceHealthTracker) failure(id string, err error) {
	if err == nil {
		return
	}
	t.markFailure(id, "最近一次请求失败，请查看对应功能或运行日志")
}

func (t *sourceHealthTracker) markFailure(id, message string) {
	t.markFailureAt(id, message, time.Now())
}

func (t *sourceHealthTracker) markFailureAt(id, message string, at time.Time) {
	if t == nil || sourceID(id) == "" || at.IsZero() {
		return
	}
	t.mu.Lock()
	entry := t.items[id]
	if at.Before(entry.checkedAt) || (at.Equal(entry.checkedAt) && entry.failed) {
		t.mu.Unlock()
		return
	}
	entry.checkedAt = at
	entry.lastFailure = at
	entry.failed = true
	// Do not surface upstream response bodies or request URLs (which can
	// contain credentials) in the status catalog.
	entry.message = message
	t.items[id] = entry
	t.mu.Unlock()
}

func (t *sourceHealthTracker) fallback(meta foundation.SourceMeta) {
	if t == nil || meta.Stale || meta.FallbackReason == "" || meta.FetchedAt.IsZero() {
		return
	}
	// Known aggregator fallbacks report which primary failed. Other fallback
	// reasons may describe a missing field rather than a failed provider.
	primary := ""
	switch {
	case strings.Contains(meta.FallbackReason, "东方财富指数快照不可用"), strings.Contains(meta.FallbackReason, "东方财富指数走势不可用"), strings.Contains(meta.FallbackReason, "东方财富期指数据不可用"):
		primary = "eastmoney"
	case strings.Contains(meta.FallbackReason, "腾讯行业强度不可用"):
		primary = "tencent"
	case strings.Contains(meta.FallbackReason, "新浪资金榜不可用"):
		primary = "sina"
	}
	if primary != "" && primary != sourceID(meta.Source) {
		t.markFailure(primary, "主数据源请求失败，已切换备用来源")
	}
}

func sourceID(value string) string {
	id, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(value)), ":")
	switch id {
	case "duanxianxia", "eastmoney", "sina", "tencent", "cls", "tradingview", "tushare":
		return id
	default:
		return ""
	}
}

func (t *sourceHealthTracker) snapshot(now time.Time) []foundation.SourceHealth {
	catalog := []foundation.SourceHealth{
		{ID: "duanxianxia", Name: "短线侠 / 开盘啦", Category: "theme,leaders,limit-up,concept"},
		{ID: "eastmoney", Name: "东方财富", Category: "quote,kline,f10,report"},
		{ID: "sina", Name: "新浪财经", Category: "quote,kline,money-flow"},
		{ID: "tencent", Name: "腾讯财经", Category: "quote,index,hk"},
		{ID: "cls", Name: "财联社", Category: "news,calendar"},
		{ID: "tradingview", Name: "TradingView", Category: "news", Status: "unconfigured", Message: "当前未接入数据服务"},
		{ID: "tushare", Name: "Tushare", Category: "basic,daily,index", Status: "unconfigured", Message: "当前未接入数据服务"},
	}
	if t == nil {
		return catalog
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	for i := range catalog {
		item := &catalog[i]
		if item.Status == "unconfigured" {
			continue
		}
		item.Status = "unknown"
		item.Message = "尚未观测到实际请求"
		entry, exists := t.items[item.ID]
		if !exists {
			continue
		}
		item.CheckedAt = &entry.checkedAt
		if !entry.lastSuccess.IsZero() {
			item.LastSuccess = &entry.lastSuccess
		}
		if !entry.lastFailure.IsZero() {
			item.LastFailure = &entry.lastFailure
		}
		if now.Sub(entry.checkedAt) > sourceObservationTTL {
			item.Message = "最近观测已过期，等待实际请求"
		} else if entry.failed {
			item.Status = "degraded"
			item.Message = entry.message
		} else {
			item.Status = "available"
			item.Message = "最近一次实际请求成功"
			item.OK = true
		}
	}
	return catalog
}
