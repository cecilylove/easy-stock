package contracts

import (
	"context"
	"easy-stock/backend/internal/foundation"
)

// Each interface is independently implementable. MarketOverviewProvider remains
// a compatibility facade for existing callers; it is not a supplier requirement.
type IndexProvider interface {
	MarketIndexes(context.Context, string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error)
	MarketIndexSeries(context.Context, string, string, int) (foundation.MarketIndexSeries, error)
}
type IndustryProvider interface {
	IndustryMomentum(context.Context, int) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta, error)
}
type FundFlowProvider interface {
	MarketFundFlows(context.Context, string, string, int) ([]foundation.MarketFundFlow, foundation.SourceMeta, error)
}
type MarginProvider interface {
	MarketMarginSeries(context.Context, int) ([]foundation.MarketMarginPoint, foundation.SourceMeta, error)
}
type BillboardProvider interface {
	MarketBillboard(context.Context, string, int) ([]foundation.MarketBillboardItem, foundation.SourceMeta, error)
	MarketBillboardDetail(context.Context, string, string, string) (foundation.MarketBillboardDetail, foundation.SourceMeta, error)
}
type AnnouncementProvider interface {
	MarketAnnouncements(context.Context, string, string, string, int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error)
}
type ReportProvider interface {
	MarketReports(context.Context, string, string, string, string, int) ([]foundation.MarketResearchItem, foundation.SourceMeta, error)
}
type BusinessProvider interface {
	StockBusinessProfile(context.Context, string) (foundation.StockBusinessProfile, error)
}
type FundamentalsProvider interface {
	StockFundamentals(context.Context, string) (foundation.StockFundamentals, error)
}
type StockBusinessProfileProvider interface {
	BusinessProvider
	FundamentalsProvider
}
type HotRankProvider interface {
	HotRank(context.Context, int) foundation.HotStockRankList
}
type FuturesTrendProvider interface {
	Trend(context.Context, string, int) (foundation.MarketFuturesPositionSeries, error)
}
type FuturesSnapshotProvider interface {
	LatestMembers(context.Context, string, string) (foundation.MarketFuturesMembers, error)
}
type FuturesMembersProvider interface {
	Members(context.Context, string, string) (foundation.MarketFuturesMembers, error)
}
type FuturesConsensusProvider interface {
	Consensus(context.Context, string) (foundation.MarketFuturesConsensus, error)
}
type USSectorProvider interface {
	USSectorMomentum(context.Context, int) ([]foundation.MarketUSSectorMomentum, foundation.SourceMeta, error)
}
type BoardProvider interface {
	Boards(context.Context, string, int) ([]foundation.Board, error)
	BoardStocks(context.Context, string, int) ([]foundation.BoardStock, error)
}
type BoardMemberProvider interface {
	SupportsMembers(foundation.BoardRef) bool
	Members(context.Context, foundation.BoardRef, int) ([]foundation.BoardStock, foundation.SourceMeta, error)
}
type ThemeFetcher interface {
	Fetch(context.Context, int) (foundation.ThemeSnapshot, error)
	FetchLimitUpPool(context.Context) (foundation.LimitUpPoolSnapshot, error)
}

// KLineRequest is the resolved identity of a price request. Source-default and
// explicit adjustment must never share a cache entry or silently change basis.
type KLineRequest struct {
	Symbol     string
	Period     string
	Limit      int
	SourceID   string
	Adjustment string
	Convention string
}
