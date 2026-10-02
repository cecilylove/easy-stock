package httpapi

import (
	"context"
	"log"
	"net/http"

	"easy-stock/backend/internal/appsettings"
	"easy-stock/backend/internal/datasource/assembly"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/datasource/service"
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/hermes"
	"easy-stock/backend/internal/marketemotion"
	"easy-stock/backend/internal/methodology"
	"easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/review"
	"easy-stock/backend/internal/stockanalysis"
	"easy-stock/backend/internal/strategy/inflection"
)

type RealtimeProvider = contracts.RealtimeProvider

type AuctionProvider = contracts.AuctionProvider

type KLineProvider = contracts.KLineProvider

type HistoryIntradayProvider = contracts.HistoryIntradayProvider

// AdjustedKLineProvider promises only its own supplier's adjustment convention.
type AdjustedKLineProvider = contracts.AdjustedKLineProvider

type NewsProvider = contracts.NewsProvider

type SectorMapProvider interface {
	Build(ctx context.Context, themeID string) (foundation.SectorMap, error)
}

type SnapshotSectorMapProvider interface {
	BuildSnapshot(ctx context.Context, themeID string, snapshotID string) (foundation.SectorMap, error)
}

type ThemeOverviewProvider interface {
	Overviews(ctx context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, error)
}

type ThemeRadarFallback interface {
	SectorMapProvider
	ThemeOverviewProvider
}

type LimitUpProvider = contracts.LimitUpProvider

type StockThemeAttributionProvider = contracts.StockThemeAttributionProvider

type MarketPoolProvider = contracts.MarketPoolProvider

type StockConceptProvider interface {
	StockCatalog(ctx context.Context) ([]foundation.StockCatalogEntry, error)
}

type StockBusinessProfileProvider interface {
	StockBusinessProfile(ctx context.Context, symbol string) (foundation.StockBusinessProfile, error)
	StockFundamentals(ctx context.Context, symbol string) (foundation.StockFundamentals, error)
}

type StockDirectoryProvider = contracts.StockDirectoryProvider

type HotStockProvider = contracts.HotStockProvider

type FuturesPositionProvider = contracts.FuturesPositionProvider

type MarketOverviewProvider = contracts.MarketOverviewProvider

type InflectionEvaluator interface {
	Evaluate(request inflection.EvaluationRequest) (inflection.Evaluation, error)
}

type ReviewImporter interface {
	ImportURL(ctx context.Context, rawURL string) (review.Post, error)
}

type Config struct {
	DataSources          *registry.Registry
	ContentSources       *registry.Registry
	ArchiveSourceID      string
	KnowledgeSourceID    string
	DataSourceRoutes     *assembly.Routes
	MarketCapabilities   *service.MarketConfig
	Token                string
	AllowedOrigins       []string
	EnforceLoopbackHost  bool
	Realtime             RealtimeProvider
	Auction              AuctionProvider
	KLinePrimary         KLineProvider
	KLineFallback        KLineProvider
	KLineStrictTencent   AdjustedKLineProvider
	Intraday             KLineProvider // Direct recent minute samples, without latest-day filtering.
	HistoryIntraday      HistoryIntradayProvider
	News                 NewsProvider
	SectorMap            SectorMapProvider
	ThemeOverview        ThemeOverviewProvider
	ThemeRadarFallback   ThemeRadarFallback
	LimitUp              LimitUpProvider
	MarketPools          MarketPoolProvider
	StockConcept         StockConceptProvider
	StockBusiness        StockBusinessProfileProvider
	StockDirectory       StockDirectoryProvider
	HotStocks            HotStockProvider
	FuturesPosition      FuturesPositionProvider
	MarketOverview       MarketOverviewProvider
	SourceProbeProviders *SourceProbeProviders
	Inflection           InflectionEvaluator
	ReviewDBPath         string
	PortfolioDBPath      string
	StockResearchDBPath  string
	StockResearchStore   *stockanalysis.ResearchStore
	MarketEmotionDBPath  string
	ThemeRadarDBPath     string
	DuanxianxiaBaseURL   string
	WeChatAPIURL         string
	ReviewHTTP           *http.Client
	ReviewStore          *review.Store
	PortfolioStore       *portfolioinspection.Store
	MarketEmotionStore   *marketemotion.Store
	ReviewImporter       ReviewImporter
	SettingsPath         string
	SettingsStore        *appsettings.Store
	ReviewAutomation     *review.Automation
	RemoteDailyReviewURL string
	RemoteDailySync      *review.RemoteDailySync
	HermesGateway        hermes.Gateway
	MasteryLibrary       *methodology.Library
	Logger               *log.Logger
	StrictPersistence    bool
}

func normalizeConfig(value any) Config {
	switch cfg := value.(type) {
	case Config:
		return cfg
	case *Config:
		if cfg != nil {
			return *cfg
		}
	}
	return Config{}
}
