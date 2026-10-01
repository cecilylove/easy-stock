package sina

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-stock/backend/internal/foundation"
)

const maxIntradayArchiveBytes = 512 * 1024

var errIntradayArchiveCapacity = errors.New("archive request capacity reached")

type intradayArchive struct {
	days map[string]historicalMinuteDay
	meta foundation.SourceMeta
}

type intradayMonthEntry struct {
	archive               intradayArchive
	hasValue              bool
	expiresAt, accessedAt time.Time
	flight                *intradayMonthFlight
}

type intradayMonthFlight struct {
	done      chan struct{}
	cancel    context.CancelFunc
	waiters   int
	abandoned bool
	archive   intradayArchive
	err       error
}

type HistoryIntradayClient struct {
	client *Client
	mu     sync.Mutex
	months map[string]*intradayMonthEntry
	now    func() time.Time
	closed bool
}

func NewHistoryIntradayClient(client *Client) *HistoryIntradayClient {
	if client == nil {
		client = NewClient()
	}
	return &HistoryIntradayClient{client: client, months: map[string]*intradayMonthEntry{}, now: time.Now}
}

func (c *HistoryIntradayClient) HistoryIntraday(ctx context.Context, symbol, date string) (foundation.StockIntradayHistory, error) {
	result := foundation.StockIntradayHistory{Symbol: symbol, TradeDate: date, Lines: []foundation.KLine{}, AvailableDates: []string{}}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	normalized, err := foundation.NormalizeSymbol(symbol)
	day, dateErr := time.Parse("2006-01-02", date)
	if err != nil || dateErr != nil || date != day.Format("2006-01-02") || len(normalized.RawCode) != 6 || (normalized.Market != "SH" && normalized.Market != "SZ" && normalized.Market != "BJ") {
		return result, fmt.Errorf("unsupported historical intraday symbol or date")
	}
	result.Symbol = normalized.Canonical
	month := day.Format("2006/01")
	result.Meta = foundation.SourceMeta{Source: "sina:historical-intraday", Provider: "sina", Capability: "stock-intraday:history"}
	archive, err := c.loadMonth(ctx, normalized.Sina, month)
	if err != nil {
		if errors.Is(err, errIntradayArchiveCapacity) {
			result.Meta = foundation.SourceMeta{}
		}
		return result, err
	}
	result.Meta = archive.meta
	result.Meta.AvailableFields = append([]string(nil), archive.meta.AvailableFields...)
	result.Meta.InstrumentID = normalized.Canonical
	result.Meta.TradeDate = date
	for actualDate := range archive.days {
		result.AvailableDates = append(result.AvailableDates, actualDate)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(result.AvailableDates)))
	decoded, ok := archive.days[date]
	if !ok {
		return result, nil
	}
	result.PreviousClose = decoded.PreviousClose
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	for _, point := range decoded.Points {
		stamp, parseErr := time.ParseInLocation("2006-01-02 15:04", decoded.Date+" "+point.Time, zone)
		if parseErr != nil {
			return result, fmt.Errorf("%w: invalid archive minute timestamp", foundation.ErrInvalidPriceData)
		}
		meta := result.Meta
		meta.AvailableFields = append([]string(nil), meta.AvailableFields...)
		result.Lines = append(result.Lines, foundation.KLine{Symbol: normalized.Canonical, Time: stamp, Close: point.Price, AveragePrice: point.AveragePrice, Volume: point.Volume, PreviousClose: decoded.PreviousClose, Meta: meta})
	}
	return result, nil
}

func (c *HistoryIntradayClient) loadMonth(ctx context.Context, native, month string) (intradayArchive, error) {
	key := native + ":" + month
	for {
		if err := ctx.Err(); err != nil {
			return intradayArchive{}, err
		}
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return intradayArchive{}, context.Canceled
		}
		now := c.now()
		entry := c.months[key]
		if entry == nil {
			if len(c.months) >= 32 {
				oldKey := ""
				var oldest time.Time
				for candidate, item := range c.months {
					if item.flight == nil && (oldKey == "" || item.accessedAt.Before(oldest)) {
						oldKey, oldest = candidate, item.accessedAt
					}
				}
				if oldKey == "" {
					c.mu.Unlock()
					return intradayArchive{}, errIntradayArchiveCapacity
				}
				delete(c.months, oldKey)
			}
			entry = &intradayMonthEntry{}
			c.months[key] = entry
		}
		entry.accessedAt = now
		if entry.hasValue && now.Before(entry.expiresAt) {
			archive := entry.archive
			c.mu.Unlock()
			return archive, nil
		}
		flight := entry.flight
		if flight != nil && flight.abandoned {
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return intradayArchive{}, ctx.Err()
			case <-flight.done:
				continue
			}
		}
		if flight == nil {
			fetchCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			flight = &intradayMonthFlight{done: make(chan struct{}), cancel: cancel}
			entry.flight = flight
			go func(flight *intradayMonthFlight) {
				defer cancel()
				archive, err := c.fetchMonth(fetchCtx, native, month)
				if err == nil {
					err = fetchCtx.Err()
				}
				c.mu.Lock()
				flight.archive, flight.err = archive, err
				if !flight.abandoned && err == nil && !c.closed {
					entry.archive, entry.hasValue = archive, true
					entry.expiresAt = c.now().Add(5 * time.Minute)
				}
				entry.flight = nil
				close(flight.done)
				c.mu.Unlock()
			}(flight)
		}
		flight.waiters++
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			c.mu.Lock()
			flight.waiters--
			if flight.waiters == 0 {
				flight.abandoned = true
				flight.cancel()
			}
			c.mu.Unlock()
			return intradayArchive{}, ctx.Err()
		case <-flight.done:
			if err := ctx.Err(); err != nil {
				return intradayArchive{}, err
			}
			return flight.archive, flight.err
		}
	}
}

func (c *HistoryIntradayClient) fetchMonth(ctx context.Context, native, month string) (intradayArchive, error) {
	requestURL := c.client.intradayHistoryBaseURL + "/" + native + "/hisdata/" + month + ".js"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return intradayArchive{}, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn/")
	req.Header.Set("User-Agent", "Mozilla/5.0 easy-stock/0.1")
	started := c.now()
	resp, err := c.client.httpClient.Do(req)
	if err != nil {
		return intradayArchive{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return intradayArchive{}, &foundation.PriceHTTPStatusError{Provider: "sina", StatusCode: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxIntradayArchiveBytes+1))
	if err != nil {
		return intradayArchive{}, err
	}
	if len(body) > maxIntradayArchiveBytes {
		return intradayArchive{}, fmt.Errorf("%w: archive exceeds size limit", foundation.ErrInvalidPriceData)
	}
	archive, err := parseIntradayArchive(body, native, month)
	if err != nil {
		return intradayArchive{}, fmt.Errorf("%w: %v", foundation.ErrInvalidPriceData, err)
	}
	archive.meta = foundation.SourceMeta{Source: "sina:historical-intraday", Provider: "sina", SourceURL: requestURL, NativeCode: native, Period: "1", RequestedAdjustment: "source", EffectiveAdjustment: "none", AdjustmentConvention: "sina:historical-transaction-points", BasisID: "sina:historical-intraday:none", TimeZone: "Asia/Shanghai", VolumeUnit: "shares", AmountCurrency: "CNY", FieldsKnown: true, AvailableFields: []string{"close", "average_price", "volume", "previous_close"}, Capability: "stock-intraday:history", FetchedAt: c.now(), LatencyMS: c.now().Sub(started).Milliseconds()}
	return archive, nil
}

// Accept only the expected variable's JSON string; never execute a JS response.
func parseIntradayArchive(body []byte, native, month string) (intradayArchive, error) {
	parts := strings.Split(month, "/")
	if len(parts) != 2 {
		return intradayArchive{}, fmt.Errorf("invalid archive month")
	}
	variable := "MLC_" + native + "_" + parts[0] + "_" + parts[1]
	pattern := regexp.MustCompile(`(?s)^\s*var\s+` + regexp.QuoteMeta(variable) + `\s*=\s*("(?:[^"\\]|\\.)*")\s*;?(.*)$`)
	match := pattern.FindSubmatch(body)
	if len(match) != 3 {
		return intradayArchive{}, fmt.Errorf("unexpected historical archive assignment")
	}
	// Real archives end with a checksum block comment. Permit only comments
	// and whitespace after the JSON string, never another JS statement.
	tail := strings.TrimSpace(string(match[2]))
	for tail != "" {
		if !strings.HasPrefix(tail, "/*") {
			return intradayArchive{}, fmt.Errorf("unexpected historical archive suffix")
		}
		end := strings.Index(tail[2:], "*/")
		if end < 0 {
			return intradayArchive{}, fmt.Errorf("unterminated historical archive comment")
		}
		tail = strings.TrimSpace(tail[end+4:])
	}
	var encoded string
	if err := json.Unmarshal(match[1], &encoded); err != nil {
		return intradayArchive{}, fmt.Errorf("invalid historical archive string")
	}
	days := strings.Split(encoded, ",")
	if len(days) == 0 || len(days) > 31 {
		return intradayArchive{}, fmt.Errorf("invalid archive day count")
	}
	archive := intradayArchive{days: map[string]historicalMinuteDay{}}
	for _, segment := range days {
		decoded, err := decodeHistoricalMinute(segment)
		if err != nil {
			return intradayArchive{}, fmt.Errorf("invalid historical minute codec: %w", err)
		}
		if !strings.HasPrefix(decoded.Date, strings.ReplaceAll(month, "/", "-")+"-") {
			return intradayArchive{}, fmt.Errorf("archive contains wrong month")
		}
		if _, exists := archive.days[decoded.Date]; exists {
			return intradayArchive{}, fmt.Errorf("archive contains duplicate date")
		}
		archive.days[decoded.Date] = decoded
	}
	return archive, nil
}

func (c *HistoryIntradayClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for _, entry := range c.months {
		if entry.flight != nil {
			entry.flight.abandoned = true
			entry.flight.cancel()
		}
	}
	return nil
}
