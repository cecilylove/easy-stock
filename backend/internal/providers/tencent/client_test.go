package tencent

import (
	"context"
	"easy-stock/backend/internal/foundation"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientParsesIndexSnapshotsAndSeries(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("param") != "" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0,"data":{"sh000001":{"day":[["2026-08-10","3943.82","3966.59","3967.59","3938.63","542118110"],["2026-08-11","3950.71","3934.09","3966.39","3930.64","529490944"]]}}}`))
			return
		}
		fields := make([]string, 33)
		fields[1], fields[2], fields[3], fields[30], fields[31], fields[32] = "SSE", "000001", "3934.09", "20260811150000", "-32.50", "-0.82"
		_, _ = w.Write([]byte("v_sh000001=\"" + strings.Join(fields, "~") + "\";\n" +
			"v_r_hkHSI=\"100~HSI~HSI~25652.820~25937.490~25998.590~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~2026/08/11 18:31:13~-284.670~-1.10~\";"))
	}))
	defer upstream.Close()
	client := NewClient(WithQuoteBaseURL(upstream.URL), WithKLineBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))

	indexes, meta, err := client.MarketIndexes(context.Background(), "global")
	if err != nil || len(indexes) != 2 || indexes[0].ID != "sse" || indexes[0].Price != 3934.09 || indexes[1].ID != "hsi" || indexes[1].ChangePercent != -1.10 || meta.Source != "tencent:index" {
		t.Fatalf("indexes=%+v meta=%+v err=%v", indexes, meta, err)
	}
	series, err := client.MarketIndexSeries(context.Background(), "sse", "day", 2)
	if err != nil || len(series.Lines) != 2 || series.Lines[1].Close != 3934.09 || series.Lines[1].ChangePercent >= 0 || series.Meta.Source != "tencent:index-kline" {
		t.Fatalf("series=%+v err=%v", series, err)
	}
}

func TestIndexIdentitiesAndForeignTimestampRemainExplicit(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("q"), "usNDX") {
			t.Error("NDX identity missing")
		}
		for _, code := range []string{"NDX", "IXIC"} {
			fields := make([]string, 33)
			fields[1], fields[2], fields[3], fields[30], fields[31], fields[32] = code, "."+code, "12345", "2026-09-30 17:15:59", "100", "1.2"
			_, _ = w.Write([]byte("v_us" + code + "=\"" + strings.Join(fields, "~") + "\";"))
		}
	}))
	defer upstream.Close()
	client := NewClient(WithQuoteBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
	items, _, err := client.MarketIndexes(context.Background(), "core")
	if err != nil || len(items) != 2 || items[0].ID != "nasdaq100" || items[1].ID != "nasdaq_composite" {
		t.Fatalf("items=%+v err=%v", items, err)
	}
	for _, item := range items {
		if !item.TradeTime.IsZero() || item.Status != "unknown" || item.Meta.TimeZone != "unknown" || item.Meta.NativeTimestamp == "" {
			t.Fatalf("foreign timezone fabricated: %+v", item)
		}
	}
	definition, _ := findIndex("nasdaq")
	if definition.KLineKey != "usNDX" {
		t.Fatalf("legacy identity=%+v", definition)
	}
}

func TestIndexSeriesReverseOrderUsesOnlyEarlierClose(t *testing.T) {
	for _, period := range []string{"day", "week", "month"} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":0,"data":{"sh000001":{"` + period + `":[["2026-09-30","110","110","111","109","10"],["2026-09-29","100","100","101","99","10"]]}}}`))
		}))
		client := NewClient(WithKLineBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
		series, err := client.MarketIndexSeries(context.Background(), "sse", period, 2)
		upstream.Close()
		if err != nil || len(series.Lines) != 2 {
			t.Fatalf("period=%s series=%+v err=%v", period, series, err)
		}
		first, second := series.Lines[0], series.Lines[1]
		if first.Close != 100 || first.ChangePercent != 0 || foundation.FieldAvailable(first.Meta, "change_percent") || !foundation.FieldAvailable(second.Meta, "change_percent") || math.Abs(second.ChangePercent-10) > 1e-8 {
			t.Fatalf("causal change/masks invalid: %+v", series.Lines)
		}
	}
}

func TestIndexSeriesRejectsEmptyAndInvalidNumbers(t *testing.T) {
	for _, rows := range []string{`[]`, `[["bad","1","2","3","1","0"]]`, `[["2026-09-30","1","NaN","3","1","0"]]`, `[["2026-09-30","0","0","0","0","0"]]`} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Query().Get("param"), ",qfq") {
				t.Error("index should not request stock qfq")
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"usNDX":{"day":` + rows + `}}}`))
		}))
		client := NewClient(WithKLineBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
		series, err := client.MarketIndexSeries(context.Background(), "nasdaq", "day", 2)
		upstream.Close()
		if err == nil || len(series.Lines) != 0 {
			t.Fatalf("rows=%s series=%+v err=%v", rows, series, err)
		}
	}
}

func TestClientParsesIndustryMomentum(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":[{
			"bd_name":"光伏设备","bd_code":"pt01801735","bd_zxj":"5606.59","bd_zdf":"1.76","bd_zdf5":"5.48","bd_zdf20":"2.35",
			"nzg_code":"sz300051","nzg_name":"琏升科技","nzg_zxj":"8.93","nzg_zdf":"7.59"
		}]}`))
	}))
	defer upstream.Close()
	client := NewClient(WithIndustryBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))

	items, meta, err := client.IndustryMomentum(context.Background(), 20)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%+v meta=%+v err=%v", items, meta, err)
	}
	item := items[0]
	if item.Name != "光伏设备" || item.ChangePercent != 1.76 || item.FiveDayChangePercent != 5.48 || item.TwentyDayChange != 2.35 || item.LeaderSymbol != "300051.SZ" || item.LeaderName != "琏升科技" || item.Score <= 50 || meta.Source != "tencent:industry-rank" {
		t.Fatalf("item=%+v meta=%+v", item, meta)
	}
}

func TestClientParsesUSSectorETFQuotes(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("q"), "usXLK") || !strings.Contains(r.URL.Query().Get("q"), "usXLF") {
			http.Error(w, "missing US sector ETF query", http.StatusBadRequest)
			return
		}
		technology := make([]string, 33)
		technology[3] = "185.69"
		technology[30] = "2026/08/28 16:00:01"
		technology[32] = "-1.55"
		financial := make([]string, 33)
		financial[3] = "58.10"
		financial[30] = "2026/08/28 16:00:01"
		financial[32] = "0.38"
		_, _ = w.Write([]byte("v_usXLK=\"" + strings.Join(technology, "~") + "\";v_usXLF=\"" + strings.Join(financial, "~") + "\";"))
	}))
	defer upstream.Close()
	client := NewClient(WithQuoteBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))

	items, meta, err := client.USSectorMomentum(context.Background(), 11)
	if err != nil || len(items) != 2 || items[0].ProxySymbol != "XLF" || items[0].ChangePercent != 0.38 || items[1].ProxySymbol != "XLK" || items[1].ChangePercent != -1.55 {
		t.Fatalf("items=%+v meta=%+v err=%v", items, meta, err)
	}
	if meta.Source != "tencent:us-sector-etf" || !items[0].TradeTime.IsZero() || items[0].Meta.TimeZone != "unknown" || items[0].Meta.NativeTimestamp == "" {
		t.Fatalf("unexpected US sector metadata: %+v %+v", items, meta)
	}
}

func TestClientParsesIndustryStocks(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("board_code") != "pt01801039" || r.URL.Query().Get("count") != "200" || r.URL.Query().Get("offset") != "0" {
			http.Error(w, "unexpected query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"total":2,"rank_list":[
			{"code":"sh688300","name":"联瑞新材","zxj":"186.24","zd":"31.04","zdf":"20.00","turnover":"376979","volume":"218402.46","zsz":"449.71","ltsz":"449.71"},
			{"code":"sz301071","name":"力量钻石","zxj":"61.77","zd":"4.77","zdf":"8.37","turnover":"97585","volume":"162237.00","zsz":"157.17","ltsz":"115.54"}
		]}}`))
	}))
	defer upstream.Close()
	client := NewClient(WithIndustryStocksBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))

	stocks, meta, err := client.IndustryStocks(context.Background(), "pt01801039", 500)
	if err != nil || len(stocks) != 2 {
		t.Fatalf("stocks=%+v meta=%+v err=%v", stocks, meta, err)
	}
	if stocks[0].Symbol != "688300.SH" || stocks[0].Name != "联瑞新材" || stocks[0].ChangePercent != 20 || stocks[0].Amount != 2_184_024_600 || stocks[1].Symbol != "301071.SZ" {
		t.Fatalf("unexpected industry stocks: %+v", stocks)
	}
	if meta.Source != "tencent:industry-constituents" {
		t.Fatalf("unexpected meta: %+v", meta)
	}
}
