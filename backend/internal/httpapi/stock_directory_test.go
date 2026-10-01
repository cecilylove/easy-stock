package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type countingStockDirectoryProvider struct {
	calls atomic.Int32
}

func (provider *countingStockDirectoryProvider) StockCatalog(context.Context) ([]foundation.StockCatalogEntry, error) {
	provider.calls.Add(1)
	meta := foundation.SourceMeta{Source: "test:stock-directory", FetchedAt: time.Date(2026, 8, 9, 9, 30, 0, 0, time.Local)}
	return []foundation.StockCatalogEntry{
		{BoardStock: foundation.BoardStock{Symbol: "600519.SH", Name: "贵州茅台", Meta: meta}},
		{BoardStock: foundation.BoardStock{Symbol: "000001.SZ", Name: "平安银行", Meta: meta}},
		{BoardStock: foundation.BoardStock{Symbol: "600519.SH", Name: "重复项", Meta: meta}},
		{BoardStock: foundation.BoardStock{Symbol: "", Name: "无效项", Meta: meta}},
	}, nil
}

func TestStockDirectoryCachesNamesAndCodes(t *testing.T) {
	provider := &countingStockDirectoryProvider{}
	server := NewServer(Config{StockDirectory: provider})

	for requestNumber := 0; requestNumber < 2; requestNumber++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/stocks/directory", nil)
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, body = %s", requestNumber+1, rec.Code, rec.Body.String())
		}
		var payload struct {
			Data stockDirectoryData `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode stock directory: %v", err)
		}
		if payload.Data.Total != 2 || len(payload.Data.Stocks) != 2 {
			t.Fatalf("unexpected directory size: %+v", payload.Data)
		}
		if payload.Data.Stocks[0].Code != "000001" || payload.Data.Stocks[0].Name != "平安银行" || payload.Data.Stocks[1].Symbol != "600519.SH" {
			t.Fatalf("unexpected stock directory: %+v", payload.Data.Stocks)
		}
		if payload.Data.Source != "test:stock-directory" || payload.Data.UpdatedAt.IsZero() || payload.Data.ExpiresAt.IsZero() {
			t.Fatalf("missing directory cache metadata: %+v", payload.Data)
		}
	}

	if provider.calls.Load() != 1 {
		t.Fatalf("catalog calls = %d, want one cached load", provider.calls.Load())
	}
}

type observationDirectoryProvider struct{ fail bool }

func (p *observationDirectoryProvider) StockCatalog(context.Context) ([]foundation.StockCatalogEntry, error) {
	if p.fail {
		return nil, errors.New("upstream offline")
	}
	return []foundation.StockCatalogEntry{{BoardStock: foundation.BoardStock{Symbol: "000002.SZ", Name: "万科A", Meta: foundation.SourceMeta{Source: "sina:stock-directory", FetchedAt: time.Now()}}}}, nil
}

func TestDirectoryObservesOnlyActualLoadAndRetainsFailureWithStaleCache(t *testing.T) {
	provider := &observationDirectoryProvider{}
	cache := newStockDirectoryCache(time.Hour)
	health := newSourceHealthTracker()
	observed := 0
	observe := func(catalog []foundation.StockCatalogEntry, err error) {
		observed++
		if err != nil {
			health.failure("sina", err)
			return
		}
		for _, item := range catalog {
			health.success(item.Meta)
		}
	}
	_, err := cache.load(context.Background(), provider, observe)
	if err != nil {
		t.Fatal(err)
	}
	first := sourceByID(t, health.snapshot(time.Now()), "sina")
	_, err = cache.load(context.Background(), provider, observe)
	second := sourceByID(t, health.snapshot(time.Now()), "sina")
	if err != nil || observed != 1 || first.CheckedAt == nil || second.CheckedAt == nil || !second.CheckedAt.Equal(*first.CheckedAt) {
		t.Fatalf("cache renewed observation: calls=%d first=%+v second=%+v err=%v", observed, first, second, err)
	}
	cache.snapshot.expiresAt = time.Now().Add(-time.Second)
	provider.fail = true
	data, err := cache.load(context.Background(), provider, observe)
	entry := sourceByID(t, health.snapshot(time.Now()), "sina")
	if err != nil || !data.Stale || len(data.Stocks) != 1 || observed != 2 || entry.Status != "degraded" {
		t.Fatalf("stale failure concealed: data=%+v entry=%+v calls=%d err=%v", data, entry, observed, err)
	}
}
