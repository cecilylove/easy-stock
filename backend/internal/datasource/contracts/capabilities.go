// Package contracts defines supplier-neutral, typed data access capabilities.
// Providers implement only the capabilities they actually support.
package contracts

import (
	"context"
	"easy-stock/backend/internal/foundation"
	"time"
)

type RealtimeProvider interface {
	Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error)
}

type AuctionProvider interface {
	AuctionTrace(ctx context.Context, symbol string) (foundation.AuctionTrace, error)
}

type KLineProvider interface {
	KLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error)
}

type HistoryIntradayProvider interface {
	HistoryIntraday(context.Context, string, string) (foundation.StockIntradayHistory, error)
}

type AdjustedKLineProvider interface {
	KLineAdjusted(context.Context, string, string, int, string) ([]foundation.KLine, error)
}

type NewsProvider interface {
	LatestNews(ctx context.Context, limit int) ([]foundation.NewsItem, error)
}

type LimitUpProvider interface {
	RecentLimitUps(ctx context.Context, lookbackDays int) ([]foundation.LimitUpEvent, error)
}

// LimitUpHistoryProvider preserves successfully fetched empty trading days.
// Optional so existing events-only suppliers remain compatible.
type LimitUpHistoryProvider interface {
	RecentLimitUpHistory(context.Context, int) (foundation.LimitUpHistory, error)
}

// ProgressiveLimitUpHistoryProvider returns final coverage from the same fetch;
// callers must not issue a second history request to discover empty days.
type ProgressiveLimitUpHistoryProvider interface {
	ProgressiveRecentLimitUpHistory(context.Context, int, func(foundation.LimitUpHistory)) (foundation.LimitUpHistory, error)
}

// ProgressiveRecentLimitUpProvider publishes immutable cumulative history.
// Its final error may accompany useful events and must retain incomplete coverage.
type ProgressiveRecentLimitUpProvider interface {
	ProgressiveRecentLimitUps(context.Context, int, func([]foundation.LimitUpEvent)) ([]foundation.LimitUpEvent, error)
}

type StockThemeAttributionProvider interface {
	StockThemes(ctx context.Context, symbol string, lookbackDays int) ([]foundation.StockThemeAttribution, error)
}

type MarketPoolProvider interface {
	BrokenLimitUpPool(ctx context.Context, date time.Time) ([]foundation.MarketLimitEvent, error)
	LimitDownPool(ctx context.Context, date time.Time) ([]foundation.MarketLimitEvent, error)
}

type StockDirectoryProvider interface {
	StockCatalog(ctx context.Context) ([]foundation.StockCatalogEntry, error)
}

type HotStockProvider interface {
	HotStockRanks(ctx context.Context, limit int) []foundation.HotStockRankList
}

type FuturesPositionProvider interface {
	Trend(ctx context.Context, variety string, limit int) (foundation.MarketFuturesPositionSeries, error)
	Members(ctx context.Context, contract string, tradeDate string) (foundation.MarketFuturesMembers, error)
	Consensus(ctx context.Context, tradeDate string) (foundation.MarketFuturesConsensus, error)
}

type MarketOverviewProvider interface {
	MarketIndexes(ctx context.Context, scope string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error)
	MarketIndexSeries(ctx context.Context, id string, period string, limit int) (foundation.MarketIndexSeries, error)
	IndustryMomentum(ctx context.Context, limit int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error)
	MarketFundFlows(ctx context.Context, dimension string, sortKey string, limit int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error)
	MarketMarginSeries(ctx context.Context, limit int) ([]foundation.MarketMarginPoint, foundation.SourceMeta, error)
	MarketBillboard(ctx context.Context, tradeDate string, limit int) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error)
	MarketBillboardDetail(ctx context.Context, symbol string, tradeDate string, reason string) (foundation.MarketBillboardDetail, foundation.SourceMeta, error)
	MarketAnnouncements(ctx context.Context, query string, symbol string, category string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error)
	MarketReports(ctx context.Context, kind string, query string, symbol string, industry string, limit int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error)
}
