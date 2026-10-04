package foundation

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type SourceMeta struct {
	Source          string   `json:"source"`
	SourceURL       string   `json:"source_url,omitempty"`
	AvailableFields []string `json:"available_fields,omitempty"`
	FieldsKnown     bool     `json:"fields_known,omitempty"`
	// Field provenance overrides apply only to hydrated fields; Source remains the original input.
	FieldSources         map[string]string    `json:"field_sources,omitempty"`
	FieldFetchedAt       map[string]time.Time `json:"field_fetched_at,omitempty"`
	Provider             string               `json:"provider,omitempty"`
	NativeCode           string               `json:"native_code,omitempty"`
	InstrumentID         string               `json:"instrument_id,omitempty"`
	Period               string               `json:"period,omitempty"`
	RequestedAdjustment  string               `json:"requested_adjustment,omitempty"`
	EffectiveAdjustment  string               `json:"effective_adjustment,omitempty"`
	AdjustmentConvention string               `json:"adjustment_convention,omitempty"`
	BasisID              string               `json:"basis_id,omitempty"`
	AsOf                 string               `json:"as_of,omitempty"`
	TimeZone             string               `json:"time_zone,omitempty"`
	NativeTimestamp      string               `json:"native_timestamp,omitempty"`
	VolumeUnit           string               `json:"volume_unit,omitempty"`
	AmountCurrency       string               `json:"amount_currency,omitempty"`
	Partial              bool                 `json:"partial,omitempty"`
	QueryCoverage        string               `json:"query_coverage,omitempty"` // complete, bounded, unsupported; separate from body status
	MissingIDs           []string             `json:"missing_ids,omitempty"`
	CoveredDates         []string             `json:"covered_dates,omitempty"`
	RequestedSort        string               `json:"requested_sort,omitempty"`
	EffectiveSort        string               `json:"effective_sort,omitempty"`
	MemberSet            *MemberSetMeta       `json:"member_set,omitempty"`
	Capability           string               `json:"capability,omitempty"`
	// ExecutionState is explicit for migrated adapters; empty keeps historical compatibility.
	ExecutionState string              `json:"execution_state,omitempty"` // fetched, cache, joined, skipped
	Observations   []SourceObservation `json:"-"`
	FetchedAt      time.Time           `json:"fetched_at"`
	LatencyMS      int64               `json:"latency_ms"`
	Stale          bool                `json:"stale"`
	TradeDate      string              `json:"trade_date,omitempty"`
	SnapshotID     string              `json:"snapshot_id,omitempty"`
	NextRefreshAt  *time.Time          `json:"next_refresh_at,omitempty"`
	FallbackReason string              `json:"fallback_reason,omitempty"`
	CarryForward   bool                `json:"carry_forward,omitempty"`
}

// BoardRef preserves native classification identity; codes are not portable across sources.
type BoardRef struct {
	Provider              string `json:"provider"`
	NativeCode            string `json:"native_code"`
	Dimension             string `json:"dimension"`
	Name                  string `json:"name"`
	ClassificationVersion string `json:"classification_version,omitempty"`
}

type MemberSetMeta struct {
	Kind     string   `json:"kind"`
	Complete bool     `json:"complete"`
	Total    int      `json:"total"`
	Returned int      `json:"returned"`
	HasMore  bool     `json:"has_more"`
	Scope    string   `json:"scope,omitempty"`
	Method   string   `json:"method,omitempty"`
	BoardRef BoardRef `json:"board_ref"`
}

// FieldAvailable treats explicit known-empty masks as no data, not all fields.
func FieldAvailable(meta SourceMeta, field string) bool {
	if !meta.FieldsKnown && len(meta.AvailableFields) == 0 {
		return true
	}
	for _, name := range meta.AvailableFields {
		if name == field {
			return true
		}
	}
	return false
}

type QuoteLevel struct {
	Price  float64 `json:"price"`
	Volume float64 `json:"volume"` // shares, never lots
}

type Quote struct {
	Symbol        string       `json:"symbol"`
	Name          string       `json:"name"`
	Price         float64      `json:"price"`
	Open          float64      `json:"open"`
	PreviousClose float64      `json:"previous_close"`
	High          float64      `json:"high"`
	Low           float64      `json:"low"`
	Change        float64      `json:"change"`
	ChangePercent float64      `json:"change_percent"`
	TradeTime     time.Time    `json:"trade_time,omitempty"`
	Volume        *float64     `json:"volume,omitempty"`
	Amount        *float64     `json:"amount,omitempty"`
	Bids          []QuoteLevel `json:"bids,omitempty"`
	Asks          []QuoteLevel `json:"asks,omitempty"`
	Meta          SourceMeta   `json:"meta"`
}

// AuctionTrace is a separately sourced indicative pre-open price series.
// Pre-09:26 values are not transaction prices or executed minute bars.
type AuctionTrace struct {
	Symbol    string         `json:"symbol"`
	TradeDate string         `json:"trade_date"`
	Points    []AuctionPoint `json:"points"`
	Meta      SourceMeta     `json:"meta"`
}

type AuctionPoint struct {
	Time   time.Time `json:"time"`
	Price  float64   `json:"price"`
	Volume float64   `json:"volume,omitempty"`
	Amount float64   `json:"amount,omitempty"`
}

type KLine struct {
	Symbol        string     `json:"symbol"`
	Time          time.Time  `json:"time"`
	Open          float64    `json:"open"`
	High          float64    `json:"high"`
	Low           float64    `json:"low"`
	Close         float64    `json:"close"`
	AveragePrice  float64    `json:"average_price,omitempty"`
	PreviousClose float64    `json:"previous_close,omitempty"`
	Volume        float64    `json:"volume"`
	Amount        float64    `json:"amount"`
	TurnoverRate  float64    `json:"turnover_rate,omitempty"`
	ChangePercent float64    `json:"change_percent,omitempty"`
	Meta          SourceMeta `json:"meta"`
}

// StockIntradayHistory preserves the archive's own date, baseline and points.
type StockIntradayHistory struct {
	Symbol         string
	TradeDate      string
	Lines          []KLine
	AvailableDates []string
	PreviousClose  float64
	Meta           SourceMeta
}

type NewsItem struct {
	ID          string     `json:"id,omitempty"`
	Title       string     `json:"title"`
	Content     string     `json:"content,omitempty"`
	URL         string     `json:"url,omitempty"`
	PublishedAt time.Time  `json:"published_at,omitempty"`
	Tags        []string   `json:"tags,omitempty"`
	Meta        SourceMeta `json:"meta"`
}

type SourceHealth struct {
	ID           string                   `json:"id"`
	Name         string                   `json:"name"`
	Category     string                   `json:"category"`
	OK           bool                     `json:"ok"`
	Status       string                   `json:"status"`
	Message      string                   `json:"message,omitempty"`
	CheckedAt    *time.Time               `json:"checked_at,omitempty"`
	LastSuccess  *time.Time               `json:"last_success,omitempty"`
	LastFailure  *time.Time               `json:"last_failure,omitempty"`
	Capabilities []SourceCapabilityHealth `json:"capabilities,omitempty"`
}

type SourceCapabilityHealth struct {
	Capability  string     `json:"capability"`
	Status      string     `json:"status"`
	CheckedAt   time.Time  `json:"checked_at"`
	LastSuccess *time.Time `json:"last_success,omitempty"`
	LastFailure *time.Time `json:"last_failure,omitempty"`
	Message     string     `json:"message,omitempty"`
}

type Board struct {
	Code           string     `json:"code"`
	Name           string     `json:"name"`
	ChangePercent  float64    `json:"change_percent"`
	TotalMarketCap float64    `json:"total_market_cap"`
	FloatMarketCap float64    `json:"float_market_cap"`
	MainNetInflow  float64    `json:"main_net_inflow"`
	Meta           SourceMeta `json:"meta"`
}

type BoardStock struct {
	Symbol               string     `json:"symbol"`
	Name                 string     `json:"name"`
	Price                float64    `json:"price"`
	Change               float64    `json:"change"`
	ChangePercent        float64    `json:"change_percent"`
	FiveDayChangePercent float64    `json:"five_day_change_percent,omitempty"`
	Volume               float64    `json:"volume"`
	Amount               float64    `json:"amount"`
	TotalMarketCap       float64    `json:"total_market_cap"`
	FloatMarketCap       float64    `json:"float_market_cap"`
	MainNetInflow        float64    `json:"main_net_inflow"`
	LimitUpStreak        int        `json:"limit_up_streak,omitempty"`
	LimitUpDays          int        `json:"limit_up_days,omitempty"`
	LimitUpCount         int        `json:"limit_up_count,omitempty"`
	FirstLimitTime       string     `json:"first_limit_time,omitempty"`
	LastLimitTime        string     `json:"last_limit_time,omitempty"`
	FirstLimitDate       string     `json:"first_limit_date,omitempty"`
	LastLimitDate        string     `json:"last_limit_date,omitempty"`
	LimitRegime          string     `json:"limit_regime,omitempty"`
	RankScore            int        `json:"rank_score,omitempty"`
	RankRole             string     `json:"rank_role,omitempty"`
	Meta                 SourceMeta `json:"meta"`
}

// StockCatalogEntry is one A-share from EastMoney's stock-selection catalog.
// Industry and Concepts are membership evidence used to build thematic
// constituent pools without maintaining stock-code lists in local rules.
type StockCatalogEntry struct {
	BoardStock
	Industry string
	Concepts []string
}

type LimitUpEvent struct {
	Symbol          string               `json:"symbol"`
	Name            string               `json:"name"`
	Date            time.Time            `json:"date"`
	Price           float64              `json:"price"`
	ChangePercent   float64              `json:"change_percent"`
	Amount          float64              `json:"amount"`
	FloatMarketCap  float64              `json:"float_market_cap"`
	TurnoverRate    float64              `json:"turnover_rate"`
	Streak          int                  `json:"streak"`
	FirstLimitTime  string               `json:"first_limit_time"`
	LastLimitTime   string               `json:"last_limit_time"`
	OpenCount       int                  `json:"open_count"`
	Industry        string               `json:"industry"`
	Days            int                  `json:"days"`
	Count           int                  `json:"count"`
	Concepts        []string             `json:"concepts,omitempty"`
	PrimaryTheme    string               `json:"primary_theme,omitempty"`
	ThemeSource     string               `json:"theme_source,omitempty"`
	ThemeKind       ThemeAttributionKind `json:"theme_kind,omitempty"`
	PoolThemeKind   ThemeAttributionKind `json:"pool_theme_kind,omitempty"`
	ThemeRank       int                  `json:"theme_rank,omitempty"`
	ThemeLeaderRole string               `json:"theme_leader_role,omitempty"`
	StreakLabel     string               `json:"streak_label,omitempty"`
	BoardType       string               `json:"board_type,omitempty"`
	Meta            SourceMeta           `json:"meta"`
}

// LimitUpCoverageError accompanies useful history when trading days failed.
// CoveredDates includes successfully fetched empty pools, not just event dates.
// MissingDates includes failed or unattempted dates; cancellation is retained in Cause.
type LimitUpCoverageError struct {
	RequestedDates []string
	CoveredDates   []string
	MissingDates   []string
	Cause          error
}

func (e *LimitUpCoverageError) Error() string {
	return fmt.Sprintf("limit-up history incomplete; missing dates: %s; cause: %v", strings.Join(e.MissingDates, ","), e.Cause)
}
func (e *LimitUpCoverageError) Unwrap() error { return e.Cause }

// CloneSourceMeta isolates all reference-valued metadata from cache/consumer mutations.
func CloneSourceMeta(meta SourceMeta) SourceMeta {
	meta.AvailableFields = append([]string(nil), meta.AvailableFields...)
	meta.MissingIDs = append([]string(nil), meta.MissingIDs...)
	meta.CoveredDates = append([]string(nil), meta.CoveredDates...)
	if meta.FieldSources != nil {
		fields := make(map[string]string, len(meta.FieldSources))
		for key, value := range meta.FieldSources {
			fields[key] = value
		}
		meta.FieldSources = fields
	}
	if meta.FieldFetchedAt != nil {
		fields := make(map[string]time.Time, len(meta.FieldFetchedAt))
		for key, value := range meta.FieldFetchedAt {
			fields[key] = value
		}
		meta.FieldFetchedAt = fields
	}
	if meta.MemberSet != nil {
		value := *meta.MemberSet
		meta.MemberSet = &value
	}
	if meta.NextRefreshAt != nil {
		value := *meta.NextRefreshAt
		meta.NextRefreshAt = &value
	}
	if meta.Observations != nil {
		observations := make([]SourceObservation, len(meta.Observations))
		for i, value := range meta.Observations {
			observations[i] = value
			observations[i].Meta = CloneSourceMeta(value.Meta)
		}
		meta.Observations = observations
	}
	return meta
}

// LimitUpFieldAvailable applies strict presence for new collectors and the
// historical nonzero convention only for old fixtures/unmarked retained events.
func LimitUpFieldAvailable(event LimitUpEvent, field string) bool {
	var number float64
	switch field {
	case "price":
		number = event.Price
	case "change_percent":
		number = event.ChangePercent
	case "amount":
		number = event.Amount
	case "float_market_cap":
		number = event.FloatMarketCap
	case "turnover_rate":
		number = event.TurnoverRate
	case "streak":
		number = float64(event.Streak)
	case "open_count":
		number = float64(event.OpenCount)
	case "days":
		number = float64(event.Days)
	case "count":
		number = float64(event.Count)
	default:
		return FieldAvailable(event.Meta, field)
	}
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return false
	}
	if event.Meta.FieldsKnown || len(event.Meta.AvailableFields) > 0 {
		return FieldAvailable(event.Meta, field)
	}
	return number != 0
}

// StockThemeAttribution is an authoritative or cached per-stock theme label.
// It keeps source provenance and evidence role so downstream analysis can prefer retained data
// without conflating it with broad industry/catalog fallbacks.
type StockThemeAttribution struct {
	Kind      ThemeAttributionKind `json:"kind,omitempty"`
	Symbol    string               `json:"symbol"`
	Theme     string               `json:"theme"`
	Concepts  []string             `json:"concepts,omitempty"`
	Source    string               `json:"source"`
	TradeDate string               `json:"trade_date,omitempty"`
	Rank      int                  `json:"rank,omitempty"`
	Role      string               `json:"role,omitempty"`
}

// StockBusinessProfile describes what the company primarily does. It is kept
// separate from market concepts because a broad concept membership is not
// evidence that the stock is currently being traded as that theme.
type StockBusinessProfile struct {
	Symbol        string     `json:"symbol"`
	Name          string     `json:"name,omitempty"`
	MainBusiness  string     `json:"main_business"`
	Industry      string     `json:"industry,omitempty"`
	IndustryPath  string     `json:"industry_path,omitempty"`
	Description   string     `json:"description,omitempty"`
	BusinessScope string     `json:"business_scope,omitempty"`
	Meta          SourceMeta `json:"meta"`
}

// StockFundamentals contains the latest reported financial snapshot used by
// the non-short-term stock route. Canonical units are CNY for amounts and
// percentage points for percentage fields; the period is consolidated cumulative. Providers
// should set Meta.FieldsKnown and list only valid numeric JSON field names in
// Meta.AvailableFields; numeric zero placeholders alone do not prove validity.
type StockFundamentals struct {
	Symbol                                 string     `json:"symbol"`
	ReportDate                             string     `json:"report_date"`
	ReportName                             string     `json:"report_name"`
	PublishedAt                            time.Time  `json:"published_at,omitempty"`
	NotApplicableFields                    []string   `json:"not_applicable_fields,omitempty"`
	Revenue                                float64    `json:"revenue"`
	RevenueYearOverYear                    float64    `json:"revenue_yoy"`
	NetProfit                              float64    `json:"net_profit"`
	NetProfitYearOverYear                  float64    `json:"net_profit_yoy"`
	DeductedNetProfit                      float64    `json:"deducted_net_profit"`
	DeductedNetProfitYearOverYear          float64    `json:"deducted_net_profit_yoy"`
	DeductedNetProfitAvailable             bool       `json:"deducted_net_profit_available"`
	DeductedNetProfitYearOverYearAvailable bool       `json:"deducted_net_profit_yoy_available"`
	DeductedNetProfitReportDate            string     `json:"deducted_net_profit_report_date,omitempty"`
	EPS                                    float64    `json:"eps"`
	ROE                                    float64    `json:"roe"`
	GrossMargin                            float64    `json:"gross_margin"`
	DebtRatio                              float64    `json:"debt_ratio"`
	OperatingCashFlowPerShare              float64    `json:"operating_cash_flow_per_share"`
	Meta                                   SourceMeta `json:"meta"`
}

// FieldAvailable distinguishes reported zeroes from missing financial values.
// Explicit source masks are authoritative. Unmarked historical snapshots retain
// their old finite-value semantics; deducted amount and growth are independent.
func (item StockFundamentals) FieldAvailable(field string) bool {
	var value float64
	legacyAvailable := true
	switch field {
	case "revenue":
		value = item.Revenue
	case "revenue_yoy":
		value = item.RevenueYearOverYear
	case "net_profit":
		value = item.NetProfit
	case "net_profit_yoy":
		value = item.NetProfitYearOverYear
	case "deducted_net_profit":
		value = item.DeductedNetProfit
		legacyAvailable = item.DeductedNetProfitAvailable || value != 0
	case "deducted_net_profit_yoy":
		value = item.DeductedNetProfitYearOverYear
		legacyAvailable = item.DeductedNetProfitYearOverYearAvailable || item.DeductedNetProfitAvailable || value != 0
	case "eps":
		value = item.EPS
	case "roe":
		value = item.ROE
	case "gross_margin":
		value = item.GrossMargin
	case "debt_ratio":
		value = item.DebtRatio
	case "operating_cash_flow_per_share":
		value = item.OperatingCashFlowPerShare
	default:
		return false
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return false
	}
	if item.Meta.FieldsKnown {
		for _, available := range item.Meta.AvailableFields {
			if available == field {
				return true
			}
		}
		return false
	}
	return legacyAvailable
}

// MarketLimitEvent represents one stock in a daily limit-event pool that is
// not necessarily still sealed at the close. It is used for final broken-board
// and limit-down pools while LimitUpEvent remains the richer sealed-limit-up
// record used by the ladder.
type MarketLimitEvent struct {
	Symbol        string     `json:"symbol"`
	Name          string     `json:"name"`
	Date          time.Time  `json:"date"`
	Price         float64    `json:"price"`
	ChangePercent float64    `json:"change_percent"`
	Amount        float64    `json:"amount"`
	Industry      string     `json:"industry,omitempty"`
	Meta          SourceMeta `json:"meta"`
}

type SectorMap struct {
	Theme     string           `json:"theme"`
	Name      string           `json:"name"`
	Tabs      []string         `json:"tabs"`
	ThemeTabs []SectorMapTab   `json:"theme_tabs,omitempty"`
	Groups    []SectorMapGroup `json:"groups"`
	Meta      SourceMeta       `json:"meta"`
}

type SectorMapTab struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ThemeOverview struct {
	Aliases              []string     `json:"aliases,omitempty"`
	LeaderStocks         []BoardStock `json:"leader_stocks,omitempty"`
	Theme                string       `json:"theme"`
	Name                 string       `json:"name"`
	ChangePercent        float64      `json:"change_percent"`
	MainNetInflow        float64      `json:"main_net_inflow"`
	RisingNodes          int          `json:"rising_nodes"`
	FallingNodes         int          `json:"falling_nodes"`
	MatchedNodes         int          `json:"matched_nodes"`
	TotalNodes           int          `json:"total_nodes"`
	TopNode              string       `json:"top_node,omitempty"`
	TopNodeChangePercent float64      `json:"top_node_change_percent"`
	TrendScore           int          `json:"trend_score,omitempty"`
	DailyStrengthScore   int          `json:"daily_strength_score,omitempty"`
	FiveDayStrengthScore int          `json:"five_day_strength_score,omitempty"`
	TrendStage           string       `json:"trend_stage,omitempty"`
	LimitUpCount         int          `json:"limit_up_count,omitempty"`
	BoardCount           int          `json:"board_count,omitempty"`
	PreviousCount        int          `json:"previous_count,omitempty"`
	ActiveDays           int          `json:"active_days,omitempty"`
	MaxStreak            int          `json:"max_streak,omitempty"`
	Leaders              []string     `json:"leaders,omitempty"`
	Source               string       `json:"source,omitempty"`
	SourceRank           int          `json:"source_rank,omitempty"`
	DailyRank            int          `json:"daily_rank,omitempty"`
	FiveDayRank          int          `json:"five_day_rank,omitempty"`
	ProviderRank         int          `json:"provider_rank,omitempty"`
	SourceStrength       float64      `json:"source_strength,omitempty"`
	IndustryDailyScore   int          `json:"industry_daily_score,omitempty"`
	IndustryFiveDayScore int          `json:"industry_five_day_score,omitempty"`
	KaipanlaDailyScore   int          `json:"kaipanla_daily_score,omitempty"`
	KaipanlaFiveDayScore int          `json:"kaipanla_five_day_score,omitempty"`
	TradeDate            string       `json:"trade_date,omitempty"`
	SnapshotID           string       `json:"snapshot_id,omitempty"`
	CarryForward         bool         `json:"carry_forward,omitempty"`
	Provisional          bool         `json:"provisional,omitempty"`
}

type SectorMapGroup struct {
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Nodes []SectorMapNode `json:"nodes"`
}

type SectorMapNode struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	BoardCode      string         `json:"board_code,omitempty"`
	BoardName      string         `json:"board_name,omitempty"`
	BoardSource    string         `json:"board_source,omitempty"`
	BoardRef       *BoardRef      `json:"board_ref,omitempty"`
	MemberSet      *MemberSetMeta `json:"member_set,omitempty"`
	ChangePercent  float64        `json:"change_percent"`
	MainNetInflow  float64        `json:"main_net_inflow"`
	Stocks         []BoardStock   `json:"stocks"`
	StockSource    string         `json:"stock_source,omitempty"`
	MatchStatus    string         `json:"match_status"`
	MatchedBy      []string       `json:"matched_by,omitempty"`
	Warnings       []string       `json:"warnings,omitempty"`
	CandidateCount int            `json:"candidate_count,omitempty"`
}
