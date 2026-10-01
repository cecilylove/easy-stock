package tencent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestStockKLineStrictKeyMatrixAndUnits(t *testing.T) {
	for _, period := range []string{"day", "week", "month"} {
		for _, adjust := range []string{"none", "qfq", "hfq"} {
			t.Run(period+"-"+adjust, func(t *testing.T) {
				suffix, key := adjust, adjust+period
				if adjust == "none" {
					suffix, key = "", period
				}
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if got := r.URL.Query().Get("param"); got != "sz000002,"+period+",,,2,"+suffix {
						t.Errorf("unexpected param %s", got)
					}
					fmt.Fprintf(w, `{"code":0,"data":{"sz000002":{"%s":[["2026-09-29","3.7","4.08","4.1","3.6","800"],["2026-09-30","3.8","4.26","4.4","3.67","123.5"]],"qt":{"sz000002":["snapshot-not-historical-amount"]}}}}`, key)
				}))
				defer upstream.Close()
				client := NewStockKLineClient(NewClient(WithKLineBaseURL(upstream.URL), WithHTTPClient(upstream.Client())))
				lines, err := client.KLineAdjusted(context.Background(), "000002.SZ", period, 2, adjust)
				if err != nil || len(lines) != 2 {
					t.Fatalf("lines=%+v err=%v", lines, err)
				}
				bar := lines[1]
				if bar.Volume != 12350 || bar.Amount != 0 || bar.Meta.VolumeUnit != "shares" || bar.Meta.EffectiveAdjustment != adjust || bar.Meta.Provider != "tencent" || bar.PreviousClose != 4.08 || foundation.FieldAvailable(bar.Meta, "amount") {
					t.Fatalf("contract %+v", bar)
				}
				if _, offset := bar.Time.Zone(); offset != 8*60*60 {
					t.Fatalf("host-dependent time %s", bar.Time)
				}
			})
		}
	}
}

func TestStockKLineBoardSpecificVolumeUnits(t *testing.T) {
	for _, sample := range []struct {
		symbol, native string
		volume         float64
	}{{"688300.SH", "sh688300", 6197446}, {"300750.SZ", "sz300750", 29699500}, {"600519.SH", "sh600519", 29699500}, {"000002.SZ", "sz000002", 29699500}} {
		for _, period := range []string{"day", "week", "month"} {
			for _, adjust := range []string{"none", "qfq", "hfq"} {
				t.Run(sample.symbol+period+adjust, func(t *testing.T) {
					key := adjust + period
					if adjust == "none" {
						key = period
					}
					nativeVolume := "296995"
					if sample.native == "sh688300" {
						nativeVolume = "6197446"
					}
					upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						fmt.Fprintf(w, `{"code":0,"data":{"%s":{"%s":[["2026-09-30","149","144","151","143","%s"]]}}}`, sample.native, key, nativeVolume)
					}))
					defer upstream.Close()
					client := NewStockKLineClient(NewClient(WithKLineBaseURL(upstream.URL), WithHTTPClient(upstream.Client())))
					bars, err := client.KLineAdjusted(context.Background(), sample.symbol, period, 1, adjust)
					if err != nil || len(bars) != 1 || bars[0].Volume != sample.volume || bars[0].Meta.VolumeUnit != "shares" {
						t.Fatalf("board volume %+v %v", bars, err)
					}
				})
			}
		}
	}
}

func TestStockKLineQuoteUnitEvidenceRejectsWrongConvention(t *testing.T) {
	for _, sample := range []struct{ symbol, native, high, low, triple string }{{"688300.SH", "sh688300", "151.57", "143.10", "144.30/6197446/908273486"}, {"300750.SZ", "sz300750", "292.70", "285.80", "291.11/296995/8613929784"}} {
		fields := make([]string, 36)
		fields[33], fields[34], fields[35] = sample.high, sample.low, sample.triple
		raw, _ := json.Marshal(map[string][]string{sample.native: fields})
		symbol, _ := foundation.NormalizeSymbol(sample.symbol)
		if err := validateStockQuoteVolumeUnit(raw, sample.native, symbol); err != nil {
			t.Fatalf("actual sampled unit evidence rejected %s %v", sample.symbol, err)
		}
		parts := strings.Split(sample.triple, "/")
		parts[2] = "1"
		fields[35] = strings.Join(parts, "/")
		raw, _ = json.Marshal(map[string][]string{sample.native: fields})
		if err := validateStockQuoteVolumeUnit(raw, sample.native, symbol); err == nil {
			t.Fatalf("contradictory unit accepted %s", sample.symbol)
		}
	}
	if SupportsStockKLine("689009.SH", "day", "none") {
		t.Fatal("unverified depositary receipt board claimed")
	}
}

func TestStockKLineNoDataIsInstrumentScoped(t *testing.T) {
	for _, body := range []string{"", "   ", `{"code":0,"data":null}`, `{"code":0,"data":{}}`, `{"code":0,"data":{"sz009999":{"qfqday":null}}}`, `{"code":0,"data":{"sz009999":{"qfqday":[]}}}`, `{"code":0,"data":{"sz009999":{"day":[["2026-09-30","1","2","3","1","10"]]}}}`} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		client := NewStockKLineClient(NewClient(WithKLineBaseURL(upstream.URL), WithHTTPClient(upstream.Client())))
		_, err := client.KLineAdjusted(context.Background(), "009999.SZ", "day", 1, "qfq")
		upstream.Close()
		if !errors.Is(err, foundation.ErrPriceNoData) {
			t.Errorf("missing symbol/series not typed: %s %v", body, err)
		}
	}
}

func TestStockKLineDoesNotBorrowRawOrSnapshotFields(t *testing.T) {
	for _, body := range []string{
		`{"code":0,"data":{"sz000002":{"day":[["2026-09-30","3.8","4.26","4.4","3.67","123"]]}}}`,
		`{"code":0,"data":{"sz000002":{"qfqday":[]}}}`,
		`{"code":0,"data":null}`,
		`{"code":0,"data":{"sz000002":{"qfqday":[["2026-09-30","3.8","4.26","bad","3.67","123"]]}}}`,
		`{"code":0,"data":{"sz000002":{"qfqday":[["2026-09-30","3.8","4.26","3","3.67","123"]]}}}`,
	} {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		client := NewStockKLineClient(NewClient(WithKLineBaseURL(upstream.URL), WithHTTPClient(upstream.Client())))
		if lines, err := client.KLineAdjusted(context.Background(), "000002.SZ", "day", 2, "qfq"); err == nil || len(lines) > 0 {
			t.Errorf("accepted invalid/mismatched strict response: %s", body)
		}
		upstream.Close()
	}
}

func TestStockKLineSupportsAdjustedNegativeAndZeroPrices(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("param")
		key := "day"
		if strings.HasSuffix(query, "qfq") {
			key = "qfqday"
		}
		fmt.Fprintf(w, `{"code":0,"data":{"sz000002":{"%s":[["2000-01-03","-2","0","1","-3","123"]]}}}`, key)
	}))
	defer upstream.Close()
	client := NewStockKLineClient(NewClient(WithKLineBaseURL(upstream.URL), WithHTTPClient(upstream.Client())))
	if lines, err := client.KLineAdjusted(context.Background(), "000002.SZ", "day", 1, "qfq"); err != nil || len(lines) != 1 || lines[0].Open != -2 || lines[0].Close != 0 {
		t.Fatalf("valid adjusted prices rejected %+v %v", lines, err)
	}
	if _, err := client.KLineAdjusted(context.Background(), "000002.SZ", "day", 1, "none"); err == nil {
		t.Fatal("accepted negative transaction price")
	}
}

func TestStockKLineUnsupportedMakesNoRequestAndCancellation(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); <-r.Context().Done() }))
	defer upstream.Close()
	client := NewStockKLineClient(NewClient(WithKLineBaseURL(upstream.URL), WithHTTPClient(upstream.Client())))
	for _, request := range [][3]string{{"920001.BJ", "day", "qfq"}, {"000002.SZ", "5", "none"}, {"000002.SZ", "day", "invalid"}, {"000001.SH", "day", "none"}} {
		if _, err := client.KLineAdjusted(context.Background(), request[0], request[1], 2, request[2]); err == nil {
			t.Errorf("accepted unsupported %v", request)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("unsupported capability attempted upstream")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := client.KLineAdjusted(ctx, "000002.SZ", "day", 2, "qfq"); err == nil {
		t.Fatal("slow source ignored cancellation")
	}
}

func TestStockKLineUnorderedSortDuplicateRejectAndMask(t *testing.T) {
	meta := foundation.SourceMeta{AvailableFields: []string{"open", "high", "low", "close", "volume"}, FieldsKnown: true}
	rows := [][]any{{"2026-09-30", "1", "2", "3", "0", "10"}, {"2026-09-29", "0", "0", "1", "-1", "10"}}
	lines, err := parseStockKLines(rows, "000002.SZ", "qfq", 2, meta)
	if err != nil || len(lines) != 2 || lines[0].Time.Day() != 29 || foundation.FieldAvailable(lines[1].Meta, "change_percent") {
		t.Fatalf("zero predecessor misleading return %+v %v", lines, err)
	}
	rows = append(rows, rows[0])
	if _, err := parseStockKLines(rows, "000002.SZ", "qfq", 3, meta); err == nil {
		t.Fatal("duplicate dates accepted")
	}
}
