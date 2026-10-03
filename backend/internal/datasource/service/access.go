package service

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/datasource/runtime"
	"easy-stock/backend/internal/foundation"
	"time"
)

// Access projects a registered supplier's independent abilities. Disabled or
// removed capabilities return Unsupported, instead of reviving a default source.
// It has no page cache: detail, WS and research retain their own freshness rules.
type Access struct {
	sourceID string
	cap      registry.Capabilities
}

func NewAccess(id string, cap registry.Capabilities) *Access { return &Access{sourceID: id, cap: cap} }
func (s *Access) unsupported(capability string) error {
	return &contracts.Error{Kind: contracts.Unsupported, SourceID: s.sourceID, Capability: capability}
}
func (s *Access) Realtime(ctx context.Context, symbols []string) ([]foundation.Quote, error) {
	if s.cap.Realtime == nil {
		return nil, s.unsupported("quote")
	}
	return runtime.Call(ctx, 0, func(ctx context.Context) ([]foundation.Quote, error) { return s.cap.Realtime.Realtime(ctx, symbols) })
}
func (s *Access) AuctionTrace(ctx context.Context, symbol string) (foundation.AuctionTrace, error) {
	if s.cap.Auction == nil {
		return foundation.AuctionTrace{}, s.unsupported("auction")
	}
	return s.cap.Auction.AuctionTrace(ctx, symbol)
}
func (s *Access) KLine(ctx context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	if s.cap.KLine == nil {
		return nil, s.unsupported("kline")
	}
	return s.cap.KLine.KLine(ctx, symbol, period, limit)
}
func (s *Access) SupportsKLine(symbol, period string) bool {
	return SourceKLineSupports(s.cap.KLine, symbol, period)
}
func (s *Access) KLineAdjusted(ctx context.Context, symbol, period string, limit int, adjust string) ([]foundation.KLine, error) {
	if s.cap.AdjustedKLine == nil {
		return nil, s.unsupported("adjusted-kline")
	}
	return s.cap.AdjustedKLine.KLineAdjusted(ctx, symbol, period, limit, adjust)
}
func (s *Access) SupportsAdjustedKLine(symbol, period, adjust string) bool {
	if p, ok := s.cap.AdjustedKLine.(interface {
		SupportsAdjustedKLine(string, string, string) bool
	}); ok {
		return p.SupportsAdjustedKLine(symbol, period, adjust)
	}
	return s.cap.AdjustedKLine != nil
}
func (s *Access) HistoryIntraday(ctx context.Context, symbol, date string) (foundation.StockIntradayHistory, error) {
	if s.cap.HistoryIntraday == nil {
		return foundation.StockIntradayHistory{}, s.unsupported("historical-intraday")
	}
	return s.cap.HistoryIntraday.HistoryIntraday(ctx, symbol, date)
}
func (s *Access) StockCatalog(ctx context.Context) ([]foundation.StockCatalogEntry, error) {
	if s.cap.Directory == nil {
		return nil, s.unsupported("stock-directory")
	}
	return s.cap.Directory.StockCatalog(ctx)
}
func (s *Access) StockBusinessProfile(ctx context.Context, symbol string) (foundation.StockBusinessProfile, error) {
	if s.cap.Business == nil {
		return foundation.StockBusinessProfile{}, s.unsupported("business")
	}
	return s.cap.Business.StockBusinessProfile(ctx, symbol)
}
func (s *Access) StockFundamentals(ctx context.Context, symbol string) (foundation.StockFundamentals, error) {
	if s.cap.Fundamentals == nil {
		return foundation.StockFundamentals{}, s.unsupported("fundamentals")
	}
	return s.cap.Fundamentals.StockFundamentals(ctx, symbol)
}
func (s *Access) RecentLimitUps(ctx context.Context, days int) ([]foundation.LimitUpEvent, error) {
	if s.cap.LimitUp == nil {
		return nil, s.unsupported("limit-up")
	}
	return s.cap.LimitUp.RecentLimitUps(ctx, days)
}

// RecentLimitUpHistory forwards neutral coverage, falling back conservatively
// for old suppliers without inferring empty dates from an empty event slice.
func (s *Access) RecentLimitUpHistory(ctx context.Context, days int) (foundation.LimitUpHistory, error) {
	if err := ctx.Err(); err != nil {
		return foundation.LimitUpHistory{}, err
	}
	if s.cap.LimitUp == nil {
		return foundation.LimitUpHistory{}, s.unsupported("limit-up")
	}
	if provider, ok := s.cap.LimitUp.(contracts.LimitUpHistoryProvider); ok {
		history, err := provider.RecentLimitUpHistory(ctx, days)
		return foundation.CloneLimitUpHistory(history), err
	}
	events, err := s.cap.LimitUp.RecentLimitUps(ctx, days)
	return foundation.LimitUpHistoryFromEvents(events, err), err
}

func (s *Access) ProgressiveRecentLimitUpHistory(ctx context.Context, days int, publish func(foundation.LimitUpHistory)) (foundation.LimitUpHistory, error) {
	if err := ctx.Err(); err != nil {
		return foundation.LimitUpHistory{}, err
	}
	if s.cap.LimitUp == nil {
		return foundation.LimitUpHistory{}, s.unsupported("limit-up")
	}
	if provider, ok := s.cap.LimitUp.(contracts.ProgressiveLimitUpHistoryProvider); ok {
		history, err := provider.ProgressiveRecentLimitUpHistory(ctx, days, func(value foundation.LimitUpHistory) {
			if publish != nil && ctx.Err() == nil {
				publish(foundation.CloneLimitUpHistory(value))
			}
		})
		return foundation.CloneLimitUpHistory(history), err
	}
	// Prefer an ordinary coverage-aware request over losing successful empty days.
	if _, ok := s.cap.LimitUp.(contracts.LimitUpHistoryProvider); ok {
		history, err := s.RecentLimitUpHistory(ctx, days)
		if publish != nil && ctx.Err() == nil {
			publish(foundation.CloneLimitUpHistory(history))
		}
		return history, err
	}
	events, err := s.ProgressiveRecentLimitUps(ctx, days, func(items []foundation.LimitUpEvent) {
		if publish != nil && ctx.Err() == nil {
			publish(foundation.LimitUpHistoryFromEvents(items, nil))
		}
	})
	return foundation.LimitUpHistoryFromEvents(events, err), err
}

// ProgressiveRecentLimitUps explicitly preserves the optional supplier capability
// through the Access wrapper, including partial events and the final typed error.
func (s *Access) ProgressiveRecentLimitUps(ctx context.Context, days int, publish func([]foundation.LimitUpEvent)) ([]foundation.LimitUpEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.cap.LimitUp == nil {
		return nil, s.unsupported("limit-up")
	}
	if provider, ok := s.cap.LimitUp.(contracts.ProgressiveRecentLimitUpProvider); ok {
		events, err := provider.ProgressiveRecentLimitUps(ctx, days, func(events []foundation.LimitUpEvent) {
			if publish != nil && ctx.Err() == nil {
				publish(cloneLimitUpEvents(events))
			}
		})
		return cloneLimitUpEvents(events), err
	}
	events, err := s.cap.LimitUp.RecentLimitUps(ctx, days)
	if publish != nil && ctx.Err() == nil {
		publish(cloneLimitUpEvents(events))
	}
	return cloneLimitUpEvents(events), err
}

func (s *Access) BrokenLimitUpPool(ctx context.Context, date time.Time) ([]foundation.MarketLimitEvent, error) {
	if s.cap.Pools == nil {
		return nil, s.unsupported("broken-limit-up")
	}
	return s.cap.Pools.BrokenLimitUpPool(ctx, date)
}
func (s *Access) LimitDownPool(ctx context.Context, date time.Time) ([]foundation.MarketLimitEvent, error) {
	if s.cap.Pools == nil {
		return nil, s.unsupported("limit-down")
	}
	return s.cap.Pools.LimitDownPool(ctx, date)
}
func (s *Access) Boards(ctx context.Context, keyword string, limit int) ([]foundation.Board, error) {
	if s.cap.Boards == nil {
		return nil, s.unsupported("boards")
	}
	return s.cap.Boards.Boards(ctx, keyword, limit)
}
func (s *Access) BoardStocks(ctx context.Context, code string, limit int) ([]foundation.BoardStock, error) {
	if s.cap.Boards == nil {
		return nil, s.unsupported("board-stocks")
	}
	return s.cap.Boards.BoardStocks(ctx, code, limit)
}
