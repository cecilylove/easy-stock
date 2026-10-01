package marketoverview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/eastmoney"
	"easy-stock/backend/internal/providers/tencent"
)

type controlledIndexes struct {
	failingPrimary
	items  []foundation.MarketIndexSnapshot
	series foundation.MarketIndexSeries
	err    error
	calls  int
	wait   bool
	period string
}

func (p *controlledIndexes) MarketIndexes(ctx context.Context, _ string) ([]foundation.MarketIndexSnapshot, foundation.SourceMeta, error) {
	p.calls++
	if p.wait {
		<-ctx.Done()
		return nil, foundation.SourceMeta{}, ctx.Err()
	}
	return p.items, foundation.SourceMeta{Source: "tencent:index"}, p.err
}
func (p *controlledIndexes) MarketIndexSeries(ctx context.Context, _ string, period string, _ int) (foundation.MarketIndexSeries, error) {
	p.calls++
	p.period = period
	if p.wait {
		<-ctx.Done()
		return foundation.MarketIndexSeries{}, ctx.Err()
	}
	return p.series, p.err
}
func TestIndexesTencentOnlyNeverSupplementMissingIdentity(t *testing.T) {
	preferred := &controlledIndexes{items: []foundation.MarketIndexSnapshot{{ID: "sse", Price: 100, Meta: foundation.SourceMeta{Source: "tencent:index"}}, {ID: "nasdaq_composite", SecID: "usIXIC", Code: ".IXIC", Price: 200}}}
	retired := &controlledIndexes{items: []foundation.MarketIndexSnapshot{{ID: "nasdaq", SecID: "100.NDX", Price: 300}, {ID: "nikkei", Price: 400}}}
	items, meta, err := New(retired, preferred, nil, nil).MarketIndexes(context.Background(), "global")
	if err != nil || len(items) != 2 || retired.calls != 0 || !meta.Partial || len(meta.Observations) != 1 || len(meta.MissingIDs) == 0 {
		t.Fatalf("items=%+v meta=%+v retired=%d err=%v", items, meta, retired.calls, err)
	}
	if CanonicalIndexID("nasdaq") != "nasdaq100" {
		t.Fatal("legacy nasdaq identity changed")
	}
}
func TestIndexProviderRetainsPerBarFieldMasks(t *testing.T) {
	first := foundation.SourceMeta{Source: "tencent:index-kline", FieldsKnown: true, AvailableFields: []string{"close"}}
	second := first
	second.AvailableFields = []string{"close", "change_percent"}
	preferred := &controlledIndexes{series: foundation.MarketIndexSeries{Index: foundation.MarketIndexSnapshot{ID: "sse", Meta: second}, Lines: []foundation.KLine{{Time: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC), Close: 100, Meta: first}, {Time: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), Close: 110, ChangePercent: 10, Meta: second}}, Meta: second}}
	retired := &controlledIndexes{}
	series, err := New(retired, preferred, nil, nil).MarketIndexSeries(context.Background(), "sse", "day", 2)
	if err != nil || retired.calls != 0 || foundation.FieldAvailable(series.Lines[0].Meta, "change_percent") || !foundation.FieldAvailable(series.Lines[1].Meta, "change_percent") || !foundation.FieldAvailable(series.Index.Meta, "change_percent") {
		t.Fatalf("lost masks series=%+v err=%v", series, err)
	}
}
func TestIndexSeriesEmptyAndSlowNeverFallsBack(t *testing.T) {
	for _, wait := range []bool{false, true} {
		preferred := &controlledIndexes{wait: wait}
		retired := &controlledIndexes{}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		series, err := New(retired, preferred, nil, nil).MarketIndexSeries(ctx, "sse", "day", 2)
		cancel()
		if err == nil || retired.calls != 0 || len(series.Meta.Observations) != 1 || !series.Meta.Observations[0].Failed {
			t.Fatalf("wait=%v retired=%d series=%+v err=%v", wait, retired.calls, series, err)
		}
	}
}
func TestIndexCancellationNeverLaunchesRequests(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	preferred, retired := &controlledIndexes{}, &controlledIndexes{}
	p := New(retired, preferred, nil, nil)
	_, _, err := p.MarketIndexes(ctx, "core")
	if err != context.Canceled || preferred.calls != 0 || retired.calls != 0 {
		t.Fatalf("err=%v calls=%d/%d", err, preferred.calls, retired.calls)
	}
	_, err = p.MarketIndexSeries(ctx, "sse", "day", 20)
	if err != context.Canceled || preferred.calls != 0 || retired.calls != 0 {
		t.Fatalf("err=%v calls=%d/%d", err, preferred.calls, retired.calls)
	}
}
func TestIndexSeriesRejectsNDXCompositeSubstitutionWithoutFallback(t *testing.T) {
	preferred := &controlledIndexes{series: foundation.MarketIndexSeries{Index: foundation.MarketIndexSnapshot{ID: "nasdaq", SecID: "usIXIC", Code: ".IXIC"}, Lines: []foundation.KLine{{Time: time.Now(), Close: 100}}}}
	retired := &controlledIndexes{}
	series, err := New(retired, preferred, nil, nil).MarketIndexSeries(context.Background(), "nasdaq", "day", 2)
	if err == nil || retired.calls != 0 || !series.Meta.Observations[0].Failed {
		t.Fatalf("series=%+v retired=%d err=%v", series, retired.calls, err)
	}
}
func TestIndexUnsupportedHasNoRequestsOrObservations(t *testing.T) {
	for _, query := range [][2]string{{"nikkei", "day"}, {"sse", "1"}, {"sse", "5"}, {"sse", "15"}, {"sse", "30"}, {"sse", "60"}, {"sse", "120"}, {"sse", "year"}} {
		preferred, retired := &controlledIndexes{}, &controlledIndexes{}
		series, err := New(retired, preferred, nil, nil).MarketIndexSeries(context.Background(), query[0], query[1], 2)
		if !errors.Is(err, ErrUnsupportedIndexSeries) || preferred.calls != 0 || retired.calls != 0 || len(series.Meta.Observations) != 0 || SupportsIndexSeries(query[0], query[1]) {
			t.Fatalf("query=%v series=%+v err=%v", query, series, err)
		}
	}
}
func TestIndexPeriodAliasesNormalizedBeforeProvider(t *testing.T) {
	for alias, period := range map[string]string{"": "day", "daily": "day", "101": "day", "weekly": "week", "102": "week", "monthly": "month", "103": "month"} {
		preferred := &controlledIndexes{series: foundation.MarketIndexSeries{Index: foundation.MarketIndexSnapshot{ID: "sse"}, Lines: []foundation.KLine{{Time: time.Now(), Close: 100}}}}
		_, err := New(&controlledIndexes{}, preferred, nil, nil).MarketIndexSeries(context.Background(), "sse", alias, 2)
		if err != nil || preferred.period != period || !SupportsIndexSeries("sse", alias) {
			t.Fatalf("alias=%s period=%s err=%v", alias, preferred.period, err)
		}
	}
}

type indexOnlyTransport struct {
	mode           string
	eastmoneyCalls int
	tencentCalls   int
}

func (tr *indexOnlyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host == "retired-eastmoney.invalid" {
		tr.eastmoneyCalls++
		return nil, fmt.Errorf("retired Eastmoney index request")
	}
	tr.tencentCalls++
	if tr.mode == "failed" {
		return nil, fmt.Errorf("Tencent failure")
	}
	body := ""
	if r.URL.Query().Get("param") != "" {
		native := strings.Split(r.URL.Query().Get("param"), ",")[0]
		period := strings.Split(r.URL.Query().Get("param"), ",")[1]
		rows := `[["2026-09-30","100","101","102","99","10"]]`
		if tr.mode == "empty" {
			rows = `[]`
		}
		body = `{"code":0,"data":{"` + native + `":{"` + period + `":` + rows + `}}}`
	} else if tr.mode != "empty" {
		for _, key := range strings.Split(r.URL.Query().Get("q"), ",") {
			fields := make([]string, 33)
			fields[1], fields[3], fields[30], fields[31], fields[32] = "fixture", "100", "20260930150000", "1", "1"
			switch key {
			case "usDJI":
				fields[2] = ".DJI"
			case "usINX":
				fields[2] = ".INX"
			case "usNDX":
				fields[2] = ".NDX"
			case "usIXIC":
				fields[2] = ".IXIC"
			case "r_hkHSI":
				fields[2] = "HSI"
			case "ukUKX":
				fields[2] = "UKX"
			default:
				fields[2] = key[2:]
			}
			body += `v_` + key + `="` + strings.Join(fields, "~") + `";`
		}
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}
func TestIndexTransportNeverUsesEastmoneyIncludingEmptyAndFailure(t *testing.T) {
	for _, mode := range []string{"valid", "empty", "failed"} {
		tr := &indexOnlyTransport{mode: mode}
		hc := &http.Client{Transport: tr}
		em := eastmoney.NewClient(eastmoney.WithBaseURL("https://retired-eastmoney.invalid"), eastmoney.WithQuoteBaseURL("https://retired-eastmoney.invalid"), eastmoney.WithHTTPClient(hc))
		tq := tencent.NewClient(tencent.WithQuoteBaseURL("https://tencent.invalid"), tencent.WithKLineBaseURL("https://tencent.invalid/kline"), tencent.WithHTTPClient(hc))
		p := New(em, tq, nil, nil)
		for _, scope := range []string{"core", "global"} {
			items, meta, err := p.MarketIndexes(context.Background(), scope)
			if mode == "valid" {
				want := 12
				if scope == "global" {
					want = 13
				}
				if err != nil || len(items) != want || len(meta.Observations) != 1 {
					t.Fatalf("scope=%s items=%d meta=%+v err=%v", scope, len(items), meta, err)
				}
				if scope == "global" && (!meta.Partial || strings.Join(meta.MissingIDs, ",") != "nikkei,kospi,taiwan,dax,cac") {
					t.Fatalf("missing IDs=%v", meta.MissingIDs)
				}
			} else if err == nil || len(meta.Observations) != 1 || !meta.Observations[0].Failed {
				t.Fatalf("mode=%s meta=%+v err=%v", mode, meta, err)
			}
		}
		for _, period := range []string{"day", "weekly", "103"} {
			_, err := p.MarketIndexSeries(context.Background(), "sse", period, 2)
			if (err == nil) != (mode == "valid") {
				t.Fatalf("mode=%s period=%s err=%v", mode, period, err)
			}
		}
		_, _ = p.MarketIndexSeries(context.Background(), "nasdaq", "day", 2)
		_, _ = p.MarketIndexSeries(context.Background(), "ixic", "day", 2)
		before := tr.tencentCalls
		_, _ = p.MarketIndexSeries(context.Background(), "sse", "5", 2)
		_, _ = p.MarketIndexSeries(context.Background(), "nikkei", "day", 2)
		if tr.eastmoneyCalls != 0 || tr.tencentCalls != before {
			t.Fatalf("mode=%s EM calls=%d unsupported requests=%d", mode, tr.eastmoneyCalls, tr.tencentCalls-before)
		}
	}
}
