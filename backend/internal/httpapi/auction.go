package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

var errDetailAuctionNotCurrent = errors.New("auction source has no current-day points")

func (s *Server) auctionTrace(w http.ResponseWriter, r *http.Request) {
	if s.auctionProvider == nil {
		writeError(w, http.StatusServiceUnavailable, "auction provider is unavailable")
		return
	}
	symbol, err := foundation.NormalizeSymbol(strings.TrimSpace(r.URL.Query().Get("symbol")))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if r.URL.Query().Get("detail") == "1" {
		trace, stale, err := s.detailAuctions.load(r.Context(), detailPollKey(symbol.Canonical, "auction", time.Now()), func(ctx context.Context) (foundation.AuctionTrace, error) {
			value, loadErr := s.auctionProvider.AuctionTrace(ctx, symbol.Canonical)
			if loadErr != nil && shouldObserveFailure(ctx) {
				s.sourceHealth.failure(s.auctionSourceID, loadErr)
			}
			if loadErr == nil {
				s.sourceHealth.success(value.Meta)
				if value.Symbol != symbol.Canonical || len(value.Points) == 0 || value.TradeDate != time.Now().In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02") {
					loadErr = errDetailAuctionNotCurrent
				}
			}
			return value, loadErr
		})
		if err != nil {
			writeError(w, http.StatusBadGateway, "竞价上游行情暂时无响应")
			return
		}
		if stale {
			trace.Meta.Stale = true
			trace.Meta.FallbackReason = "本机暂用最近一次可用竞价快照，请以源抓取时间为准"
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": trace, "status": "ready"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	trace, err := s.auctionProvider.AuctionTrace(ctx, symbol.Canonical)
	if err != nil {
		if shouldObserveFailure(ctx) {
			s.sourceHealth.failure(s.auctionSourceID, err)
		}
		writeError(w, http.StatusBadGateway, "竞价上游行情暂时无响应")
		return
	}
	if trace.Symbol != symbol.Canonical || len(trace.Points) == 0 || trace.TradeDate == "" {
		writeError(w, http.StatusBadGateway, "竞价来源数据不完整")
		return
	}
	// Never treat an old trading day as the current auction. Show an empty
	// state instead of inserting previous-session prices into today's chart.
	china := time.FixedZone("CST", 8*60*60)
	if trace.TradeDate != time.Now().In(china).Format("2006-01-02") {
		writeJSON(w, http.StatusOK, map[string]any{"data": foundation.AuctionTrace{Symbol: symbol.Canonical, TradeDate: trace.TradeDate, Points: []foundation.AuctionPoint{}, Meta: trace.Meta}, "status": "historical"})
		return
	}
	s.sourceHealth.success(trace.Meta)
	writeJSON(w, http.StatusOK, map[string]any{"data": trace, "status": "ready"})
}
