package service

import (
	"context"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
)

type replacementThemes struct {
	snapshot foundation.ThemeSnapshot
	pool     foundation.LimitUpPoolSnapshot
	calls    int
}

func (p *replacementThemes) Fetch(context.Context, int) (foundation.ThemeSnapshot, error) {
	p.calls++
	return p.snapshot, nil
}
func (p *replacementThemes) FetchLimitUpPool(context.Context) (foundation.LimitUpPoolSnapshot, error) {
	p.calls++
	return p.pool, nil
}

func TestThemeReplacementIdentityAndTradingDate(t *testing.T) {
	now := time.Now()
	provider := &replacementThemes{snapshot: foundation.ThemeSnapshot{TradeDate: "2026-10-01", FetchedAt: now, Themes: []foundation.ThemeSnapshotItem{{Code: "native", Name: "通信", LeadersLoaded: true, Leaders: []foundation.ThemeLeader{{Symbol: "600001.SH", Role: "龙一"}}}}}, pool: foundation.LimitUpPoolSnapshot{TradeDate: "2026-10-01", FetchedAt: now, Events: []foundation.LimitUpEvent{{Symbol: "600001.SH"}}}}
	access := NewThemes("replacement", provider)
	snapshot, err := access.Fetch(context.Background(), 3)
	if err != nil || snapshot.Meta.Source != "replacement:theme" || snapshot.Meta.Provider != "replacement" || snapshot.ID == "" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	pool, err := access.FetchLimitUpPool(context.Background())
	if err != nil || pool.Events[0].Meta.Source != "replacement:limit-up" {
		t.Fatalf("pool=%+v err=%v", pool, err)
	}
	if provider.pool.Events[0].Meta.Source != "" {
		t.Fatal("identity projection mutated the supplier's retained slice")
	}
	events := applyKaipanlaThemeLeaders([]foundation.LimitUpEvent{{Symbol: "600001.SH", Date: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}}, []foundation.ThemeSnapshot{snapshot})
	if events[0].ThemeSource != "replacement:theme-leader" {
		t.Fatalf("old supplier attribution leaked: %+v", events)
	}
	provider.snapshot.Meta.Source = "duanxianxia:kaipanla"
	if _, err := access.Fetch(context.Background(), 3); contracts.Kind(err) != contracts.InvalidResponse {
		t.Fatalf("mismatched supplier accepted: %v", err)
	}
	provider.snapshot.Meta.Source = ""
	provider.snapshot.TradeDate = ""
	if _, err := access.Fetch(context.Background(), 3); contracts.Kind(err) != contracts.InvalidResponse {
		t.Fatalf("missing trading date accepted: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := provider.calls
	if _, err := access.Fetch(ctx, 3); contracts.Kind(err) != contracts.Canceled || provider.calls != before {
		t.Fatalf("canceled request fetched: calls=%d err=%v", provider.calls, err)
	}
}
