package stockanalysis

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"

	"easy-stock/backend/internal/foundation"
)

func financialMask(fields ...string) foundation.SourceMeta {
	return foundation.SourceMeta{FieldsKnown: true, AvailableFields: fields}
}

func TestFinancialAnalysisRealZeroVersusMissing(t *testing.T) {
	zero := analyzeFundamentals(&foundation.StockFundamentals{
		ReportDate: "2026-06-30", NetProfit: 20, DeductedNetProfitAvailable: true,
		Meta: financialMask("net_profit", "deducted_net_profit", "deducted_net_profit_yoy", "revenue_yoy", "roe", "gross_margin", "debt_ratio", "operating_cash_flow_per_share"),
	})
	if !zero.FieldsKnown || !zero.Available || !zero.ScoreAvailable || !zero.RecurringNetProfitAvailable || !zero.RecurringNetProfitYearOverYearAvailable {
		t.Fatalf("real zero lost field availability: %+v", zero)
	}
	if !slices.Contains(zero.AvailableFields, "gross_margin") || !strings.Contains(zero.Summary, "毛利率0.0%") || !strings.Contains(zero.Summary, "扣非净利同比+0.0%") || zero.Sustainability != "较差" {
		t.Fatalf("real zero became unknown: %+v", zero)
	}
	missing := analyzeFundamentals(&foundation.StockFundamentals{
		ReportDate: "2026-06-30", RevenueYearOverYear: 100, ROE: 99, DebtRatio: 20,
		DeductedNetProfitAvailable: true, DeductedNetProfit: 100, DeductedNetProfitYearOverYear: 100,
		Meta: financialMask(),
	})
	if !missing.FieldsKnown || missing.Available || missing.ScoreAvailable || missing.Score != 0 || missing.Quality != "数据不足" || len(missing.AvailableFields) != 0 || missing.RecurringNetProfitAvailable || missing.RecurringNetProfitYearOverYearAvailable {
		t.Fatalf("missing fields or contradictory legacy flags participated: %+v", missing)
	}
	for _, phrase := range []string{"营收同比未知", "归母净利同比未知", "扣非净利同比未知", "ROE 未知", "毛利率未知", "负债率未知", "EPS 未知", "每股经营现金流未知", "扣非净利润未知", "未知不代表无风险"} {
		if !strings.Contains(missing.Summary, phrase) {
			t.Fatalf("missing %q in %q", phrase, missing.Summary)
		}
	}
	if missing.RevenueYearOverYear != 0 || missing.ROE != 0 || missing.RecurringNetProfit != 0 {
		t.Fatalf("invalid placeholder leaked through output: %+v", missing)
	}
}

func TestFinancialAnalysisDeductedAmountAndGrowthAreIndependent(t *testing.T) {
	item := foundation.StockFundamentals{
		ReportDate: "2026-06-30", NetProfit: 100, NetProfitYearOverYear: 120,
		DeductedNetProfit: 80, DeductedNetProfitYearOverYear: 200,
		DeductedNetProfitAvailable: true,
		Meta:                       financialMask("net_profit", "net_profit_yoy", "deducted_net_profit"),
	}
	amountOnly := analyzeFundamentals(&item)
	if !amountOnly.RecurringNetProfitAvailable || amountOnly.RecurringNetProfitYearOverYearAvailable || amountOnly.RecurringNetProfitYearOverYear != 0 || amountOnly.Score != 44 {
		t.Fatalf("missing deducted growth participated or borrowed parent growth: %+v", amountOnly)
	}
	if !strings.Contains(amountOnly.Summary, "扣非净利同比未知") || slices.Contains(amountOnly.AvailableFields, "recurring_net_profit_yoy") {
		t.Fatalf("missing growth displayed as zero: %+v", amountOnly)
	}
	status, message := fundamentalQualityStatus(&amountOnly)
	if status != "limited" || !strings.Contains(message, "扣非同比") {
		t.Fatalf("partial report promoted to ready: %s %s", status, message)
	}
	item.Meta = financialMask("net_profit", "net_profit_yoy", "deducted_net_profit_yoy")
	growthOnly := analyzeFundamentals(&item)
	if growthOnly.RecurringNetProfitAvailable || !growthOnly.RecurringNetProfitYearOverYearAvailable || growthOnly.RecurringNetProfit != 0 || growthOnly.Score != 70 || growthOnly.Sustainability != "待确认" {
		t.Fatalf("deducted growth fabricated an amount or ratio: %+v", growthOnly)
	}
	if slices.Contains(growthOnly.AvailableFields, "non_recurring_profit") || slices.Contains(growthOnly.AvailableFields, "non_recurring_profit_ratio") || !strings.Contains(growthOnly.Summary, "扣非净利润未知") {
		t.Fatalf("unknown amount computed into derived output: %+v", growthOnly)
	}
}

func TestFinancialIndustryMissingGrossMarginDoesNotScoreAsZero(t *testing.T) {
	item := foundation.StockFundamentals{
		ReportDate: "2026-06-30", RevenueYearOverYear: 16, NetProfit: 100,
		DeductedNetProfit: 95, DeductedNetProfitYearOverYear: 12, ROE: 16,
		GrossMargin: 99, DebtRatio: 80, OperatingCashFlowPerShare: 1,
		Meta: financialMask("revenue_yoy", "net_profit", "deducted_net_profit", "deducted_net_profit_yoy", "roe", "debt_ratio", "operating_cash_flow_per_share"),
	}
	got := analyzeFundamentals(&item)
	if got.Score != 59 || got.GrossMargin != 0 || slices.Contains(got.AvailableFields, "gross_margin") || !strings.Contains(got.Summary, "毛利率未知") {
		t.Fatalf("non-applicable bank gross margin affected score: %+v", got)
	}
	status, message := fundamentalQualityStatus(&got)
	if status != "limited" || !strings.Contains(message, "毛利率") || !strings.Contains(message, "不能据此排除风险") {
		t.Fatalf("bank missing fields claimed complete validation: %s %s", status, message)
	}
}

func TestFinancialAnalysisRequiresBothAmountsForNonRecurringEvidence(t *testing.T) {
	got := analyzeFundamentals(&foundation.StockFundamentals{
		ReportDate: "2026-06-30", NetProfit: 100, DeductedNetProfit: 50,
		Meta: financialMask("deducted_net_profit"),
	})
	if got.Sustainability != "待确认" || got.NonRecurringProfit != 0 || got.NonRecurringProfitRatio != 0 || got.ScoreAvailable || got.Score != 0 {
		t.Fatalf("missing parent profit became zero or safe evidence: %+v", got)
	}
	if !got.Available || slices.Contains(got.AvailableFields, "non_recurring_profit") {
		t.Fatalf("amount contract incorrect: %+v", got)
	}
	zeroDenominator := analyzeFundamentals(&foundation.StockFundamentals{
		ReportDate: "2026-06-30", NetProfit: 0, DeductedNetProfit: -10,
		Meta: financialMask("net_profit", "deducted_net_profit"),
	})
	if !slices.Contains(zeroDenominator.AvailableFields, "non_recurring_profit") || slices.Contains(zeroDenominator.AvailableFields, "non_recurring_profit_ratio") || zeroDenominator.Sustainability != "待确认" || zeroDenominator.ScoreAvailable {
		t.Fatalf("zero denominator fabricated a safe ratio: %+v", zeroDenominator)
	}
}

func TestFinancialLegacyCompatibilityAndNonFiniteOutput(t *testing.T) {
	legacy := analyzeFundamentals(&foundation.StockFundamentals{
		ReportDate: "2026-06-30", RevenueYearOverYear: 16, NetProfitYearOverYear: 12,
		ROE: 16, GrossMargin: 40, DebtRatio: 30, OperatingCashFlowPerShare: 1,
	})
	if legacy.Score != 84 || !legacy.ScoreAvailable || !legacy.Available || legacy.RecurringNetProfitAvailable || legacy.RecurringNetProfitYearOverYearAvailable {
		t.Fatalf("unmarked fixture score changed: %+v", legacy)
	}
	var historical foundation.StockFundamentals
	if err := json.Unmarshal([]byte(`{"report_date":"2026-06-30","net_profit":100,"deducted_net_profit":95,"deducted_net_profit_available":true,"deducted_net_profit_yoy":0}`), &historical); err != nil {
		t.Fatal(err)
	}
	old := analyzeFundamentals(&historical)
	if !old.RecurringNetProfitAvailable || !old.RecurringNetProfitYearOverYearAvailable || !slices.Contains(old.AvailableFields, "recurring_net_profit_yoy") {
		t.Fatalf("old explicit zero snapshot incompatible: %+v", old)
	}
	// Even unmarked or explicitly masked non-finite values are unavailable.
	for _, meta := range []foundation.SourceMeta{{}, financialMask("revenue_yoy", "roe", "deducted_net_profit", "deducted_net_profit_yoy")} {
		got := analyzeFundamentals(&foundation.StockFundamentals{
			ReportDate: "2026-06-30", RevenueYearOverYear: math.NaN(), ROE: math.Inf(1),
			DeductedNetProfit: math.Inf(-1), DeductedNetProfitYearOverYear: math.NaN(), Meta: meta,
		})
		for _, field := range []string{"revenue_yoy", "roe", "recurring_net_profit", "recurring_net_profit_yoy"} {
			if slices.Contains(got.AvailableFields, field) {
				t.Fatalf("non-finite field marked available: %+v", got)
			}
		}
		if _, err := json.Marshal(got); err != nil {
			t.Fatalf("invalid analysis JSON: %v", err)
		}
	}
	// A valid zero deducted growth alone cannot fabricate the missing amount.
	growthOnly := analyzeFundamentals(&foundation.StockFundamentals{
		ReportDate: "2026-06-30", DeductedNetProfitYearOverYearAvailable: true,
	})
	if growthOnly.RecurringNetProfitAvailable || !growthOnly.RecurringNetProfitYearOverYearAvailable {
		t.Fatalf("legacy independent flags lost: %+v", growthOnly)
	}
}

func TestFinancialUnscoredSnapshotExcludedFromSignalsOnBothRoutes(t *testing.T) {
	for _, days := range []int{10, 60} {
		analysis, err := Analyze(Input{
			Symbol: "600000.SH", KLines: syntheticTrendLines("600000.SH", days, 10, .1, 500_000_000),
			Fundamentals: &foundation.StockFundamentals{ReportDate: "2026-06-30", EPS: 0, Meta: financialMask("eps")},
		})
		if err != nil {
			t.Fatal(err)
		}
		if analysis.Fundamental == nil || !analysis.Fundamental.Available || analysis.Fundamental.ScoreAvailable {
			t.Fatalf("EPS-only snapshot contract incorrect for %d days: %+v", days, analysis.Fundamental)
		}
		for _, signal := range analysis.Signals {
			if signal.Key == "fundamental" {
				t.Fatalf("unscored financial snapshot entered signal calculation for %d days: %+v", days, signal)
			}
		}
		for _, dimension := range analysis.Scorecard.Dimensions {
			if dimension.Key == "fundamental" && dimension.Status != "missing" {
				t.Fatalf("unscored financial snapshot entered scorecard for %d days: %+v", days, dimension)
			}
		}
	}
}

func TestFinancialDerivedOverflowStaysUnknownAndJSONSafe(t *testing.T) {
	for _, tc := range []struct{ net, deducted float64 }{
		{math.MaxFloat64, -math.MaxFloat64}, {1e-8, math.MaxFloat64}, {1, 1e306},
	} {
		got := analyzeFundamentals(&foundation.StockFundamentals{
			ReportDate: "2026-06-30", NetProfit: tc.net, DeductedNetProfit: tc.deducted,
			Meta: financialMask("net_profit", "deducted_net_profit"),
		})
		if _, err := json.Marshal(got); err != nil {
			t.Fatalf("derived overflow leaked into JSON: %+v: %v", got, err)
		}
	}
}
