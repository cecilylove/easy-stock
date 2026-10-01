package httpapi

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

const stockIntradaySampleLimit = 1000

var stockIntradayZone = time.FixedZone("Asia/Shanghai", 8*60*60)

type stockIntradayData struct {
	Symbol         string                `json:"symbol"`
	TradeDate      string                `json:"trade_date"`
	Lines          []foundation.KLine    `json:"lines"`
	AvailableDates []string              `json:"available_dates"`
	Availability   string                `json:"availability"`
	Meta           foundation.SourceMeta `json:"meta"`
	PreviousClose  *float64              `json:"previous_close,omitempty"`
	Message        string                `json:"message,omitempty"`
}

func emptyStockIntraday(symbol, date, message string) stockIntradayData {
	return stockIntradayData{Symbol: symbol, TradeDate: date, Lines: []foundation.KLine{}, AvailableDates: []string{}, Availability: "unavailable", Message: message,
		Meta: foundation.SourceMeta{Capability: "stock-intraday:recent-sample", Period: "1", FieldsKnown: true}}
}

func (s *Server) intraday(w http.ResponseWriter, r *http.Request) {
	symbol, err := foundation.NormalizeSymbol(strings.TrimSpace(r.URL.Query().Get("symbol")))
	if err != nil {
		writeError(w, http.StatusBadRequest, "symbol is required and must be a valid stock symbol")
		return
	}
	date := strings.TrimSpace(r.URL.Query().Get("date"))
	day, err := time.ParseInLocation("2006-01-02", date, stockIntradayZone)
	if err != nil || day.Format("2006-01-02") != date || day.After(time.Now().In(stockIntradayZone)) {
		writeError(w, http.StatusBadRequest, "date must be YYYY-MM-DD and cannot be in the future")
		return
	}
	if r.URL.Query().Get("adjust") != "" || r.URL.Query().Get("provider") != "" {
		writeError(w, http.StatusBadRequest, "historical intraday uses the actual minute source; provider/adjust is not supported")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	stopClose := context.AfterFunc(s.intradayContext, cancel)
	defer stopClose()
	if ctx.Err() != nil {
		return
	}
	data, stale, err := s.stockIntraday.load(ctx, symbol.Canonical+":"+date, func(fetchCtx context.Context) (stockIntradayData, error) {
		if err := fetchCtx.Err(); err != nil {
			return stockIntradayData{}, err
		}
		var history stockIntradayData
		historyFetched := false
		var historyFailure error
		now := time.Now().In(stockIntradayZone)
		historyAttempted := s.historyIntradayProvider != nil && stockIntradayHistoryReady(date, now)
		// Monthly archives have a fixed full-day grid; never interpret today's
		// future slots as actual trades before the session closes.
		if historyAttempted {
			historyAttemptAt := time.Now()
			historyCtx, cancel := context.WithTimeout(fetchCtx, 8*time.Second)
			archive, historyErr := s.historyIntradayProvider.HistoryIntraday(historyCtx, symbol.Canonical, date)
			if historyErr == nil {
				historyErr = historyCtx.Err()
			}
			cancel()
			if historyErr == nil {
				history, historyErr = stockIntradayFromHistory(archive, symbol.Canonical, date)
			}
			if historyErr == nil {
				historyFetched = true
				// Provider month-cache hits preserve the real archive fetch time.
				s.sourceHealth.observe(foundation.SourceObservation{Meta: archive.Meta, SourceID: sourceID(archive.Meta.Source), Capability: "stock-intraday:history", AttemptAt: archive.Meta.FetchedAt})
				if len(history.Lines) > 0 {
					return history, nil
				}
			}
			historyFailure = historyErr
			if historyErr != nil && fetchCtx.Err() == nil && !errors.Is(historyErr, context.Canceled) && archive.Meta.Source != "" {
				s.sourceHealth.observe(foundation.SourceObservation{SourceID: sourceID(archive.Meta.Source), Capability: priceFailureCapability("stock-intraday:history", symbol.Canonical, historyErr), AttemptAt: historyAttemptAt, Failed: true})
			}
			if err := fetchCtx.Err(); err != nil {
				return stockIntradayData{}, err
			}
		}
		attemptAt := time.Now()
		lines, loadErr := s.intradayProvider.KLine(fetchCtx, symbol.Canonical, "1", stockIntradaySampleLimit)
		if loadErr == nil {
			loadErr = fetchCtx.Err()
		}
		if loadErr == nil {
			lines, loadErr = normalizeKLineContract(lines, symbol.Canonical, "1", "source", "")
		}
		if loadErr == nil {
			// A minute transaction series cannot use adjusted negative/zero prices,
			// even though the shared daily K contract legitimately permits them.
			for _, line := range lines {
				if line.Open <= 0 || line.High <= 0 || line.Low <= 0 || line.Close <= 0 {
					loadErr = fmt.Errorf("%w: nonpositive minute transaction price", foundation.ErrInvalidPriceData)
					break
				}
			}
		}
		if loadErr != nil {
			if shouldObserveFailure(fetchCtx) && !errors.Is(loadErr, context.Canceled) && s.intradaySourceID != "" {
				s.sourceHealth.observe(foundation.SourceObservation{SourceID: s.intradaySourceID, Capability: priceFailureCapability("stock-intraday:recent-sample", symbol.Canonical, loadErr), AttemptAt: attemptAt, Failed: true})
			}
			if historyFetched {
				return history, nil
			}
			return stockIntradayData{}, loadErr
		}
		// Only the actual fetch renews observations; cache reads do not.
		meta := lines[len(lines)-1].Meta
		meta.Capability = "stock-intraday:recent-sample"
		s.sourceHealth.observe(foundation.SourceObservation{Meta: meta, SourceID: sourceID(meta.Source), Capability: meta.Capability, AttemptAt: attemptAt})
		value := selectStockIntraday(lines, symbol.Canonical, date, time.Now())
		// A successful recent fetch for another day cannot turn an archive
		// refresh failure into a successful empty value: returning an error
		// preserves this exact symbol/date's previously cached archive as stale.
		if len(value.Lines) == 0 && historyFailure != nil {
			return stockIntradayData{}, historyFailure
		}
		if historyAttempted {
			if len(value.Lines) == 0 && historyFetched {
				return history, nil
			}
			value.Meta.FallbackReason = "指定交易日历史档案暂不可用，回退最近分钟样本；未拼接不同接口数据"
			for i := range value.Lines {
				value.Lines[i].Meta.FallbackReason = value.Meta.FallbackReason
			}
		}
		return value, nil
	})
	if err != nil {
		data := emptyStockIntraday(symbol.Canonical, date, "历史分时上游暂不可用，请稍后重试")
		data.Meta.Source = s.intradaySourceID
		writeJSON(w, http.StatusBadGateway, map[string]any{"data": data, "error": data.Message})
		return
	}
	if stale {
		data.Meta.Stale = true
		data.Message = "上游暂不可用，展示指定交易日最近一次可用样本"
		data.Lines = cloneDetailKLinesAsStale(data.Lines)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data})
}

func stockIntradayFromHistory(history foundation.StockIntradayHistory, symbol, date string) (result stockIntradayData, validationErr error) {
	defer func() {
		if validationErr != nil {
			validationErr = fmt.Errorf("%w: %v", foundation.ErrInvalidPriceData, validationErr)
		}
	}()
	data := emptyStockIntraday(symbol, date, "历史档案未提供指定交易日数据；未替换为其他日期")
	if history.Symbol != symbol || history.TradeDate != date {
		return data, fmt.Errorf("historical intraday identity mismatch")
	}
	data.AvailableDates = append([]string{}, history.AvailableDates...)
	data.Meta = history.Meta
	data.Meta.TradeDate = date
	data.Meta.Capability = "stock-intraday:history"
	if history.Meta.Stale || history.Meta.Period != "1" || history.Meta.EffectiveAdjustment != "none" || history.Meta.BasisID == "" || history.Meta.Source == "" {
		return data, fmt.Errorf("invalid historical archive raw basis")
	}
	if len(history.Lines) == 0 {
		return data, nil
	}
	if history.PreviousClose <= 0 || math.IsNaN(history.PreviousClose) || math.IsInf(history.PreviousClose, 0) {
		return data, fmt.Errorf("invalid historical previous close")
	}
	data.Lines = append([]foundation.KLine(nil), history.Lines...)
	minutes := map[string]bool{}
	for i := range data.Lines {
		line := &data.Lines[i]
		stamp := line.Time.In(stockIntradayZone)
		minute := stamp.Format("15:04")
		if line.Symbol != symbol || stamp.Format("2006-01-02") != date || stamp.Second() != 0 || stamp.Nanosecond() != 0 || minutes[minute] || line.Meta.Source != history.Meta.Source || line.Meta.BasisID != history.Meta.BasisID || line.Meta.Period != "1" || line.Meta.EffectiveAdjustment != "none" {
			return data, fmt.Errorf("invalid historical minute date or identity")
		}
		minutes[minute] = true
		if line.Meta.Stale || !line.Meta.FieldsKnown || !foundation.FieldAvailable(line.Meta, "close") || !foundation.FieldAvailable(line.Meta, "average_price") || !foundation.FieldAvailable(line.Meta, "volume") || !foundation.FieldAvailable(line.Meta, "previous_close") || line.PreviousClose != history.PreviousClose {
			return data, fmt.Errorf("historical native fields not present")
		}
		for _, value := range []float64{line.Close, line.AveragePrice, line.Volume} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return data, fmt.Errorf("nonfinite historical minute field")
			}
		}
		if line.Close <= 0 || line.AveragePrice <= 0 || line.Volume < 0 {
			return data, fmt.Errorf("invalid historical minute field")
		}
		line.Meta.Capability = data.Meta.Capability
	}
	for _, session := range [][2]int{{570, 690}, {781, 900}} {
		for m := session[0]; m <= session[1]; m++ {
			if !minutes[fmt.Sprintf("%02d:%02d", m/60, m%60)] {
				return data, fmt.Errorf("incomplete historical archive grid")
			}
		}
	}
	if len(minutes) != 241 && !(len(minutes) == 242 && minutes["13:00"]) {
		return data, fmt.Errorf("unexpected historical archive grid")
	}
	sort.Slice(data.Lines, func(i, j int) bool { return data.Lines[i].Time.Before(data.Lines[j].Time) })
	data.PreviousClose = &history.PreviousClose
	data.Availability, data.Message = "available", ""
	data.Meta.Partial = false
	return data, nil
}

func stockIntradayHistoryReady(date string, now time.Time) bool {
	now = now.In(stockIntradayZone)
	return date < now.Format("2006-01-02") || date == now.Format("2006-01-02") && now.Hour() >= 15
}

func selectStockIntraday(lines []foundation.KLine, symbol, date string, now time.Time) stockIntradayData {
	data := emptyStockIntraday(symbol, date, "指定交易日不在最近分钟样本内，或该日没有行情；未替换为其他日期")
	dates := map[string]bool{}
	for _, line := range lines {
		day := line.Time.In(stockIntradayZone).Format("2006-01-02")
		dates[day] = true
		if day == date {
			data.Lines = append(data.Lines, line)
		}
	}
	for day := range dates {
		data.AvailableDates = append(data.AvailableDates, day)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(data.AvailableDates)))
	if len(lines) > 0 {
		data.Meta = lines[len(lines)-1].Meta
		data.Meta.TradeDate = date
		data.Meta.Capability = "stock-intraday:recent-sample"
	}
	if len(data.Lines) == 0 {
		data.Meta.Partial = false
		return data
	}
	data.Meta = data.Lines[len(data.Lines)-1].Meta
	data.Meta.TradeDate, data.Meta.Capability = date, "stock-intraday:recent-sample"
	data.Availability, data.Message = "partial", "仅提供最近分钟样本，指定交易日尚未覆盖完整交易时段"
	if stockIntradayComplete(data.Lines) && (date < now.In(stockIntradayZone).Format("2006-01-02") || now.In(stockIntradayZone).Hour() >= 15) {
		data.Availability, data.Message = "available", ""
	}
	data.Meta.Partial = data.Availability == "partial"
	for i := range data.Lines {
		data.Lines[i].Meta.Partial = data.Meta.Partial
		data.Lines[i].Meta.TradeDate, data.Lines[i].Meta.Capability = date, data.Meta.Capability
	}
	// Current snapshots or the prior observed minute are not a prior-day close.
	// Only retain an explicit same-baseline field present on every selected bar.
	close := data.Lines[0].PreviousClose
	if close > 0 && !math.IsNaN(close) && !math.IsInf(close, 0) {
		for _, line := range data.Lines {
			if !line.Meta.FieldsKnown || !foundation.FieldAvailable(line.Meta, "previous_close") || line.PreviousClose != close {
				return data
			}
		}
		data.PreviousClose = &close
	}
	return data
}

// Sina's observed minutes label completed bars: the closing auction has one
// 15:00 bar, without separate 14:58/14:59 bars. Require every expected minute
// instead of inferring a complete day from its first/last timestamp.
func stockIntradayComplete(lines []foundation.KLine) bool {
	minutes := map[string]bool{}
	for _, line := range lines {
		stamp := line.Time.In(stockIntradayZone)
		if stamp.Second() != 0 || stamp.Nanosecond() != 0 {
			return false
		}
		minutes[stamp.Format("15:04")] = true
	}
	for _, session := range [][2]int{{9*60 + 31, 11*60 + 30}, {13*60 + 1, 14*60 + 57}, {15 * 60, 15 * 60}} {
		for minute := session[0]; minute <= session[1]; minute++ {
			if !minutes[fmt.Sprintf("%02d:%02d", minute/60, minute%60)] {
				return false
			}
		}
	}
	return true
}

func (s *Server) closeStockIntraday() {
	if s.intradayCancel != nil {
		s.intradayCancel()
	}
	if closer, ok := s.historyIntradayProvider.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
	if s.stockIntraday == nil {
		return
	}
	s.stockIntraday.mu.Lock()
	defer s.stockIntraday.mu.Unlock()
	for _, entry := range s.stockIntraday.entries {
		if entry.flight != nil {
			entry.flight.abandoned = true
			entry.flight.cancel()
		}
	}
}
