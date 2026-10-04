package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/assembly"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/stockanalysis"
)

// Explicit public source check: uses registered adapters plus HTTP handlers and
// research evidence projection, in-memory stores, no model or personal data.
func TestLiveDisclosureMigrationOfficialHTTPAndResearchEvidence(t *testing.T) {
	if os.Getenv("A_STOCK_LIVE_DISCLOSURE_TEST") != "1" {
		t.Skip("explicit public disclosure sample only")
	}
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	var mu sync.Mutex
	var next time.Time
	calls := []string{}
	http.DefaultTransport = companyLiveTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Host != "vip.stock.finance.sina.com.cn" && r.URL.Host != "stock.finance.sina.com.cn" {
			return nil, fmt.Errorf("unexpected fallback/external dependency %s", r.URL.Host)
		}
		if len(calls) >= 16 {
			return nil, fmt.Errorf("public disclosure request cap")
		}
		if delay := time.Until(next); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-r.Context().Done():
				timer.Stop()
				return nil, r.Context().Err()
			case <-timer.C:
			}
		}
		response, err := original.RoundTrip(r)
		next = time.Now().Add(time.Second)
		calls = append(calls, r.URL.String())
		return response, err
	})
	defaultSources := assembly.Default("")
	sinaSlot, _ := defaultSources.Lookup("sina")
	sources, err := registry.New(sinaSlot)
	if err != nil {
		t.Fatal(err)
	}
	routes := assembly.Routes{Announcements: "sina", Reports: "sina"}
	s := NewServer(Config{DataSources: sources, DataSourceRoutes: &routes, ReviewDBPath: ":memory:", StockResearchDBPath: ":memory:", PortfolioDBPath: ":memory:"})
	defer s.Close()
	if err := s.StartupError(); err != nil {
		t.Fatal(err)
	}
	output := []map[string]any{}
	for _, sample := range []struct{ path, source, kind string }{{"/api/v1/research/announcements?symbol=600519.SH&limit=1", "sina:announcements", "announcement"}, {"/api/v1/research/institution-reports?symbol=600519.SH&limit=1", "sina:reports", "opinion"}, {"/api/v1/research/industries?limit=1", "sina:reports", "opinion"}} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, sample.path, nil).WithContext(ctx)
		s.ServeHTTP(rec, req)
		cancel()
		if rec.Code != 200 {
			t.Fatalf("%s HTTP %d %s", sample.path, rec.Code, rec.Body.String())
		}
		var payload struct {
			Data []foundation.MarketResearchItem `json:"data"`
			Meta foundation.SourceMeta           `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Data) != 1 || payload.Meta.Source != sample.source || payload.Data[0].Meta.Source != sample.source {
			t.Fatalf("route not linked %+v", payload)
		}
		item := payload.Data[0]
		if item.Title == "" || item.PublishedAt.IsZero() || item.URL == "" || !item.Meta.FieldsKnown {
			t.Fatalf("required source fields missing %+v", item)
		}
		evidence := stockanalysis.ResearchItemSource(item, sample.kind, time.Now())
		if evidence.Provider != sample.source || evidence.URL != item.URL || !evidence.PublishedAt.Equal(item.PublishedAt) {
			t.Fatalf("research evidence lost identity %+v", evidence)
		}
		if item.ContentStatus != "available" && item.ContentStatus != "truncated" {
			t.Fatalf("official detail not readable %+v", item)
		}
		output = append(output, map[string]any{"path": sample.path, "item": item, "meta": payload.Meta, "evidence": evidence})
		t.Logf("%s %s content=%s/%s coverage=%s title=%s", sample.path, sample.source, item.ContentStatus, item.ContentScope, payload.Meta.QueryCoverage, item.Title)
	}
	if dir := os.Getenv("A_STOCK_DISCLOSURE_EVIDENCE_DIR"); dir != "" {
		data, _ := json.MarshalIndent(map[string]any{"at": time.Now(), "scope": "registered Sina adapters + actual market HTTP + research source projection, no AI/PDF/download/personal stores; fallback forbidden in this isolated check", "samples": output, "requests": calls}, "", "  ")
		if err := os.WriteFile(dir+"/live-samples.json", data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
