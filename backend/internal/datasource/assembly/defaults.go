// Package assembly is the composition root for implemented public adapters.
// It returns typed neutral capabilities, without importing HTTP or business.
package assembly

import (
	"context"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/cls"
	"easy-stock/backend/internal/providers/duanxianxia"
	"easy-stock/backend/internal/providers/eastmoney"
	"easy-stock/backend/internal/providers/futuresposition"
	"easy-stock/backend/internal/providers/hotstock"
	"easy-stock/backend/internal/providers/sina"
	"easy-stock/backend/internal/providers/tencent"
	"easy-stock/backend/internal/providers/ths"
)

func Default(baseURL string) *registry.Registry {
	em, sn, qq := eastmoney.NewClient(), sina.NewClient(), tencent.NewClient()
	caps := map[string]registry.Capabilities{
		"duanxianxia": {Theme: duanxianxia.NewClient(duanxianxia.ClientConfig{BaseURL: baseURL})},
		"eastmoney":   {Auction: em, Industry: em, FundFlow: em, Margin: em, Billboard: em, Announcements: em, Reports: em, Directory: em, ProbeDirectory: freshDirectory{}, Business: em, Fundamentals: em, LimitUp: em, Pools: em, Boards: em, HotRank: hotstock.NewEastMoneyRankClient(), FuturesTrend: futuresposition.NewHistoryClient()},
		"sina":        {Realtime: sn, KLine: sn, Intraday: sn, HistoryIntraday: sina.NewHistoryIntradayClient(sn), FundFlow: sn, Directory: sn, Business: sina.NewBusinessClient(sina.BusinessConfig{}), Fundamentals: sina.NewFundamentalsClient(sina.FundamentalsConfig{})},
		"tencent":     {Index: qq, KLine: tencent.NewPriceKLineClient(qq), AdjustedKLine: tencent.NewStockKLineClient(qq), Industry: qq, BoardMembers: qq, USSector: qq},
		"cls":         {News: cls.NewClient()},
		"ths":         {HotRank: hotstock.NewTHSRankClient(), BillboardLabels: ths.NewBillboardLabelClient()},
	}
	exchange := futuresposition.NewExchangeClient()
	caps["cffex"] = registry.Capabilities{FuturesSnapshot: exchange, FuturesMembers: exchange, FuturesConsensus: exchange}
	entries := []registry.Entry{}
	for _, descriptor := range registry.DefaultDescriptors() {
		entries = append(entries, registry.Entry{Descriptor: descriptor, Capabilities: caps[descriptor.ID]})
	}
	sources, err := registry.New(entries...)
	if err != nil {
		panic(err)
	}
	return sources
}

// Directory probes must bypass the adapter's six-hour catalog cache.
type freshDirectory struct{}

func (freshDirectory) StockCatalog(ctx context.Context) ([]foundation.StockCatalogEntry, error) {
	return eastmoney.NewClient().StockCatalog(ctx)
}
