package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type failingCachedIndexOverview struct {
	fakeMarketOverviewProvider
	started chan struct{}
	release chan struct{}
}

func (p *failingCachedIndexOverview) MarketIndexSeries(ctx context.Context, _ string, _ string, _ int) (foundation.MarketIndexSeries, error) {
	p.started <- struct{}{}
	select {
	case <-p.release:
		return foundation.MarketIndexSeries{}, fmt.Errorf("offline refresh failure")
	case <-ctx.Done():
		return foundation.MarketIndexSeries{}, ctx.Err()
	}
}

func TestIndexStaleConcurrentResponsesDoNotMutateCachedSnapshot(t *testing.T) {
	provider := &failingCachedIndexOverview{started: make(chan struct{}, 2), release: make(chan struct{})}
	server := NewServer(Config{MarketOverview: provider})
	defer server.Close()
	fetchedAt := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	firstMeta := foundation.SourceMeta{Source: "tencent:index-kline", FetchedAt: fetchedAt, FieldsKnown: true, AvailableFields: []string{"open", "high", "low", "close", "volume"}, BasisID: "tencent:index:none", InstrumentID: "sse", Period: "day", TimeZone: "UTC", EffectiveAdjustment: "none", VolumeUnit: "provider_index_volume"}
	latestMeta := firstMeta
	latestMeta.AvailableFields = append(append([]string(nil), firstMeta.AvailableFields...), "change_percent")
	original := foundation.MarketIndexSeries{
		Index: foundation.MarketIndexSnapshot{ID: "sse", Price: 110, Meta: latestMeta},
		Lines: []foundation.KLine{
			{Symbol: "sse", Time: fetchedAt.AddDate(0, 0, -1), Open: 100, High: 101, Low: 99, Close: 100, Volume: 10, Meta: firstMeta},
			{Symbol: "sse", Time: fetchedAt, Open: 110, High: 111, Low: 109, Close: 110, Volume: 20, ChangePercent: 10, Meta: latestMeta},
		},
		Meta: firstMeta,
	}
	key := "index-series:sse:day:2"
	server.marketSnapshots.ttl = -time.Second
	server.marketSnapshots.store(key, original, original.Meta)
	before, _ := server.marketSnapshots.any(key)
	beforeJSON, err := json.Marshal(before.value)
	if err != nil {
		t.Fatal(err)
	}
	responses := make(chan *httptest.ResponseRecorder, 2)
	var requests sync.WaitGroup
	for i := 0; i < 2; i++ {
		requests.Add(1)
		go func() {
			defer requests.Done()
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/market/index-series?id=sse&period=day&limit=2", nil))
			responses <- rec
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-provider.started:
		case <-time.After(5 * time.Second):
			close(provider.release)
			requests.Wait()
			t.Fatal("concurrent refresh requests did not reach loader")
		}
	}
	// A concurrent cached-value reader makes accidental shared writes visible to
	// race-enabled runs. Reads neither renew expiry nor annotate the snapshot.
	readerDone := make(chan error, 1)
	go func() {
		<-provider.release
		for i := 0; i < 200; i++ {
			item, ok := server.marketSnapshots.any(key)
			if !ok {
				readerDone <- fmt.Errorf("snapshot disappeared")
				return
			}
			data, err := json.Marshal(item.value)
			if err != nil {
				readerDone <- err
				return
			}
			if !bytes.Equal(data, beforeJSON) {
				readerDone <- fmt.Errorf("shared cache changed during concurrent stale responses")
				return
			}
		}
		readerDone <- nil
	}()
	close(provider.release)
	requests.Wait()
	close(responses)
	if err := <-readerDone; err != nil {
		t.Error(err)
	}
	for rec := range responses {
		var result struct {
			Data foundation.MarketIndexSeries `json:"data"`
			Meta foundation.SourceMeta        `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusOK || len(result.Data.Lines) != 2 || !result.Meta.Stale || !result.Data.Meta.Stale || !result.Data.Index.Meta.Stale || !strings.Contains(result.Meta.FallbackReason, "offline refresh failure") {
			t.Fatalf("stale response status=%d result=%+v", rec.Code, result)
		}
		for i, line := range result.Data.Lines {
			if !line.Meta.Stale || line.Meta.FallbackReason != result.Meta.FallbackReason || line.Meta.BasisID != firstMeta.BasisID || line.Meta.VolumeUnit != firstMeta.VolumeUnit || !line.Meta.FetchedAt.Equal(fetchedAt) || !line.Meta.FieldsKnown {
				t.Fatalf("lost row metadata: %+v", line)
			}
			if foundation.FieldAvailable(line.Meta, "change_percent") != (i == 1) {
				t.Fatalf("row %d change mask changed: %+v", i, line.Meta)
			}
		}
	}
	after, ok := server.marketSnapshots.any(key)
	if !ok {
		t.Fatal("cached snapshot disappeared")
	}
	afterJSON, err := json.Marshal(after.value)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeJSON, afterJSON) || after.meta.Stale || after.meta.FallbackReason != "" || !after.expiresAt.Equal(before.expiresAt) {
		t.Fatalf("original snapshot mutated: before=%s after=%s beforeMeta=%+v afterMeta=%+v", beforeJSON, afterJSON, before.meta, after.meta)
	}
	originalJSON, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeJSON, originalJSON) {
		t.Fatalf("provider-owned original mutated: %s", originalJSON)
	}
}
