package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
)

const ThemeLeaderSource = "duanxianxia:kaipanla-theme-leader"

// LimitUpSnapshotSource exposes retained single-source observations without leaking the supplier service.
type LimitUpSnapshotSource interface {
	LimitUpPools(context.Context, int) ([]foundation.LimitUpPoolSnapshot, foundation.ThemeFetchMeta, error)
	EarlyLimitUpPools(context.Context, int) ([]foundation.LimitUpPoolSnapshot, error)
	CachedLimitUpPools(context.Context, int) ([]foundation.LimitUpPoolSnapshot, error)
	Snapshots(context.Context, int) ([]foundation.ThemeSnapshot, foundation.ThemeFetchMeta, error)
	LimitUpPoolStale(time.Time) bool
}

type RecentLimitUpProvider interface {
	RecentLimitUps(ctx context.Context, lookbackDays int) ([]foundation.LimitUpEvent, error)
}

// LimitUpProvider keeps retained Kaipanla pools authoritative for every
// available trading day, while EastMoney supplies missing days, stocks, and
// quote fields that are absent from Kaipanla's compact pool payload.
type LimitUpProvider struct {
	primary  LimitUpSnapshotSource
	fallback RecentLimitUpProvider
}

func NewLimitUpProvider(primary LimitUpSnapshotSource, fallback RecentLimitUpProvider) *LimitUpProvider {
	return &LimitUpProvider{primary: primary, fallback: fallback}
}

// StockThemes returns retained Kaipanla per-stock attributions without
// requiring the stock to appear in the current trading day's limit-up pool.
// It deliberately returns both short-term pool concepts and trend-theme leader
// labels so the analysis engine can choose the right source for each route.
func (p *LimitUpProvider) StockThemes(ctx context.Context, symbol string, lookbackDays int) ([]foundation.StockThemeAttribution, error) {
	if p.primary == nil {
		return nil, fmt.Errorf("kaipanla theme cache is unavailable")
	}
	limit := max(lookbackDays, 2)
	pools, _, poolErr := p.primary.LimitUpPools(ctx, limit)
	snapshots, _, snapshotErr := p.primary.Snapshots(ctx, limit)
	if poolErr != nil && snapshotErr != nil {
		return nil, fmt.Errorf("kaipanla theme cache failed: pools: %v; themes: %w", poolErr, snapshotErr)
	}

	items := make([]foundation.StockThemeAttribution, 0, 8)
	for _, pool := range pools {
		for _, event := range pool.Events {
			if event.Symbol != symbol {
				continue
			}
			theme := firstCachedConcept(event.Concepts)
			if theme == "" {
				continue
			}
			source := strings.TrimSpace(event.Meta.Source)
			if source == "" {
				source = pool.Meta.Source
				if source == "" {
					source = "duanxianxia:kaipanla-limit-up"
				}
			}
			items = append(items, foundation.StockThemeAttribution{
				Kind:   foundation.ThemeAttributionPool,
				Symbol: symbol, Theme: theme, Concepts: append([]string(nil), event.Concepts...),
				Source: source, TradeDate: pool.TradeDate,
			})
		}
	}
	for _, snapshot := range snapshots {
		for _, theme := range snapshot.Themes {
			name := strings.TrimSpace(theme.Name)
			if name == "" || !theme.LeadersLoaded {
				continue
			}
			for _, leader := range theme.Leaders {
				if leader.Symbol != symbol {
					continue
				}
				items = append(items, foundation.StockThemeAttribution{
					Kind:   foundation.ThemeAttributionLeader,
					Symbol: symbol, Theme: name, Concepts: []string{name},
					Source: themeLeaderSource(snapshot), TradeDate: snapshot.TradeDate,
					Rank: theme.Rank, Role: leader.Role,
				})
			}
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].TradeDate != items[j].TradeDate {
			return items[i].TradeDate > items[j].TradeDate
		}
		leftPriority := cachedThemeSourcePriority(items[i].Source)
		rightPriority := cachedThemeSourcePriority(items[j].Source)
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		return items[i].Rank < items[j].Rank
	})
	seen := map[string]struct{}{}
	result := make([]foundation.StockThemeAttribution, 0, len(items))
	for _, item := range items {
		themeKey := compactCachedTheme(item.Theme)
		if themeKey == "" {
			continue
		}
		key := item.Source + "|" + themeKey
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, item)
	}
	return result, nil
}

func (p *LimitUpProvider) RecentLimitUps(ctx context.Context, lookbackDays int) ([]foundation.LimitUpEvent, error) {
	history, err := p.RecentLimitUpHistory(ctx, lookbackDays)
	return history.Events, err
}

func (p *LimitUpProvider) RecentLimitUpHistory(ctx context.Context, lookbackDays int) (foundation.LimitUpHistory, error) {
	if err := ctx.Err(); err != nil {
		return foundation.LimitUpHistory{}, err
	}
	var primaryEvents []foundation.LimitUpEvent
	var themeSnapshots []foundation.ThemeSnapshot
	var primaryErr error
	covered := map[string]bool{}
	if p.primary != nil {
		pools, _, err := p.primary.LimitUpPools(ctx, max(lookbackDays, 2))
		primaryErr = err
		primaryEvents, covered = limitUpPoolEvents(pools)
		if err := ctx.Err(); err != nil {
			return foundation.LimitUpHistoryFromEvents(primaryEvents, err), err
		}
		if snapshots, _, err := p.primary.Snapshots(ctx, max(lookbackDays, 2)); err == nil {
			themeSnapshots = snapshots
		}
	}
	if err := ctx.Err(); err != nil {
		return foundation.LimitUpHistoryFromEvents(primaryEvents, err), err
	}
	var fallbackHistory foundation.LimitUpHistory
	var fallbackErr error
	if p.fallback != nil {
		fallbackHistory, fallbackErr = recentLimitUpHistory(ctx, p.fallback, lookbackDays)
	}
	fallbackEvents := fallbackHistory.Events
	events := mergeLimitUpEvents(primaryEvents, fallbackEvents, primaryErr)
	if len(primaryEvents) == 0 && len(fallbackEvents) > 0 {
		reason := "开盘啦涨停池暂无可用快照"
		if primaryErr != nil {
			reason = "开盘啦涨停池不可用：" + primaryErr.Error()
		}
		events = markLimitUpFallback(events, reason)
	}
	if p.fallback == nil {
		fallbackErr = limitUpRetainedCoverage(lookbackDays, covered, primaryErr)
	}
	err := resolveLimitUpCoverage(primaryErr, fallbackErr, covered, p.fallback != nil)
	if ctx.Err() != nil {
		err = errors.Join(err, ctx.Err())
	}
	events = applyKaipanlaThemeLeaders(markLimitUpCoverage(events, err, covered), themeSnapshots)
	return combinedLimitUpHistory(events, fallbackHistory, covered, lookbackDays, err), err
}

func recentLimitUpHistory(ctx context.Context, provider RecentLimitUpProvider, days int) (foundation.LimitUpHistory, error) {
	if history, ok := provider.(contracts.LimitUpHistoryProvider); ok {
		value, err := history.RecentLimitUpHistory(ctx, days)
		if err == nil && len(value.MissingDates) > 0 {
			err = &foundation.LimitUpCoverageError{RequestedDates: append([]string(nil), value.RequestedDates...), CoveredDates: append([]string(nil), value.CoveredDates...), MissingDates: append([]string(nil), value.MissingDates...)}
		}
		return value, err
	}
	events, err := provider.RecentLimitUps(ctx, days)
	return foundation.LimitUpHistoryFromEvents(events, err), err
}

func containsLimitUpDate(dates []string, date string) bool {
	for _, value := range dates {
		if value == date {
			return true
		}
	}
	return false
}

func combinedLimitUpHistory(events []foundation.LimitUpEvent, history foundation.LimitUpHistory, primaryCovered map[string]bool, days int, err error) foundation.LimitUpHistory {
	value := foundation.LimitUpHistoryFromEvents(events, err)
	value.Meta = foundation.CloneSourceMeta(history.Meta)
	if value.Meta.Source == "" {
		for _, event := range events {
			if event.Meta.Source != "" {
				value.Meta = foundation.CloneSourceMeta(event.Meta)
				break
			}
		}
	}
	requested := append([]string(nil), history.RequestedDates...)
	if len(requested) == 0 {
		// An events-only result cannot define the request window: that drops
		// successfully retained empty days when the history route is disabled.
		requested = foundation.LimitUpRequestedDates(time.Now(), days)
		for date, valid := range primaryCovered {
			if valid && !containsLimitUpDate(requested, date) {
				requested = append(requested, date)
			}
		}
		sort.Strings(requested)
	}
	covered := map[string]bool{}
	for _, date := range history.CoveredDates {
		covered[date] = true
	}
	for date, valid := range primaryCovered {
		if valid {
			covered[date] = true
		}
	}
	// Coverage comes only from the history supplier's reported full pools or
	// validated retained pools. Merged rows (including partial pools) cannot
	// establish whole-day completeness merely by carrying a date.
	value.RequestedDates, value.CoveredDates, value.MissingDates = requested, nil, nil
	for _, date := range requested {
		if covered[date] {
			value.CoveredDates = append(value.CoveredDates, date)
		} else {
			value.MissingDates = append(value.MissingDates, date)
		}
	}
	value.Meta.Partial = err != nil || len(value.MissingDates) > 0
	return foundation.StampLimitUpHistory(value)
}

// A retained full pool (including a valid empty pool) can repair a failed day.
// Never infer coverage merely from a stock carrying a date from another pool.
func limitUpPoolEvents(pools []foundation.LimitUpPoolSnapshot) ([]foundation.LimitUpEvent, map[string]bool) {
	var events []foundation.LimitUpEvent
	covered := map[string]bool{}
	for _, pool := range pools {
		if _, err := time.Parse("2006-01-02", pool.TradeDate); err != nil {
			continue
		}
		complete := !pool.Meta.Partial && (pool.Meta.TradeDate == "" || pool.Meta.TradeDate == pool.TradeDate)
		for _, event := range pool.Events {
			if event.Date.IsZero() || event.Date.Format("2006-01-02") != pool.TradeDate || (event.Meta.TradeDate != "" && event.Meta.TradeDate != pool.TradeDate) {
				complete = false
				continue
			}
			if event.Meta.Partial {
				complete = false
			}
			events = append(events, cloneLimitUpEvent(event))
		}
		if complete {
			covered[pool.TradeDate] = true
		}
	}
	return events, covered
}

func limitUpRetainedCoverage(days int, covered map[string]bool, cause error) error {
	if days <= 0 {
		days = 12
	}
	now := time.Now().In(time.FixedZone("Asia/Shanghai", 8*3600))
	var requested, successful, missing []string
	for offset := days - 1; offset >= 0; offset-- {
		date := now.AddDate(0, 0, -offset)
		if !foundation.IsAStockTradingDay(date) {
			continue
		}
		key := date.Format("2006-01-02")
		requested = append(requested, key)
		if covered[key] {
			successful = append(successful, key)
		} else {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return cause
	}
	return &foundation.LimitUpCoverageError{RequestedDates: requested, CoveredDates: successful, MissingDates: missing, Cause: cause}
}

func resolveLimitUpCoverage(primaryErr, historyErr error, covered map[string]bool, hasHistory bool) error {
	if hasHistory && historyErr == nil {
		return nil
	} // history contract includes valid empty dates
	if historyErr == nil {
		if primaryErr != nil {
			return primaryErr
		}
		if len(covered) == 0 {
			return fmt.Errorf("no limit-up coverage is available")
		}
		return nil
	}
	var partial *foundation.LimitUpCoverageError
	if !errors.As(historyErr, &partial) || len(partial.MissingDates) == 0 || errors.Is(historyErr, context.Canceled) {
		return errors.Join(primaryErr, historyErr)
	}
	missing := make([]string, 0, len(partial.MissingDates))
	recovered := append([]string(nil), partial.CoveredDates...)
	for _, date := range partial.MissingDates {
		if covered[date] {
			recovered = append(recovered, date)
		} else {
			missing = append(missing, date)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return errors.Join(primaryErr, &foundation.LimitUpCoverageError{RequestedDates: append([]string(nil), partial.RequestedDates...), CoveredDates: recovered, MissingDates: missing, Cause: partial.Cause})
}

func markLimitUpCoverage(events []foundation.LimitUpEvent, err error, covered map[string]bool) []foundation.LimitUpEvent {
	var partial *foundation.LimitUpCoverageError
	errors.As(err, &partial)
	for i := range events {
		meta := &events[i].Meta
		if partial != nil {
			meta.Partial = true
			meta.MissingIDs = append([]string(nil), partial.MissingDates...)
			continue
		}
		if err != nil {
			meta.Partial = true
			continue
		}
		if meta.Partial && len(meta.MissingIDs) > 0 {
			restored := true
			for _, date := range meta.MissingIDs {
				if !covered[date] {
					restored = false
				}
			}
			if restored {
				meta.Partial = false
				meta.MissingIDs = nil
			}
		}
	}
	return events
}

type kaipanlaThemeLeaderAttribution struct {
	source string
	theme  string
	rank   int
	role   string
}

func applyKaipanlaThemeLeaders(events []foundation.LimitUpEvent, snapshots []foundation.ThemeSnapshot) []foundation.LimitUpEvent {
	events = cloneLimitUpEvents(events)
	if len(events) == 0 || len(snapshots) == 0 {
		return events
	}
	byStockDate := map[string]kaipanlaThemeLeaderAttribution{}
	for _, snapshot := range snapshots {
		for _, theme := range snapshot.Themes {
			name := strings.TrimSpace(theme.Name)
			if name == "" || !theme.LeadersLoaded {
				continue
			}
			for _, leader := range theme.Leaders {
				key := snapshot.TradeDate + "|" + leader.Symbol
				candidate := kaipanlaThemeLeaderAttribution{theme: name, rank: theme.Rank, role: leader.Role, source: themeLeaderSource(snapshot)}
				previous, exists := byStockDate[key]
				if !exists || candidate.rank < previous.rank || (candidate.rank == previous.rank && leader.Rank < leaderRoleRank(previous.role)) {
					byStockDate[key] = candidate
				}
			}
		}
	}
	for index := range events {
		date := ""
		if !events[index].Date.IsZero() {
			date = events[index].Date.Format("2006-01-02")
		}
		attribution, exists := byStockDate[date+"|"+events[index].Symbol]
		if !exists {
			continue
		}
		events[index].PrimaryTheme = attribution.theme
		events[index].ThemeSource = attribution.source
		events[index].ThemeKind = foundation.ThemeAttributionLeader
		events[index].ThemeRank = attribution.rank
		events[index].ThemeLeaderRole = attribution.role
		if !containsString(events[index].Concepts, attribution.theme) {
			events[index].Concepts = append([]string{attribution.theme}, events[index].Concepts...)
		}
	}
	return events
}

func leaderRoleRank(role string) int {
	for index, candidate := range []string{"龙一", "龙二", "龙三", "龙四", "龙五"} {
		if role == candidate {
			return index + 1
		}
	}
	return 99
}

func themeLeaderSource(snapshot foundation.ThemeSnapshot) string {
	if snapshot.Meta.Source == "" || snapshot.Meta.Source == "duanxianxia:kaipanla" {
		return ThemeLeaderSource
	}
	return snapshot.Meta.Source + "-leader"
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func firstCachedConcept(values []string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func cachedThemeSourcePriority(source string) int {
	if strings.Contains(source, "kaipanla-theme-leader") {
		return 0
	}
	if strings.Contains(source, "kaipanla-limit-up") {
		return 1
	}
	return 2
}

func compactCachedTheme(value string) string {
	replacer := strings.NewReplacer("概念", "", "板块", "", "产业链", "", " ", "", "-", "", "_", "")
	return strings.ToLower(replacer.Replace(strings.TrimSpace(value)))
}

func mergeLimitUpEvents(primary []foundation.LimitUpEvent, fallback []foundation.LimitUpEvent, primaryErr error) []foundation.LimitUpEvent {
	result := make([]foundation.LimitUpEvent, 0, len(primary)+len(fallback))
	for _, event := range primary {
		if validLimitUpEventDate(event) {
			result = append(result, cloneLimitUpEvent(event))
		}
	}
	index := make(map[string]int, len(primary)+len(fallback))
	primaryDate := latestLimitUpDate(primary)
	for position, event := range result {
		index[limitUpEventKey(event)] = position
	}
	for _, candidate := range fallback {
		if !validLimitUpEventDate(candidate) {
			continue
		}
		key := limitUpEventKey(candidate)
		if position, exists := index[key]; exists {
			result[position] = fillLimitUpEvent(result[position], candidate)
			continue
		}
		copyEvent := cloneLimitUpEvent(candidate)
		if primaryDate != "" && candidate.Date.Format("2006-01-02") > primaryDate {
			copyEvent.Meta.FallbackReason = "开盘啦涨停池尚未更新到当日，当前交易日使用东方财富补位"
			copyEvent.Meta.CarryForward = true
		} else if primaryErr != nil && strings.TrimSpace(copyEvent.Meta.FallbackReason) == "" {
			copyEvent.Meta.FallbackReason = "开盘啦涨停池刷新失败，使用东方财富补位"
		}
		index[key] = len(result)
		result = append(result, copyEvent)
	}
	return result
}

func fillLimitUpEvent(primary foundation.LimitUpEvent, fallback foundation.LimitUpEvent) foundation.LimitUpEvent {
	primary = cloneLimitUpEvent(primary)
	if !validLimitUpEventDate(primary) || !validLimitUpEventDate(fallback) || limitUpEventKey(primary) != limitUpEventKey(fallback) {
		return primary
	}
	// Freeze the compatibility mask before adding hydration fields.
	if !primary.Meta.FieldsKnown && len(primary.Meta.AvailableFields) == 0 {
		legacy := primary
		for _, field := range []string{"price", "change_percent", "amount", "float_market_cap", "turnover_rate", "streak", "open_count", "days", "count"} {
			if foundation.LimitUpFieldAvailable(legacy, field) {
				primary.Meta.AvailableFields = append(primary.Meta.AvailableFields, field)
			}
		}
		for _, field := range []struct{ key, value string }{{"name", primary.Name}, {"first_limit_time", primary.FirstLimitTime}, {"last_limit_time", primary.LastLimitTime}, {"industry", primary.Industry}} {
			if field.value != "" && field.value != "--" {
				primary.Meta.AvailableFields = append(primary.Meta.AvailableFields, field.key)
			}
		}
		primary.Meta.FieldsKnown = true
	}
	record := func(field string) {
		if !containsString(primary.Meta.AvailableFields, field) {
			primary.Meta.AvailableFields = append(primary.Meta.AvailableFields, field)
		}
		if primary.Meta.FieldSources == nil {
			primary.Meta.FieldSources = map[string]string{}
		}
		if primary.Meta.FieldFetchedAt == nil {
			primary.Meta.FieldFetchedAt = map[string]time.Time{}
		}
		source := fallback.Meta.Source
		if value := fallback.Meta.FieldSources[field]; value != "" {
			source = value
		}
		fetchedAt := fallback.Meta.FetchedAt
		if value, ok := fallback.Meta.FieldFetchedAt[field]; ok {
			fetchedAt = value
		}
		primary.Meta.FieldSources[field], primary.Meta.FieldFetchedAt[field] = source, fetchedAt
	}
	for _, field := range []struct {
		key    string
		target *float64
		value  float64
	}{
		{"price", &primary.Price, fallback.Price}, {"change_percent", &primary.ChangePercent, fallback.ChangePercent}, {"amount", &primary.Amount, fallback.Amount}, {"float_market_cap", &primary.FloatMarketCap, fallback.FloatMarketCap}, {"turnover_rate", &primary.TurnoverRate, fallback.TurnoverRate},
	} {
		if !foundation.LimitUpFieldAvailable(primary, field.key) && foundation.LimitUpFieldAvailable(fallback, field.key) {
			*field.target = field.value
			record(field.key)
		}
	}
	for _, field := range []struct {
		key    string
		target *int
		value  int
	}{
		{"streak", &primary.Streak, fallback.Streak}, {"open_count", &primary.OpenCount, fallback.OpenCount}, {"days", &primary.Days, fallback.Days}, {"count", &primary.Count, fallback.Count},
	} {
		if !foundation.LimitUpFieldAvailable(primary, field.key) && foundation.LimitUpFieldAvailable(fallback, field.key) {
			*field.target = field.value
			record(field.key)
		}
	}
	for _, field := range []struct {
		key    string
		target *string
		value  string
	}{
		{"name", &primary.Name, fallback.Name}, {"first_limit_time", &primary.FirstLimitTime, fallback.FirstLimitTime}, {"last_limit_time", &primary.LastLimitTime, fallback.LastLimitTime}, {"industry", &primary.Industry, fallback.Industry},
	} {
		present := strings.TrimSpace(*field.target) != "" && *field.target != "--" && foundation.FieldAvailable(primary.Meta, field.key)
		available := strings.TrimSpace(field.value) != "" && field.value != "--" && foundation.FieldAvailable(fallback.Meta, field.key)
		if !present && available {
			*field.target = field.value
			record(field.key)
		}
	}
	return primary
}

func validLimitUpEventDate(event foundation.LimitUpEvent) bool {
	return !event.Date.IsZero() && (event.Meta.TradeDate == "" || event.Meta.TradeDate == event.Date.Format("2006-01-02"))
}

func markLimitUpFallback(events []foundation.LimitUpEvent, reason string) []foundation.LimitUpEvent {
	result := cloneLimitUpEvents(events)
	for index := range result {
		if strings.TrimSpace(result[index].Meta.FallbackReason) == "" {
			result[index].Meta.FallbackReason = reason
		}
	}
	return result
}

func cloneLimitUpEvents(events []foundation.LimitUpEvent) []foundation.LimitUpEvent {
	result := make([]foundation.LimitUpEvent, len(events))
	for index, event := range events {
		result[index] = cloneLimitUpEvent(event)
	}
	return result
}

func cloneLimitUpEvent(event foundation.LimitUpEvent) foundation.LimitUpEvent {
	event.Concepts = append([]string(nil), event.Concepts...)
	event.Meta = foundation.CloneSourceMeta(event.Meta)
	return event
}

func latestLimitUpDate(events []foundation.LimitUpEvent) string {
	latest := ""
	for _, event := range events {
		if event.Date.IsZero() {
			continue
		}
		date := event.Date.Format("2006-01-02")
		if date > latest {
			latest = date
		}
	}
	return latest
}

func limitUpEventKey(event foundation.LimitUpEvent) string {
	date := ""
	if !event.Date.IsZero() {
		date = event.Date.Format("2006-01-02")
	}
	return date + "|" + event.Symbol
}

var _ RecentLimitUpProvider = (*LimitUpProvider)(nil)

// CachedLimitUps restores retained pools without starting a remote refresh.
func (p *LimitUpProvider) CachedLimitUps(ctx context.Context, days int) ([]foundation.LimitUpEvent, error) {
	if p.primary == nil {
		return nil, nil
	}
	pools, err := p.primary.CachedLimitUpPools(ctx, max(days, 2))
	events, _ := limitUpPoolEvents(pools)
	return events, err
}

// ProgressiveLimitUps publishes immutable, cumulative pools as each source completes.
func (p *LimitUpProvider) ProgressiveLimitUps(ctx context.Context, days int, publish func([]foundation.LimitUpEvent, string, error)) {
	_, _ = p.ProgressiveLimitUpHistory(ctx, days, func(value foundation.LimitUpHistory, stage string, err error) {
		if publish != nil {
			publish(value.Events, stage, err)
		}
	})
}

// ProgressiveLimitUpHistory preserves the final empty-date coverage without a
// second fetch; the legacy callback remains available above.
func (p *LimitUpProvider) ProgressiveLimitUpHistory(ctx context.Context, days int, publish func(foundation.LimitUpHistory, string, error)) (foundation.LimitUpHistory, error) {
	if ctx.Err() != nil {
		return foundation.LimitUpHistory{}, ctx.Err()
	}
	var last foundation.LimitUpHistory
	var lastErr error
	type result struct {
		stage   string
		events  []foundation.LimitUpEvent
		history foundation.LimitUpHistory
		themes  []foundation.ThemeSnapshot
		covered map[string]bool
		err     error
	}
	updates := make(chan result, 3)
	send := func(item result) {
		select {
		case updates <- result{stage: item.stage, events: cloneLimitUpEvents(item.events), history: foundation.CloneLimitUpHistory(item.history), themes: item.themes, covered: item.covered, err: item.err}:
		case <-ctx.Done():
		}
	}
	go func() {
		var events []foundation.LimitUpEvent
		var err error
		covered := map[string]bool{}
		if p.primary != nil {
			var pools []foundation.LimitUpPoolSnapshot
			pools, err = p.primary.EarlyLimitUpPools(ctx, max(days, 2))
			_, covered = limitUpPoolEvents(pools)
			for _, pool := range pools {
				items, _ := limitUpPoolEvents([]foundation.LimitUpPoolSnapshot{pool})
				for i := range items {
					items[i].Meta.Stale = p.primary.LimitUpPoolStale(pool.FetchedAt)
				}
				events = append(events, items...)
			}
		} else {
			err = fmt.Errorf("开盘啦涨停池不可用")
		}
		send(result{stage: "primary", events: events, covered: covered, err: err})
	}()
	go func() {
		var history foundation.LimitUpHistory
		var err error
		if progressive, ok := p.fallback.(contracts.ProgressiveLimitUpHistoryProvider); ok {
			history, err = progressive.ProgressiveRecentLimitUpHistory(ctx, days, func(value foundation.LimitUpHistory) {
				send(result{stage: "history_partial", events: value.Events, history: value})
			})
		} else if _, ok := p.fallback.(contracts.LimitUpHistoryProvider); ok {
			history, err = recentLimitUpHistory(ctx, p.fallback, days)
		} else if progressive, ok := p.fallback.(contracts.ProgressiveRecentLimitUpProvider); ok {
			var events []foundation.LimitUpEvent
			events, err = progressive.ProgressiveRecentLimitUps(ctx, days, func(items []foundation.LimitUpEvent) {
				send(result{stage: "history_partial", events: items, history: foundation.LimitUpHistoryFromEvents(items, nil)})
			})
			history = foundation.LimitUpHistoryFromEvents(events, err)
		} else if p.fallback != nil {
			history, err = recentLimitUpHistory(ctx, p.fallback, days)
		} else {
			err = fmt.Errorf("历史涨停池不可用")
		}
		send(result{stage: "history", events: history.Events, history: history, err: err})
	}()
	go func() {
		var themes []foundation.ThemeSnapshot
		var err error
		if p.primary != nil {
			themes, _, err = p.primary.Snapshots(ctx, max(days, 2))
		}
		send(result{stage: "themes", themes: themes, err: err})
	}()
	var primary, fallback []foundation.LimitUpEvent
	var fallbackHistory foundation.LimitUpHistory
	var themes []foundation.ThemeSnapshot
	var primaryErr, historyErr error
	covered := map[string]bool{}
	historyDone := false
	for remaining := 3; remaining > 0; {
		select {
		case <-ctx.Done():
			return last, errors.Join(lastErr, ctx.Err())
		case item := <-updates:
			if item.stage != "history_partial" {
				remaining--
			}
			switch item.stage {
			case "primary":
				primary, primaryErr, covered = item.events, item.err, item.covered
			case "history":
				fallback, historyErr, historyDone = item.events, item.err, true
				fallbackHistory = item.history
			case "history_partial":
				fallback = item.events
				fallbackHistory = item.history
			case "themes":
				themes = item.themes
			}
			events := mergeLimitUpEvents(primary, fallback, primaryErr)
			if len(primary) == 0 && len(fallback) > 0 {
				events = markLimitUpFallback(events, "开盘啦涨停池暂无可用快照，使用东方财富补位")
			}
			if historyDone && historyErr == nil && len(fallbackHistory.MissingDates) > 0 {
				historyErr = &foundation.LimitUpCoverageError{RequestedDates: append([]string(nil), fallbackHistory.RequestedDates...), CoveredDates: append([]string(nil), fallbackHistory.CoveredDates...), MissingDates: append([]string(nil), fallbackHistory.MissingDates...)}
			}
			err := errors.Join(primaryErr, historyErr)
			if historyDone {
				coverageErr := historyErr
				if p.fallback == nil {
					coverageErr = limitUpRetainedCoverage(days, covered, primaryErr)
				}
				err = resolveLimitUpCoverage(primaryErr, coverageErr, covered, p.fallback != nil)
			}
			if item.stage == "themes" {
				err = errors.Join(err, item.err)
			}
			events = markLimitUpCoverage(events, err, covered)
			last = combinedLimitUpHistory(applyKaipanlaThemeLeaders(events, themes), fallbackHistory, covered, days, err)
			lastErr = err
			if publish != nil && ctx.Err() == nil {
				publish(foundation.CloneLimitUpHistory(last), item.stage, err)
			}
		}
	}
	if ctx.Err() != nil {
		return last, errors.Join(lastErr, ctx.Err())
	}
	return last, lastErr
}
