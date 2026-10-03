package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/marketemotion"
)

func TestLadderHistoryCoveredEmptyDaysAreVisibleAndAdjacent(t *testing.T) {
	now := time.Date(2026, 8, 7, 16, 0, 0, 0, shanghaiLocation)
	history := foundation.LimitUpHistory{RequestedDates: []string{"2026-08-05", "2026-08-06", "2026-08-07"}, CoveredDates: []string{"2026-08-05", "2026-08-06", "2026-08-07"}, Events: []foundation.LimitUpEvent{{Symbol: "600001.SH", Date: now.AddDate(0, 0, -2), Streak: 1}}, Meta: foundation.SourceMeta{Source: "history"}}
	value, err := buildLimitUpLadderHistory(history, nil, now)
	if err != nil || value.Current.TradeDate != "2026-08-07" || value.Previous.TradeDate != "2026-08-06" || value.Current.LimitUpCount != 0 || value.Previous.LimitUpCount != 0 || !value.ComparisonReady {
		t.Fatalf("empty dates skipped: %+v %v", value, err)
	}
	history.CoveredDates = []string{"2026-08-05", "2026-08-07"}
	history.MissingDates = []string{"2026-08-06"}
	value, err = buildLimitUpLadderHistory(history, nil, now)
	if err != nil || value.Previous.TradeDate != "" || value.ComparisonReady || len(value.Advance) != 0 || !value.Meta.Partial {
		t.Fatalf("nonadjacent comparison: %+v %v", value, err)
	}
	history.Events = nil
	history.CoveredDates = []string{"2026-08-06", "2026-08-07"}
	history.MissingDates = nil
	value, err = buildLimitUpLadderHistory(history, nil, now)
	if err != nil || !value.ComparisonReady || value.Current.LimitUpCount != 0 {
		t.Fatalf("all-empty result unavailable: %+v %v", value, err)
	}
	if _, err = buildLimitUpLadderHistory(foundation.LimitUpHistory{}, nil, now); err == nil {
		t.Fatal("absent data forged empty day")
	}
}
func TestLadderHistoryLegacyNeverSkipsMissingAdjacentDay(t *testing.T) {
	now := time.Date(2026, 8, 7, 16, 0, 0, 0, shanghaiLocation)
	value, err := buildLimitUpLadder([]foundation.LimitUpEvent{{Date: now, Symbol: "600001.SH"}, {Date: now.AddDate(0, 0, -2), Symbol: "600001.SH"}}, nil, now)
	if err != nil || value.ComparisonReady || value.Previous.TradeDate != "" {
		t.Fatalf("legacy skipped missing day: %+v %v", value, err)
	}
	// National-day closure: the adjacent trading day before October 8 is September 30.
	if date := previousLimitUpTradingDate("2026-10-08"); date != "2026-09-30" {
		t.Fatalf("holiday adjacency=%s", date)
	}
}

type httpEmptyHistoryProvider struct {
	value                     foundation.LimitUpHistory
	err                       error
	historyCalls, legacyCalls int
}

func (p *httpEmptyHistoryProvider) RecentLimitUps(context.Context, int) ([]foundation.LimitUpEvent, error) {
	p.legacyCalls++
	return p.value.Events, p.err
}
func (p *httpEmptyHistoryProvider) RecentLimitUpHistory(context.Context, int) (foundation.LimitUpHistory, error) {
	p.historyCalls++
	return foundation.CloneLimitUpHistory(p.value), p.err
}
func (p *httpEmptyHistoryProvider) ProgressiveLimitUpHistory(_ context.Context, _ int, publish func(foundation.LimitUpHistory, string, error)) (foundation.LimitUpHistory, error) {
	p.historyCalls++
	for _, stage := range []string{"primary", "history", "themes"} {
		publish(p.value, stage, p.err)
	}
	return p.value, p.err
}

func TestLadderOrdinaryAndProgressiveEmptyCoverageEndToEnd(t *testing.T) {
	dates := foundation.LimitUpRequestedDates(time.Now(), 8)
	if len(dates) < 2 {
		t.Fatal("need two trading dates")
	}
	p := &httpEmptyHistoryProvider{value: foundation.LimitUpHistory{RequestedDates: dates, CoveredDates: dates, Meta: foundation.SourceMeta{Source: "empty-test"}}}
	cache := newLimitUpLadderCache(time.Second)
	value, err := cache.load(context.Background(), p, nil, nil)
	if err != nil || value.Current.TradeDate != dates[len(dates)-1] || value.Previous.TradeDate != dates[len(dates)-2] || !value.ComparisonReady || value.Current.LimitUpCount != 0 || p.historyCalls != 1 || p.legacyCalls != 0 {
		t.Fatalf("ordinary empty failed/refetched: %+v %v %+v", value, err, p)
	}
	s := &Server{limitUpProvider: p, limitUpProgress: &shortTermCache[limitUpLadderData]{}, logger: log.New(io.Discard, "", 0)}
	// Use the real progressive consumer with no upstream concepts/quotes.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var last shortTermProgress[limitUpLadderData]
	s.refreshLimitUpProgress(ctx, func(v shortTermProgress[limitUpLadderData]) { last = v })
	if ctx.Err() != nil || last.Data == nil || last.Data.Current.TradeDate != dates[len(dates)-1] || last.Data.Current.LimitUpCount != 0 || !last.Data.ComparisonReady || p.historyCalls != 2 || p.legacyCalls != 0 {
		t.Fatalf("progressive all-empty lost/refetched: %+v provider=%+v ctx=%v", last, p, ctx.Err())
	}
}

func TestLadderHTTPEmptyDayResponseAndNoDataNotForged(t *testing.T) {
	dates := foundation.LimitUpRequestedDates(time.Now(), 8)
	p := &httpEmptyHistoryProvider{value: foundation.LimitUpHistory{RequestedDates: dates, CoveredDates: dates}}
	s := &Server{limitUpProvider: p, limitUpSnapshots: newLimitUpLadderCache(time.Second)}
	response := httptest.NewRecorder()
	s.limitUpLadderHandler(response, httptest.NewRequest(http.MethodGet, "/api/v1/short-term/limit-up-ladder", nil))
	var payload struct {
		Data limitUpLadderData `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || payload.Data.Current.LimitUpCount != 0 || payload.Data.Current.TradeDate != dates[len(dates)-1] || len(payload.Data.Meta.CoveredDates) != len(dates) {
		t.Fatalf("empty HTTP response: %d %s", response.Code, response.Body.String())
	}
	p.value = foundation.LimitUpHistory{}
	s.limitUpSnapshots = newLimitUpLadderCache(time.Second)
	response = httptest.NewRecorder()
	s.limitUpLadderHandler(response, httptest.NewRequest(http.MethodGet, "/api/v1/short-term/limit-up-ladder", nil))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("missing data forged empty success: %d %s", response.Code, response.Body.String())
	}
}

func TestLadderOrdinaryPartialOtherDayKeepsExactPair(t *testing.T) {
	dates := foundation.LimitUpRequestedDates(time.Now(), 8)
	if len(dates) < 3 {
		t.Fatal("need three dates")
	}
	missing := dates[0]
	p := &httpEmptyHistoryProvider{value: foundation.LimitUpHistory{RequestedDates: dates, CoveredDates: dates[1:], MissingDates: []string{missing}, Meta: foundation.SourceMeta{Partial: true}}, err: &foundation.LimitUpCoverageError{RequestedDates: dates, CoveredDates: dates[1:], MissingDates: []string{missing}, Cause: errors.New("older date failed")}}
	value, err := newLimitUpLadderCache(time.Second).load(context.Background(), p, nil, nil)
	if err != nil || !value.ComparisonReady || !value.Meta.Partial {
		t.Fatalf("covered exact pair incorrectly discarded: %+v %v", value, err)
	}
}

func TestLadderPartialSupplierTimeoutKeepsCoveredAdjacentDays(t *testing.T) {
	dates := foundation.LimitUpRequestedDates(time.Now(), 8)
	p := &httpEmptyHistoryProvider{value: foundation.LimitUpHistory{RequestedDates: dates, CoveredDates: dates[1:], MissingDates: dates[:1], Meta: foundation.SourceMeta{Partial: true}}, err: &foundation.LimitUpCoverageError{RequestedDates: dates, CoveredDates: dates[1:], MissingDates: dates[:1], Cause: &contracts.Error{Kind: contracts.TimedOut, Cause: context.DeadlineExceeded}}}
	value, err := newLimitUpLadderCache(time.Second).load(context.Background(), p, nil, nil)
	if err != nil || !value.ComparisonReady || !value.Meta.Partial {
		t.Fatalf("supplier timeout treated as caller deadline %+v %v", value, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = newLimitUpLadderCache(time.Second).load(ctx, p, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("parent cancellation ignored")
	}
}

func TestMarketEmotionHistoryPersistsCoveredEmptyDay(t *testing.T) {
	store, err := marketemotion.OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	p := &httpEmptyHistoryProvider{value: foundation.LimitUpHistory{RequestedDates: []string{"2026-08-05", "2026-08-06", "2026-08-07"}, CoveredDates: []string{"2026-08-05", "2026-08-06", "2026-08-07"}}}
	engine := newMarketEmotionEngine(store, p, &countingMarketPoolProvider{}, &countingEmotionKLineProvider{}, nil, nil)
	engine.now = func() time.Time { return time.Date(2026, 8, 7, 20, 0, 0, 0, shanghaiLocation) }
	value, err := engine.load(context.Background())
	if err != nil || len(value.Points) != 3 || value.Latest.TradeDate != "2026-08-07" || p.historyCalls != 1 || p.legacyCalls != 0 {
		t.Fatalf("emotion skipped empty days: %+v %v %+v", value, err, p)
	}
}
