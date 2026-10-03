package eastmoney

import (
	"context"
	"easy-stock/backend/internal/foundation"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMarginIncompleteMarketsDoNotCreateFalseMarketCollapse(t *testing.T) {
	rows := `{"DIM_DATE":"2026-09-29","SCDM":"001","RZYE":100,"RQYE":10,"RZRQYE":110}, {"DIM_DATE":"2026-09-29","SCDM":"002","RZYE":10,"RQYE":0,"RZRQYE":10}, {"DIM_DATE":"2026-09-29","SCDM":"007","RZYE":200,"RQYE":20,"RZRQYE":220}, {"DIM_DATE":"2026-09-30","SCDM":"007","RZYE":210,"RQYE":21,"RZRQYE":231}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"success":true,"result":{"pages":1,"data":[%s]}}`, rows)
	}))
	defer server.Close()
	items, meta, err := NewClient(WithDatacenterBaseURL(server.URL), WithHTTPClient(server.Client())).MarketMarginSeries(context.Background(), 2)
	if err != nil || len(items) != 2 {
		t.Fatalf("%+v %v", items, err)
	}
	latest := items[1]
	if !items[0].CoverageComplete || latest.CoverageComplete || !latest.CoverageKnown || len(latest.Markets) != 1 || len(latest.MissingMarkets) != 2 || latest.ChangeAvailable || foundation.FieldAvailable(latest.Meta, "margin_balance_change") || latest.MarginBalanceChange != 0 || !meta.Partial {
		t.Fatalf("coverage lost: %+v %+v", items, meta)
	}
	if foundation.FieldAvailable(latest.Meta, "financing_net_buy_amount") {
		t.Fatal("missing field became zero net buy")
	}
}

func TestMarginMarketDuplicatesAndUnknownAreRejected(t *testing.T) {
	for _, market := range []string{"007", "999", ""} {
		t.Run(market, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"success":true,"result":{"data":[{"DIM_DATE":"2026-09-30","SCDM":"007"},{"DIM_DATE":"2026-09-30","SCDM":%q}]}}`, market)
			}))
			defer server.Close()
			_, _, err := NewClient(WithDatacenterBaseURL(server.URL), WithHTTPClient(server.Client())).MarketMarginSeries(context.Background(), 2)
			if err == nil {
				t.Fatal("unsafe market rows accepted")
			}
		})
	}
}

func TestMarginCompleteTrueZeroChangeIsAvailable(t *testing.T) {
	point := foundation.MarketMarginPoint{TradeDate: "2026-09-29"}
	for _, market := range marginMarkets {
		if err := addMarginMarket(&point, market, []catalogNumber{{0, true}, {0, true}, {0, true}, {0, true}, {0, true}, {0, true}, {0, true}, {0, true}}); err != nil {
			t.Fatal(err)
		}
	}
	finalizeMarginCoverage(&point)
	if !point.CoverageComplete || !foundation.FieldAvailable(point.Meta, "margin_balance") || point.MarginBalance != 0 || point.Meta.Partial {
		t.Fatalf("zero presence lost %+v", point)
	}
}

func TestMarginOverflowOrMissingFieldDoesNotBecomeValidAggregate(t *testing.T) {
	point := foundation.MarketMarginPoint{TradeDate: "2026-09-30"}
	for _, market := range marginMarkets {
		numbers := make([]catalogNumber, len(marginFields))
		for i := range numbers {
			numbers[i] = catalogNumber{Value: 0, Valid: true}
		}
		numbers[2] = catalogNumber{Value: 1e308, Valid: true}
		if err := addMarginMarket(&point, market, numbers); err != nil {
			t.Fatal(err)
		}
	}
	finalizeMarginCoverage(&point)
	if foundation.FieldAvailable(point.Meta, "margin_balance") || point.MarginBalance != 0 || !point.Meta.Partial {
		t.Fatalf("overflow accepted %+v", point)
	}
}

func TestMarginDailyChangeRequiresCompleteAdjacentDays(t *testing.T) {
	for _, test := range []struct {
		previous string
		change   bool
	}{{"2026-09-29", true}, {"2026-09-28", false}} {
		rows := []string{}
		for _, date := range []string{test.previous, "2026-09-30"} {
			for _, market := range marginMarkets {
				rows = append(rows, fmt.Sprintf(`{"DIM_DATE":%q,"SCDM":%q,"RZYE":0,"RQYE":0,"RZRQYE":0}`, date, market))
			}
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"success":true,"result":{"data":[%s]}}`, strings.Join(rows, ","))
		}))
		items, _, err := NewClient(WithDatacenterBaseURL(server.URL), WithHTTPClient(server.Client())).MarketMarginSeries(context.Background(), 2)
		server.Close()
		if err != nil || len(items) != 2 || items[1].ChangeAvailable != test.change || foundation.FieldAvailable(items[1].Meta, "margin_balance_change") != test.change {
			t.Fatalf("daily change coverage %+v %v", items, err)
		}
	}
}

func TestBillboardNativeSeatCodesNeverBecomeCounts(t *testing.T) {
	for _, code := range []int{11111, 33133, 5} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"success":true,"result":{"data":[{"SECUCODE":"600001.SH","BUY_SEAT":%d,"SELL_SEAT":%d}]}}`, code, code)
		}))
		items, _, err := NewClient(WithDatacenterBaseURL(server.URL), WithHTTPClient(server.Client())).MarketBillboard(context.Background(), "2026-09-30", 1)
		server.Close()
		if err != nil || len(items) != 1 || items[0].SeatCountsKnown || items[0].BuySeats != 0 || items[0].SellSeats != 0 {
			t.Fatalf("native encoding leaked %+v %v", items, err)
		}
	}
}
