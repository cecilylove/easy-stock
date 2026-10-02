package duanxianxia

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

func TestFetchLimitUpPoolDecryptsKaipanlaPayload(t *testing.T) {
	location := time.FixedZone("CST", 8*60*60)
	now := time.Date(2026, 8, 7, 11, 40, 0, 0, location)
	plain := []byte(`{"list":[["603629","利通电子",10.01,120000000,2,"09:31:02","算力租赁+云计算","6天5板",880000000,9200000000,"回封","4","10:22:08"]]}`)
	encrypted := encryptPoolFixture(t, plain)
	requests := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/vendor/stockdata/datasource.json":
			_, _ = w.Write([]byte(`{"data_url":"` + "http://" + r.Host + `"}`))
		case "/vendor/stockdata/ztpool.json":
			w.Header().Set("Last-Modified", "Fri, 07 Aug 2026 03:30:20 GMT")
			_, _ = w.Write([]byte(encrypted))
		default:
			http.NotFound(w, r)
		}
	}))
	defer remote.Close()

	client := NewClient(ClientConfig{BaseURL: remote.URL, Now: func() time.Time { return now }})
	snapshot, err := client.FetchLimitUpPool(context.Background())
	if err != nil {
		t.Fatalf("FetchLimitUpPool: %v", err)
	}
	if requests != 2 || snapshot.TradeDate != "2026-08-07" || len(snapshot.Events) != 1 {
		t.Fatalf("unexpected snapshot: requests=%d snapshot=%+v", requests, snapshot)
	}
	event := snapshot.Events[0]
	if event.Symbol != "603629.SH" || event.Streak != 4 || event.Days != 6 || event.Count != 5 || event.BoardType != "回封" {
		t.Fatalf("unexpected event: %+v", event)
	}
	if len(event.Concepts) != 2 || event.Concepts[0] != "算力租赁" || event.Meta.Source != "duanxianxia:kaipanla-limit-up" {
		t.Fatalf("unexpected concepts/meta: %+v", event)
	}
}

func TestStockThemesReturnsPoolAndLeaderAttributionsSeparately(t *testing.T) {
	store, err := OpenStore("")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	location := time.FixedZone("CST", 8*60*60)
	tradeDate := time.Date(2026, 8, 7, 0, 0, 0, 0, location)
	if err := store.SaveLimitUpSuccess(context.Background(), LimitUpPoolSnapshot{
		ID: "pool", TradeDate: "2026-08-07", FetchedAt: tradeDate.Add(10 * time.Hour),
		Events: []foundation.LimitUpEvent{{
			Symbol: "003032.SZ", Name: "传智教育", Date: tradeDate,
			Concepts: []string{"机器人概念", "职业教育"},
			Meta:     foundation.SourceMeta{Source: "duanxianxia:kaipanla-limit-up"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSuccess(context.Background(), Snapshot{
		ID: "themes", TradeDate: "2026-08-07", FetchedAt: tradeDate.Add(10 * time.Hour),
		Themes: []Theme{{
			Name: "机器人概念", Rank: 2, LeadersLoaded: true,
			Leaders: []Leader{{Rank: 2, Role: "龙二", Symbol: "003032.SZ", Name: "传智教育"}},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	now := tradeDate.Add(11 * time.Hour)
	if allowed, _, err := store.TryBegin(context.Background(), now, 5*time.Minute); err != nil || !allowed {
		t.Fatalf("reserve refresh gate: allowed=%v err=%v", allowed, err)
	}
	provider := NewLimitUpProvider(NewService(nil, store, ServiceConfig{Now: func() time.Time { return now }}), nil)
	items, err := provider.StockThemes(context.Background(), "003032.SZ", 40)
	if err != nil {
		t.Fatal(err)
	}
	bySource := map[string]foundation.StockThemeAttribution{}
	for _, item := range items {
		bySource[item.Source] = item
	}
	pool := bySource["duanxianxia:kaipanla-limit-up"]
	leader := bySource[kaipanlaThemeLeaderSource]
	if pool.Theme != "机器人概念" || len(pool.Concepts) != 2 {
		t.Fatalf("pool attribution missing: %+v", items)
	}
	if leader.Theme != "机器人概念" || leader.Role != "龙二" {
		t.Fatalf("leader attribution missing: %+v", items)
	}
}

func encryptPoolFixture(t *testing.T, plain []byte) string {
	t.Helper()
	block, err := aes.NewCipher(poolCipherKey)
	if err != nil {
		t.Fatal(err)
	}
	padding := block.BlockSize() - len(plain)%block.BlockSize()
	padded := append(append([]byte(nil), plain...), make([]byte, padding)...)
	for index := len(padded) - padding; index < len(padded); index++ {
		padded[index] = byte(padding)
	}
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, poolCipherIV).CryptBlocks(ciphertext, padded)
	return base64.StdEncoding.EncodeToString(ciphertext)
}
