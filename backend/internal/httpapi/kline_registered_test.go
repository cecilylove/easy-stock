package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/assembly"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/foundation"
)

type registeredAdjustedPrice struct {
	source string
	calls  int
}

func (p *registeredAdjustedPrice) SupportsAdjustedKLine(symbol, period, adjustment string) bool {
	return symbol == "000002.SZ" && (period == "day" || period == "week" || period == "month") && (adjustment == "qfq" || adjustment == "none")
}

func (p *registeredAdjustedPrice) KLineAdjusted(_ context.Context, symbol, period string, _ int, adjustment string) ([]foundation.KLine, error) {
	p.calls++
	bar := annualTestMonth("2026-09-30", 10, 12, 9, 11, 500)
	bar.Symbol = symbol
	bar.Meta = foundation.SourceMeta{
		Source: p.source + ":stock-kline", Provider: p.source, FetchedAt: time.Now(),
		Period: period, EffectiveAdjustment: adjustment, AdjustmentConvention: p.source + ":own-current",
		BasisID: p.source + ":" + adjustment, VolumeUnit: "shares", AmountCurrency: "CNY",
		FieldsKnown: true, AvailableFields: []string{"open", "high", "low", "close", "volume"},
	}
	return []foundation.KLine{bar}, nil
}

func TestRegisteredStrictPriceSourceHTTPKeepsSupplierConvention(t *testing.T) {
	provider := &registeredAdjustedPrice{source: "alternate"}
	sources, err := registry.New(registry.Entry{
		Descriptor:   registry.Descriptor{ID: "alternate", Name: "Fixture prices", Enabled: true, Implemented: true, Capabilities: []string{"adjusted-kline"}},
		Capabilities: registry.Capabilities{AdjustedKLine: provider},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Config{DataSources: sources, DataSourceRoutes: &assembly.Routes{Strict: []string{"alternate"}}})
	defer server.Close()
	if err := server.StartupError(); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000002.SZ&provider=alternate&adjust=qfq&period=day", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("registered strict source rejected: %d %s", response.Code, response.Body.String())
	}
	var payload struct {
		Data []foundation.KLine `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data) != 1 || payload.Data[0].Meta.Provider != "alternate" || payload.Data[0].Meta.BasisID != "alternate:qfq" || payload.Data[0].Volume != 500 || provider.calls != 1 {
		t.Fatalf("registered convention changed: %+v calls=%d", payload.Data, provider.calls)
	}
	for _, query := range []string{
		"symbol=000002.SZ&provider=alternate&adjust=hfq", "symbol=920001.BJ&provider=alternate&adjust=qfq", "symbol=000002.SZ&provider=tencent&adjust=qfq", "symbol=000002.SZ&provider=eastmoney&adjust=qfq",
	} {
		response = httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?"+query, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("unsupported source/scope accepted: %s: %d %s", query, response.Code, response.Body.String())
		}
	}
	if provider.calls != 1 {
		t.Fatal("unsupported requests contacted registered source")
	}
}
