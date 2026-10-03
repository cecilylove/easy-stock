package eastmoney

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

var marginFields = []string{"financing_balance", "securities_lending_balance", "margin_balance", "financing_buy_amount", "financing_repay_amount", "financing_net_buy_amount", "securities_lending_sell_volume", "securities_lending_repay_volume"}
var marginMarkets = []string{"001", "002", "007"} // Shenzhen, Beijing, Shanghai.

// Each market contributes at most once. Presence is intersected across all
// contributing rows so a missing field never becomes a valid zero aggregate.
func addMarginMarket(point *foundation.MarketMarginPoint, market string, numbers []catalogNumber) error {
	market = strings.TrimSpace(market)
	known := market == "001" || market == "002" || market == "007"
	if !known {
		// Unknown aggregates cannot safely be combined with native market rows.
		return fmt.Errorf("eastmoney margin balance has unknown market identity %q", market)
	}
	for _, existing := range point.Markets {
		if existing == market {
			return fmt.Errorf("eastmoney margin balance duplicate market %s on %s", market, point.TradeDate)
		}
	}
	first := len(point.Markets) == 0
	point.Markets = append(point.Markets, market)
	values := []*float64{&point.FinancingBalance, &point.SecuritiesLendingBalance, &point.MarginBalance, &point.FinancingBuyAmount, &point.FinancingRepayAmount, &point.FinancingNetBuyAmount, &point.SecuritiesLendingSellVolume, &point.SecuritiesLendingRepayVolume}
	fields := make([]string, 0, len(numbers))
	for i, number := range numbers {
		if number.Valid {
			*values[i] += number.Value
			if !math.IsNaN(*values[i]) && !math.IsInf(*values[i], 0) && (first || foundation.FieldAvailable(point.Meta, marginFields[i])) {
				fields = append(fields, marginFields[i])
			}
		}
	}
	point.Meta.FieldsKnown = true
	point.Meta.AvailableFields = fields
	return nil
}

func adjacentMarginTradingDays(previous, current string) bool {
	date, err := time.Parse("2006-01-02", current)
	if err != nil {
		return false
	}
	for i := 0; i < 32; i++ {
		date = date.AddDate(0, 0, -1)
		if foundation.IsAStockTradingDay(date) {
			return date.Format("2006-01-02") == previous
		}
	}
	return false
}

func finalizeMarginCoverage(point *foundation.MarketMarginPoint) {
	sort.Strings(point.Markets)
	point.CoverageKnown = true
	for _, expected := range marginMarkets {
		found := false
		for _, actual := range point.Markets {
			if expected == actual {
				found = true
				break
			}
		}
		if !found {
			point.MissingMarkets = append(point.MissingMarkets, expected)
		}
	}
	point.CoverageComplete = len(point.MissingMarkets) == 0
	values := []*float64{&point.FinancingBalance, &point.SecuritiesLendingBalance, &point.MarginBalance, &point.FinancingBuyAmount, &point.FinancingRepayAmount, &point.FinancingNetBuyAmount, &point.SecuritiesLendingSellVolume, &point.SecuritiesLendingRepayVolume}
	for i, value := range values {
		if !foundation.FieldAvailable(point.Meta, marginFields[i]) {
			*value = 0
		}
	}
	point.Meta.Partial = !point.CoverageComplete || len(point.Meta.AvailableFields) < len(marginFields)
	point.Meta.MissingIDs = append([]string(nil), point.MissingMarkets...)
	if !point.CoverageComplete {
		point.Meta.FallbackReason = "仅部分市场覆盖，不表示全市场；不计算跨日余额变化"
	}
}
