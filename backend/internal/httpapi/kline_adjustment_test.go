package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"easy-stock/backend/internal/foundation"
)

type adjustedTestProvider struct {
	adjustment string
	period     string
	limit      int
	err        error
}

func (p *adjustedTestProvider) KLine(context.Context, string, string, int) ([]foundation.KLine, error) {
	return nil, errors.New("unexpected implicit request")
}
func (p *adjustedTestProvider) KLineAdjusted(_ context.Context, symbol, period string, limit int, adjustment string) ([]foundation.KLine, error) {
	p.adjustment, p.period, p.limit = adjustment, period, limit
	if p.err != nil {
		return nil, p.err
	}
	return []foundation.KLine{annualTestMonth("2026-09-30", 10, 11, 9, 10.5, 100)}, nil
}
func TestExplicitAdjustmentDoesNotSilentlySwitchFallback(t *testing.T) {
	primary := &adjustedTestProvider{err: errors.New("fixture source unavailable")}
	fallbackCalled := false
	server := NewServer(Config{KLinePrimary: primary, KLineFallback: klineProviderFunc(func(context.Context, string, string, int) ([]foundation.KLine, error) {
		fallbackCalled = true
		return fallbackTestBars(), nil
	})})
	defer server.Close()
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000002.SZ&period=day&adjust=hfq", nil))
	if response.Code != 502 || fallbackCalled || primary.adjustment != "hfq" {
		t.Fatalf("adjustment contract broken: status=%d fallback=%v", response.Code, fallbackCalled)
	}
	primary.err = nil
	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000002.SZ&period=%20year%20&limit=2&adjust=none", nil))
	if response.Code != 200 || primary.period != "month" || primary.limit != 36 || !strings.Contains(response.Body.String(), "不复权") {
		t.Fatalf("wrong adjusted year response %d %s", response.Code, response.Body.String())
	}
	for _, adjust := range []string{"bad", "hfq", "qfq", "none"} {
		response = httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000002.SZ&period=1&detail=1&adjust="+adjust, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("minute detail silently ignored adjustment %s: %d", adjust, response.Code)
		}
	}
	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/quotes/kline?symbol=000002.SZ&adjust=bad", nil))
	if response.Code != 400 {
		t.Fatalf("invalid adjustment accepted: %d", response.Code)
	}
}
