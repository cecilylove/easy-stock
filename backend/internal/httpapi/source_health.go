package httpapi

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/datasource/registry"
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
	sources      *registry.Registry
	mu           sync.RWMutex
	items        map[string]sourceObservation
	capabilities map[string]map[string]sourceObservation
}

func newSourceHealthTracker() *sourceHealthTracker {
	return newSourceHealthTrackerFor(registry.Default())
}
func newSourceHealthTrackerFor(sources *registry.Registry) *sourceHealthTracker {
	return &sourceHealthTracker{sources: sources, items: make(map[string]sourceObservation), capabilities: make(map[string]map[string]sourceObservation)}
}
func (t *sourceHealthTracker) sourceID(value string) string {
	if t == nil {
		return ""
	}
	return t.sources.SourceID(value)
}

func (t *sourceHealthTracker) success(meta foundation.SourceMeta) {
	if t == nil || meta.Stale || meta.FetchedAt.IsZero() {
		return
	}
	id := t.sourceID(meta.Source)
	if meta.Capability != "" {
		t.recordCapability(id, meta.Capability, meta.FetchedAt, false)
	}
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
	capability := event.Capability
	if capability == "" {
		capability = event.Meta.Capability
	}
	id := event.SourceID
	if id == "" {
		id = t.sourceID(event.Meta.Source)
	}
	at := event.AttemptAt
	if !event.Failed && !event.Meta.FetchedAt.IsZero() {
		at = event.Meta.FetchedAt
	}
	if !event.Meta.Stale {
		t.recordCapability(id, capability, at, event.Failed)
	}
	if event.Failed {
		// A completed empty/malformed result concerns one instrument, not the
		// whole supplier. Keep its scoped evidence without degrading the market.
		if strings.Contains(capability, ":symbol:") {
			return
		}
		t.markFailureAt(id, "最近一次请求失败，请查看对应功能或运行日志", event.AttemptAt)
		return
	}
	if capability == "" {
		t.fallback(event.Meta)
	} // Legacy events have no structured capability.
	t.success(event.Meta)
}

func (t *sourceHealthTracker) recordCapability(id, capability string, at time.Time, failed bool) {
	if t == nil || t.sourceID(id) == "" || capability == "" || at.IsZero() {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.capabilities == nil {
		t.capabilities = make(map[string]map[string]sourceObservation)
	}
	if t.capabilities[id] == nil {
		t.capabilities[id] = make(map[string]sourceObservation)
	}
	entry := t.capabilities[id][capability]
	if !at.After(entry.checkedAt) {
		return
	}
	if strings.Contains(capability, ":symbol:") {
		// Instrument keys are user-selectable. Bound their retained observations
		// independently so they cannot displace market capability evidence.
		const symbolObservationLimit = 128
		count, oldestKey := 0, ""
		var oldest time.Time
		for key, observation := range t.capabilities[id] {
			if !strings.Contains(key, ":symbol:") {
				continue
			}
			if at.Sub(observation.checkedAt) > sourceObservationTTL {
				delete(t.capabilities[id], key)
				continue
			}
			count++
			if oldestKey == "" || observation.checkedAt.Before(oldest) || (observation.checkedAt.Equal(oldest) && key < oldestKey) {
				oldestKey, oldest = key, observation.checkedAt
			}
		}
		if _, exists := t.capabilities[id][capability]; !exists && count >= symbolObservationLimit {
			delete(t.capabilities[id], oldestKey)
		}
	}
	entry.checkedAt, entry.failed = at, failed
	if failed {
		entry.lastFailure = at
		entry.message = "本能力最近请求失败，不代表该来源其他接口不可用"
		if strings.Contains(capability, ":symbol:") {
			entry.message = "该标的本次没有有效价格数据，不代表来源整体或同市场其他股票不可用"
		}
	} else {
		entry.lastSuccess = at
		entry.message = "本能力最近请求返回有效数据"
	}
	t.capabilities[id][capability] = entry
}

func (t *sourceHealthTracker) cacheFailure(meta foundation.SourceMeta, source string) {
	// A composed provider's previous source says nothing about which upstream
	// failed in the current refresh. Only a known single-provider route may
	// attribute the failure to that same source.
	if source != "" && t.sourceID(meta.Source) == source {
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
	if t == nil || t.sourceID(id) == "" || at.IsZero() {
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
	if primary != "" && primary != t.sourceID(meta.Source) {
		t.markFailure(primary, "主数据源请求失败，已切换备用来源")
	}
}

func sourceID(value string) string { return registry.Default().SourceID(value) }

func (t *sourceHealthTracker) snapshot(now time.Time) []foundation.SourceHealth {
	sources := registry.Default()
	if t != nil && t.sources != nil {
		sources = t.sources
	}
	catalog := []foundation.SourceHealth{}
	for _, d := range sources.Catalog() {
		catalog = append(catalog, foundation.SourceHealth{ID: d.ID, Name: d.Name, Category: strings.Join(d.Capabilities, ",")})
	}
	for i := range catalog {
		catalog[i].Status = "unknown"
		catalog[i].Message = "尚未观测到实际请求"
	}
	if t == nil {
		return catalog
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	for i := range catalog {
		item := &catalog[i]
		for capability, observation := range t.capabilities[item.ID] {
			status := "available"
			if now.Sub(observation.checkedAt) > sourceObservationTTL {
				status = "unknown"
			} else if observation.failed {
				status = "degraded"
			}
			row := foundation.SourceCapabilityHealth{Capability: capability, Status: status, CheckedAt: observation.checkedAt, Message: observation.message}
			if !observation.lastSuccess.IsZero() {
				value := observation.lastSuccess
				row.LastSuccess = &value
			}
			if !observation.lastFailure.IsZero() {
				value := observation.lastFailure
				row.LastFailure = &value
			}
			item.Capabilities = append(item.Capabilities, row)
		}
		sort.Slice(item.Capabilities, func(i, j int) bool { return item.Capabilities[i].Capability < item.Capabilities[j].Capability })
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
