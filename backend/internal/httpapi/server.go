package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

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

	"easy-stock/backend/internal/providers/duanxianxia"

	"easy-stock/backend/internal/review"
	"easy-stock/backend/internal/runtimelog"
	"easy-stock/backend/internal/sector"
	"easy-stock/backend/internal/stockanalysis"
	"easy-stock/backend/internal/strategy/inflection"
)

type Server struct {
	dataSources              *registry.Registry
	contentSources           *registry.Registry
	defaultStrictPriceSource string
	priceRoutes              []service.PriceRoute
	strictPriceSources       map[string]contracts.AdjustedKLineProvider
	ladderThemeAI            *ladderThemeAI
	mux                      *http.ServeMux
	token                    string
	allowedOrigins           []string
	enforceLoopbackHost      bool
	realtimeProvider         RealtimeProvider
	detailQuotes             *detailPollCache[[]foundation.Quote]
	detailKLines             *detailPollCache[[]foundation.KLine]
	stockIntraday            *detailPollCache[stockIntradayData]
	intradayProvider         KLineProvider
	historyIntradayProvider  HistoryIntradayProvider
	intradaySourceID         string
	intradayContext          context.Context
	intradayCancel           context.CancelFunc
	detailAuctions           *detailPollCache[foundation.AuctionTrace]
	auctionProvider          AuctionProvider
	auctionSourceID          string
	realtimeSourceID         string
	kLinePrimary             KLineProvider
	kLinePrimarySourceID     string
	kLineFallback            KLineProvider
	kLineFallbackSourceID    string
	kLineStrictTencent       AdjustedKLineProvider
	kLineRoutes              *klineRouteState
	newsProvider             NewsProvider
	newsSourceID             string
	sectorMap                SectorMapProvider
	themeOverview            ThemeOverviewProvider
	limitUpProvider          LimitUpProvider
	marketPools              MarketPoolProvider
	stockConcepts            StockConceptProvider
	stockBusiness            StockBusinessProfileProvider
	stockDirectory           StockDirectoryProvider
	stockDirectorySourceID   string
	hotStockProvider         HotStockProvider
	futuresPosition          FuturesPositionProvider
	futuresSourceID          string
	futuresExchangeSourceID  string
	futuresMembersSourceID   string
	futuresConsensusSourceID string
	marketOverview           MarketOverviewProvider
	marketFailureSourceID    string
	marketIndexSourceID      string
	marketIndustrySourceID   string
	marketFlowSourceID       string
	inflection               InflectionEvaluator
	themeSnapshots           *themeSnapshotCache
	limitUpSnapshots         *limitUpLadderCache
	limitUpProgress          *shortTermCache[limitUpLadderData]
	emotionProgress          *shortTermCache[marketemotion.History]
	stockDirectories         *stockDirectoryCache
	hotStockRanks            *hotStockRankCache
	marketSnapshots          *marketOverviewCache
	sourceHealth             *sourceHealthTracker
	sourceProbes             *sourceProbeTracker
	marketEmotion            *marketEmotionEngine
	marketEmotionIntraday    *marketEmotionIntradayCache
	reviewStore              *review.Store
	portfolioStore           *portfolioinspection.Store
	portfolioInspection      *portfolioinspection.Service
	portfolioExpectation     *portfolioinspection.ExpectationService
	stockResearchStore       *stockanalysis.ResearchStore
	stockResearch            *stockanalysis.ResearchService
	reviewImporter           ReviewImporter
	wechatAPIURL             string
	settingsStore            *appsettings.Store
	reviewAutomation         *review.Automation
	remoteDailySync          *review.RemoteDailySync
	hermesGateway            hermes.Gateway
	usageGateway             hermes.Gateway
	masteryLibrary           *methodology.Library
	marketEmotionStore       *marketemotion.Store
	themeRadarStore          *duanxianxia.Store
	themeProgress            *themeProgressCache
	startupError             error
	logger                   *log.Logger
	tokenUsage               *tokenUsageStore
}

func NewServer(config any) *Server {
	cfg := normalizeConfig(config)
	intradayContext, intradayCancel := context.WithCancel(context.Background())
	tokenUsage := newTokenUsageStore(cfg.SettingsPath)
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	var startupErrors []error
	sources := cfg.DataSources
	if sources == nil {
		sources = assembly.Default(cfg.DuanxianxiaBaseURL)
	}
	routes := assembly.DefaultRoutes()
	if cfg.DataSourceRoutes != nil {
		routes = *cfg.DataSourceRoutes
	}
	if err := routes.Validate(sources); err != nil {
		startupErrors = append(startupErrors, err)
	}
	capability := func(id string) registry.Capabilities { return assembly.Capabilities(sources, id) }
	access := func(id string) *service.Access { return service.NewAccess(id, capability(id)) }
	intradaySourceID := ""
	if cfg.Intraday == nil && cfg.HistoryIntraday == nil {
		cfg.HistoryIntraday = access(routes.HistoryIntraday)
	}
	if cfg.Intraday == nil {
		cap := capability(routes.Intraday)
		cap.KLine = cap.Intraday
		cfg.Intraday = service.NewAccess(routes.Intraday, cap)
		intradaySourceID = routes.Intraday
	}
	realtimeSourceID, primarySourceID, fallbackSourceID, newsSourceID := "", "", "", ""
	if cfg.Realtime == nil {
		cfg.Realtime = access(routes.Realtime)
		realtimeSourceID = routes.Realtime
	}
	auctionSourceID := ""
	if cfg.Auction == nil {
		cfg.Auction = access(routes.Auction)
		auctionSourceID = routes.Auction
	}
	if cfg.KLineStrictTencent == nil {
		cfg.KLineStrictTencent = capability("tencent").AdjustedKLine
	}
	if cfg.DataSourceRoutes != nil {
		allowed := false
		for _, id := range routes.Strict {
			if id == "tencent" {
				allowed = true
			}
		}
		if !allowed {
			cfg.KLineStrictTencent = nil
		}
	}
	if cfg.KLinePrimary == nil && len(routes.KLine) > 0 {
		primarySourceID = routes.KLine[0]
		cfg.KLinePrimary = capability(primarySourceID).KLine
	}
	if cfg.KLineFallback == nil && len(routes.KLine) > 1 {
		fallbackSourceID = routes.KLine[1]
		cfg.KLineFallback = capability(fallbackSourceID).KLine
	}
	if cfg.News == nil {
		cfg.News = capability(routes.News).News
		newsSourceID = routes.News
	}
	kaipanlaClient := capability(routes.Theme).Theme
	if kaipanlaClient != nil {
		kaipanlaClient = service.NewThemes(routes.Theme, kaipanlaClient)
	}
	var kaipanlaService *duanxianxia.Service
	var radarStore *duanxianxia.Store
	if strings.TrimSpace(cfg.ThemeRadarDBPath) != "" {
		if store, err := duanxianxia.OpenStore(cfg.ThemeRadarDBPath); err == nil {
			radarStore = store
			if kaipanlaClient != nil {
				kaipanlaService = duanxianxia.NewService(kaipanlaClient, store, duanxianxia.ServiceConfig{
					SourceID:         routes.Theme,
					RefreshInterval:  5 * time.Minute,
					LeaderThemeLimit: 3,
				})
			}
		} else if cfg.StrictPersistence {
			startupErrors = append(startupErrors, fmt.Errorf("open theme radar database: %w", err))
		}
	}
	usingDefaultLimitUp := cfg.LimitUp == nil
	if usingDefaultLimitUp {
		if kaipanlaService != nil {
			cfg.LimitUp = service.NewLimitUpProvider(kaipanlaService, access(routes.LimitUp))
		} else {
			cfg.LimitUp = access(routes.LimitUp)
		}
	}
	if cfg.MarketPools == nil {
		cfg.MarketPools = access(routes.Pools)
	}
	if cfg.StockConcept == nil && usingDefaultLimitUp {
		cfg.StockConcept = access(routes.Directory)
	}
	if cfg.StockBusiness == nil {
		businessCap := capability(routes.Business)
		businessCap.Fundamentals = capability(routes.Fundamentals).Fundamentals
		cfg.StockBusiness = service.NewAccess(routes.Business, businessCap)
	}
	stockDirectorySourceID := ""
	if cfg.StockDirectory == nil {
		cfg.StockDirectory = access(routes.Directory)
		stockDirectorySourceID = routes.Directory
	}
	marketFailureSourceID := ""
	marketIndexSourceID, marketIndustrySourceID, marketFlowSourceID := "", "", ""
	if cfg.MarketOverview == nil {
		marketConfig := service.MarketConfig{Index: capability(routes.Index).Index, Industry: capability(routes.Industry).Industry, IndustryFallback: capability(routes.IndustryFallback).Industry, FundFlow: capability(routes.FundFlow).FundFlow, FundFlowFallback: capability(routes.FundFlowFallback).FundFlow, Margin: capability(routes.Margin).Margin, Billboard: capability(routes.Billboard).Billboard, Announcements: capability(routes.Announcements).Announcements, Reports: capability(routes.Reports).Reports, USSector: capability(routes.USSector).USSector, USSectorFallback: capability(routes.USSectorFallback).USSector, MarginSourceID: routes.Margin, BillboardSourceID: routes.Billboard, AnnouncementsSourceID: routes.Announcements, ReportsSourceID: routes.Reports, USSectorSourceID: routes.USSector, USSectorFallbackSourceID: routes.USSectorFallback, IndexSourceID: routes.Index, IndustrySourceID: routes.Industry, IndustryFallbackSourceID: routes.IndustryFallback, FundFlowSourceID: routes.FundFlow, FundFlowFallbackSourceID: routes.FundFlowFallback}
		if cfg.MarketCapabilities != nil {
			marketConfig = *cfg.MarketCapabilities
		}
		cfg.MarketOverview = service.WithBillboardLabels(service.NewMarket(marketConfig), capability(routes.BillboardLabels).BillboardLabels, routes.BillboardLabels)
		// Registered capability services emit their own source observations.
		// A missing capability must not inherit an unrelated supplier identity.
		marketIndexSourceID, marketIndustrySourceID, marketFlowSourceID = marketConfig.IndexSourceID, marketConfig.IndustrySourceID, marketConfig.FundFlowSourceID
	}
	if cfg.SectorMap == nil {
		mapper := sector.NewMapper(
			access(routes.Boards),
			sector.WithQuoteProvider(cfg.Realtime),
			sector.WithLimitUpProvider(cfg.LimitUp),
			sector.WithStockCatalogProvider(cfg.StockDirectory),
		)
		var defaultSectorMap SectorMapProvider = mapper
		var defaultThemeOverview ThemeOverviewProvider = mapper
		var radarFallback sector.RadarFallback = mapper
		if cfg.ThemeRadarFallback != nil {
			radarFallback = cfg.ThemeRadarFallback
		}
		var radarSource sector.RadarSnapshotSource
		if kaipanlaService != nil {
			radarSource = kaipanlaService
		}
		memberSources := []contracts.BoardMemberProvider{}
		for _, id := range routes.BoardMembers {
			if p := capability(id).BoardMembers; p != nil {
				memberSources = append(memberSources, p)
			}
		}
		radar := sector.NewRadarProvider(radarSource, radarFallback, cfg.Realtime, sector.RadarProviderConfig{
			IndustryMomentum:  cfg.MarketOverview,
			BoardMembers:      service.NewBoardMembers(memberSources...),
			FallbackFillLimit: 16,
		})
		defaultSectorMap = radar
		defaultThemeOverview = radar
		cfg.SectorMap = defaultSectorMap
		if cfg.ThemeOverview == nil {
			cfg.ThemeOverview = defaultThemeOverview
		}
	}
	if cfg.Inflection == nil {
		cfg.Inflection = inflection.NewEngine(inflection.DefaultConfig())
	}
	if cfg.ReviewStore == nil {
		store, err := review.OpenStore(cfg.ReviewDBPath)
		if err == nil {
			cfg.ReviewStore = store
		} else if cfg.StrictPersistence {
			startupErrors = append(startupErrors, fmt.Errorf("open review database: %w", err))
			cfg.ReviewStore, _ = review.OpenStore(":memory:")
		} else {
			cfg.ReviewStore, _ = review.OpenStore(":memory:")
		}
	}
	if cfg.MarketEmotionStore == nil {
		store, err := marketemotion.OpenStore(cfg.MarketEmotionDBPath)
		if err == nil {
			cfg.MarketEmotionStore = store
		} else if cfg.StrictPersistence {
			startupErrors = append(startupErrors, fmt.Errorf("open market emotion database: %w", err))
			cfg.MarketEmotionStore, _ = marketemotion.OpenStore("")
		} else {
			cfg.MarketEmotionStore, _ = marketemotion.OpenStore("")
		}
	}
	if cfg.PortfolioStore == nil {
		store, err := portfolioinspection.OpenStore(cfg.PortfolioDBPath)
		if err == nil {
			cfg.PortfolioStore = store
		} else if cfg.StrictPersistence {
			startupErrors = append(startupErrors, fmt.Errorf("open portfolio inspection database: %w", err))
			cfg.PortfolioStore, _ = portfolioinspection.OpenStore(":memory:")
		} else {
			cfg.PortfolioStore, _ = portfolioinspection.OpenStore(":memory:")
		}
	}
	if cfg.StockResearchStore == nil {
		store, err := stockanalysis.OpenResearchStore(cfg.StockResearchDBPath)
		if err != nil {
			if cfg.StrictPersistence {
				startupErrors = append(startupErrors, fmt.Errorf("open stock research database: %w", err))
			}
			store, _ = stockanalysis.OpenResearchStore(":memory:")
		}
		cfg.StockResearchStore = store
	}
	if cfg.ReviewHTTP == nil {
		cfg.ReviewHTTP = &http.Client{Timeout: 90 * time.Second}
	}
	if cfg.SettingsStore == nil {
		store, err := appsettings.Open(cfg.SettingsPath)
		if err == nil {
			cfg.SettingsStore = store
		} else if cfg.StrictPersistence {
			startupErrors = append(startupErrors, fmt.Errorf("open settings: %w", err))
			cfg.SettingsStore, _ = appsettings.Open("")
		} else {
			cfg.SettingsStore, _ = appsettings.Open("")
		}
	}
	if cfg.HotStocks == nil {
		rankSources := []contracts.HotRankProvider{}
		for _, id := range routes.HotRanks {
			if p := capability(id).HotRank; p != nil {
				rankSources = append(rankSources, p)
			}
		}
		cfg.HotStocks = service.NewHotRanks(rankSources...)
	}
	futuresSourceID := ""
	futuresExchangeSourceID := ""
	futuresMembersSourceID, futuresConsensusSourceID := "", ""
	if cfg.FuturesPosition == nil {
		cfg.FuturesPosition = service.NewFuturesCapabilities(service.FuturesConfig{
			HistoryID: routes.FuturesHistory, SnapshotID: routes.FuturesSnapshotSource(), MembersID: routes.FuturesMembersSource(), ConsensusID: routes.FuturesConsensusSource(),
			History: capability(routes.FuturesHistory).FuturesTrend, Snapshot: capability(routes.FuturesSnapshotSource()).FuturesSnapshot,
			Members: capability(routes.FuturesMembersSource()).FuturesMembers, Consensus: capability(routes.FuturesConsensusSource()).FuturesConsensus,
		})
		futuresSourceID = routes.FuturesHistory
		futuresExchangeSourceID = routes.FuturesSnapshotSource()
		futuresMembersSourceID, futuresConsensusSourceID = routes.FuturesMembersSource(), routes.FuturesConsensusSource()
	}
	probeProviders := cfg.SourceProbeProviders
	if probeProviders == nil {
		probeProviders = &SourceProbeProviders{
			Sina: capability("sina").Realtime, Tencent: capability("tencent").Index, CLS: capability("cls").News,
			EastMoney: capability("eastmoney").ProbeDirectory, THS: service.NewHotRanks(capability("ths").HotRank),
			CFFEX: service.NewFuturesCapabilities(service.FuturesConfig{SnapshotID: "cffex", Snapshot: capability("cffex").FuturesSnapshot}), Kaipanla: capability("duanxianxia").Theme,
		}
	}
	if cfg.HermesGateway != nil && (!cfg.StrictPersistence || len(startupErrors) == 0) {
		values := cfg.SettingsStore.Snapshot()
		var migratedKey *string
		if strings.TrimSpace(values.LLM.APIKey) != "" {
			key := strings.TrimSpace(values.LLM.APIKey)
			migratedKey = &key
			if updated, err := cfg.SettingsStore.Update(func(next *appsettings.Values) error {
				next.LLM.APIKey = ""
				return nil
			}); err == nil {
				values = updated
			}
		}
		if profileGateway, ok := cfg.HermesGateway.(hermes.ProfileGateway); ok {
			if migratedKey == nil {
				if key, err := cfg.HermesGateway.ModelAPIKey(); err == nil && strings.TrimSpace(key) != "" {
					migratedKey = &key
				}
			}
			_ = profileGateway.SyncLLMProfile(values.LLM, values.ActiveLLMProfileID, migratedKey)
		} else {
			_ = cfg.HermesGateway.SyncLLM(values.LLM, migratedKey)
		}
	}
	usageGateway := newTokenUsageGateway(cfg.HermesGateway, tokenUsage)
	contentSources := cfg.ContentSources
	if contentSources == nil {
		contentSources = assembly.Content(cfg.ReviewHTTP, cfg.WeChatAPIURL, cfg.RemoteDailyReviewURL)
	}
	for _, entry := range contentSources.Entries() {
		if _, exists := sources.Lookup(entry.Descriptor.ID); exists {
			startupErrors = append(startupErrors, fmt.Errorf("duplicate source across public and content registries: %s", entry.Descriptor.ID))
		}
	}
	if cfg.MasteryLibrary != nil && cfg.ContentSources != nil {
		knowledgeID := cfg.KnowledgeSourceID
		if knowledgeID == "" {
			knowledgeID = "githubknowledge"
		}
		cfg.MasteryLibrary.SetKnowledgeProvider(service.NewKnowledge(knowledgeID, assembly.Capabilities(contentSources, knowledgeID).Knowledge))
	}
	if cfg.ReviewImporter == nil {
		articleRoutes := []service.ArticleRoute{}
		for _, entry := range contentSources.Entries() {
			if entry.Descriptor.Enabled && entry.Descriptor.Implemented && entry.Capabilities.Article != nil {
				articleRoutes = append(articleRoutes, service.ArticleRoute{SourceID: entry.Descriptor.ID, Hosts: entry.Descriptor.ArticleHosts, Provider: entry.Capabilities.Article})
			}
		}
		cfg.ReviewImporter = review.NewImporterWithSource(service.NewArticleRoutes(articleRoutes...))
	}
	if cfg.ReviewAutomation == nil {
		cfg.ReviewAutomation = review.NewAutomation(cfg.ReviewStore, cfg.ReviewImporter, cfg.SettingsStore, cfg.ReviewHTTP, cfg.WeChatAPIURL, usageGateway)
		collections := service.NewCollections(contentSources)
		cfg.ReviewAutomation.SetCollectionSources(collections, collections, collections)
	}
	if dailyMarketProvider := newReviewDailyMarketProvider(cfg.MarketOverview); dailyMarketProvider != nil {
		cfg.ReviewAutomation.SetDailyMarketProvider(dailyMarketProvider)
	}
	if cfg.RemoteDailySync == nil {
		archiveID := cfg.ArchiveSourceID
		if archiveID == "" {
			archiveID = "official"
		}
		archiveCap := assembly.Capabilities(contentSources, archiveID)
		cfg.RemoteDailySync = review.NewRemoteDailySync(cfg.ReviewStore, review.RemoteDailySyncConfig{Provider: service.NewArchive(archiveID, archiveCap.Archive),
			BaseURL: cfg.RemoteDailyReviewURL,
			Client:  cfg.ReviewHTTP,
		})
	}
	s := &Server{
		dataSources: sources, contentSources: contentSources, defaultStrictPriceSource: routes.DefaultStrict,
		mux:                      http.NewServeMux(),
		token:                    cfg.Token,
		allowedOrigins:           append([]string(nil), cfg.AllowedOrigins...),
		enforceLoopbackHost:      cfg.EnforceLoopbackHost,
		realtimeProvider:         cfg.Realtime,
		detailQuotes:             newDetailPollCache[[]foundation.Quote](),
		detailKLines:             newDetailPollCache[[]foundation.KLine](),
		stockIntraday:            newDetailPollCache[stockIntradayData](),
		intradayProvider:         cfg.Intraday,
		historyIntradayProvider:  cfg.HistoryIntraday,
		intradaySourceID:         intradaySourceID,
		intradayContext:          intradayContext,
		intradayCancel:           intradayCancel,
		detailAuctions:           newDetailPollCache[foundation.AuctionTrace](),
		auctionProvider:          cfg.Auction,
		auctionSourceID:          auctionSourceID,
		realtimeSourceID:         realtimeSourceID,
		kLinePrimary:             cfg.KLinePrimary,
		kLinePrimarySourceID:     primarySourceID,
		kLineFallback:            cfg.KLineFallback,
		kLineFallbackSourceID:    fallbackSourceID,
		kLineStrictTencent:       cfg.KLineStrictTencent,
		kLineRoutes:              newKLineRouteState(),
		newsProvider:             cfg.News,
		newsSourceID:             newsSourceID,
		sectorMap:                cfg.SectorMap,
		themeOverview:            cfg.ThemeOverview,
		limitUpProvider:          cfg.LimitUp,
		marketPools:              cfg.MarketPools,
		stockConcepts:            cfg.StockConcept,
		stockBusiness:            cfg.StockBusiness,
		stockDirectory:           cfg.StockDirectory,
		stockDirectorySourceID:   stockDirectorySourceID,
		hotStockProvider:         cfg.HotStocks,
		futuresPosition:          cfg.FuturesPosition,
		futuresSourceID:          futuresSourceID,
		futuresExchangeSourceID:  futuresExchangeSourceID,
		futuresMembersSourceID:   futuresMembersSourceID,
		futuresConsensusSourceID: futuresConsensusSourceID,
		marketOverview:           cfg.MarketOverview,
		marketFailureSourceID:    marketFailureSourceID,
		marketIndexSourceID:      marketIndexSourceID,
		marketIndustrySourceID:   marketIndustrySourceID,
		marketFlowSourceID:       marketFlowSourceID,
		inflection:               cfg.Inflection,
		themeSnapshots:           newThemeSnapshotCache(30 * time.Second),
		themeProgress:            newThemeProgressCache(),
		limitUpSnapshots:         newLimitUpLadderCache(30 * time.Second),
		limitUpProgress:          &shortTermCache[limitUpLadderData]{},
		emotionProgress:          &shortTermCache[marketemotion.History]{},
		stockDirectories:         newStockDirectoryCache(6 * time.Hour),
		hotStockRanks:            newHotStockRankCache(2 * time.Minute),
		marketSnapshots:          newMarketOverviewCache(45 * time.Second),
		sourceHealth:             newSourceHealthTrackerFor(sources),
		sourceProbes:             newSourceProbeTrackerFor(sources, *probeProviders),
		marketEmotionIntraday:    newMarketEmotionIntradayCache(marketEmotionIntradayTTL),
		reviewStore:              cfg.ReviewStore,
		portfolioStore:           cfg.PortfolioStore,
		stockResearchStore:       cfg.StockResearchStore,
		reviewImporter:           cfg.ReviewImporter,
		wechatAPIURL:             strings.TrimSpace(cfg.WeChatAPIURL),
		settingsStore:            cfg.SettingsStore,
		ladderThemeAI:            newLadderThemeAI(cfg.SettingsPath),
		reviewAutomation:         cfg.ReviewAutomation,
		remoteDailySync:          cfg.RemoteDailySync,
		hermesGateway:            cfg.HermesGateway,
		usageGateway:             usageGateway,
		masteryLibrary:           cfg.MasteryLibrary,
		marketEmotionStore:       cfg.MarketEmotionStore,
		startupError:             errors.Join(startupErrors...),
		logger:                   cfg.Logger,
		tokenUsage:               tokenUsage,
	}
	s.newsProvider = service.NewNews(newsSourceID, cfg.News, s.sourceHealth.observe)
	if cfg.DataSourceRoutes != nil {
		for _, id := range routes.KLine {
			s.priceRoutes = append(s.priceRoutes, service.PriceRoute{SourceID: id, Provider: capability(id).KLine})
		}
		if len(routes.KLine) == 0 {
			s.priceRoutes = []service.PriceRoute{}
		}
		s.strictPriceSources = map[string]contracts.AdjustedKLineProvider{}
		for _, id := range routes.Strict {
			s.strictPriceSources[id] = capability(id).AdjustedKLine
		}
	}
	if radarStore != nil {
		s.themeRadarStore = radarStore
		if payload, err := s.themeRadarStore.LoadOverview(context.Background()); err == nil && len(payload) > 0 {
			var cached foundation.ThemeProgress
			if json.Unmarshal(payload, &cached) == nil {
				cached.Refreshing = false
				cached.Meta.Stale = true
				s.themeProgress.value = cached
			}
		}
	}
	if s.themeRadarStore != nil {
		if payload, err := s.themeRadarStore.LoadLadder(context.Background()); err == nil && len(payload) > 0 {
			var cached shortTermProgress[limitUpLadderData]
			if json.Unmarshal(payload, &cached) == nil && cached.Data != nil {
				cached.Refreshing, cached.Stale = false, true
				cached.Data.Meta.Stale = true
				if cached.Data.Intraday != nil {
					cached.Data.Intraday.Stale = true
				}
				s.limitUpProgress.value = cached
			}
		}
	}
	var emotionPrices KLineProvider
	if s.kLinePrimary != nil || len(s.priceRoutes) > 0 {
		emotionPrices = kLineAccess(s.loadKLine)
	}
	s.marketEmotion = newMarketEmotionEngine(
		cfg.MarketEmotionStore,
		s.limitUpProvider,
		s.marketPools,
		emotionPrices,
		nil,
		s.stockConcepts,
	)
	s.stockResearch = stockanalysis.NewResearchService(cfg.StockResearchStore, s.runStockResearch)
	s.portfolioInspection = portfolioinspection.NewService(cfg.PortfolioStore, usageGateway, s.analyzeStock, cfg.Logger, s.analyzeHoldingResearch)
	s.portfolioExpectation = portfolioinspection.NewExpectationService(cfg.PortfolioStore, cfg.ReviewStore, usageGateway, s.analyzeStock, cfg.Logger, s.analyzeHoldingResearch)
	s.routes()
	return s
}

func (s *Server) StartupError() error {
	if s == nil {
		return errors.New("server is nil")
	}
	return s.startupError
}

func (s *Server) Close() error {
	if s != nil && s.themeProgress != nil {
		s.themeProgress.close()
	}
	if s == nil {
		return nil
	}
	if s.sourceProbes != nil {
		s.sourceProbes.close()
	}
	s.closeStockIntraday()
	if s.limitUpProgress != nil {
		s.limitUpProgress.close()
	}
	if s.emotionProgress != nil {
		s.emotionProgress.close()
	}
	var closeErrors []error
	if s.stockResearch != nil {
		s.stockResearch.Close()
	}
	if s.stockResearchStore != nil {
		closeErrors = append(closeErrors, s.stockResearchStore.Close())
	}
	if s.reviewStore != nil {
		closeErrors = append(closeErrors, s.reviewStore.Close())
	}
	if s.marketEmotionStore != nil {
		closeErrors = append(closeErrors, s.marketEmotionStore.Close())
	}
	if s.portfolioStore != nil {
		closeErrors = append(closeErrors, s.portfolioStore.Close())
	}
	if s.themeRadarStore != nil {
		closeErrors = append(closeErrors, s.themeRadarStore.Close())
	}
	return errors.Join(closeErrors...)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	requestID := runtimeRequestID(r)
	loggedWriter := &requestLogWriter{ResponseWriter: w}
	loggedWriter.Header().Set("X-Request-ID", requestID)
	defer s.logRequest(r, loggedWriter, requestID, startedAt)

	if !s.browserRequestAllowed(r) {
		writeError(loggedWriter, http.StatusForbidden, "untrusted browser origin or host")
		return
	}
	s.withCORS(loggedWriter, r)
	if r.Method == http.MethodOptions {
		loggedWriter.WriteHeader(http.StatusNoContent)
		return
	}
	if !s.authorized(r) {
		writeError(loggedWriter, http.StatusUnauthorized, "unauthorized")
		return
	}
	s.mux.ServeHTTP(loggedWriter, r)
}

func (s *Server) RunReviewScheduler(ctx context.Context) {
	if s.reviewAutomation != nil {
		s.logSchedulerLifecycle(ctx, "reviews", "subscription_sync", func() {
			s.reviewAutomation.RunScheduler(ctx, s.logger)
		})
	}
}

func (s *Server) RunRemoteDailyReviewScheduler(ctx context.Context) {
	if s.remoteDailySync != nil {
		s.logSchedulerLifecycle(ctx, "reviews", "remote_daily_sync", func() {
			s.remoteDailySync.Run(ctx, s.logger)
		})
	}
}

func (s *Server) RunMarketEmotionScheduler(ctx context.Context) {
	if s.marketEmotion != nil {
		s.logSchedulerLifecycle(ctx, "short-term", "market_emotion", func() {
			s.marketEmotion.runScheduler(ctx, s.logger)
		})
	}
}

func (s *Server) RunMasteryScheduler(ctx context.Context) {
	if s.masteryLibrary == nil {
		return
	}
	if s.logger != nil {
		s.logger.Printf("level=info event=scheduler_start feature=trading-mastery task=mastery_snapshot")
		defer s.logger.Printf("level=info event=scheduler_stop feature=trading-mastery task=mastery_snapshot")
	}
	s.refreshMasterySnapshot(ctx, false)
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshMasterySnapshot(ctx, true)
		}
	}
}

func (s *Server) logSchedulerLifecycle(_ context.Context, feature, task string, run func()) {
	if s.logger != nil {
		s.logger.Printf("level=info event=scheduler_start feature=%s task=%s", feature, task)
		defer s.logger.Printf("level=info event=scheduler_stop feature=%s task=%s", feature, task)
	}
	run()
}

func (s *Server) refreshMasterySnapshot(ctx context.Context, force bool) {
	startedAt := time.Now()
	_, err := s.masteryLibrary.Snapshot(ctx, force)
	if s.logger == nil {
		return
	}
	if err != nil {
		s.logger.Printf("level=warn event=scheduler_error feature=trading-mastery task=mastery_snapshot force=%t duration_ms=%d error=%q", force, time.Since(startedAt).Milliseconds(), runtimelog.Redact(err.Error()))
		return
	}
	s.logger.Printf("level=info event=scheduler_run feature=trading-mastery task=mastery_snapshot force=%t duration_ms=%d", force, time.Since(startedAt).Milliseconds())
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", s.health)
	s.mux.HandleFunc("GET /api/v1/sources", s.sources)
	s.mux.HandleFunc("POST /api/v1/sources/check", s.checkSources)
	s.mux.HandleFunc("GET /api/v1/quotes/realtime", s.realtime)
	s.mux.HandleFunc("GET /api/v1/quotes/auction", s.auctionTrace)
	s.mux.HandleFunc("GET /api/v1/quotes/kline", s.kline)
	s.mux.HandleFunc("GET /api/v1/quotes/intraday", s.intraday)
	s.mux.HandleFunc("GET /api/v1/quotes/kline/batch", s.klineBatch)
	s.mux.HandleFunc("GET /api/v1/market/news", s.news)
	s.mux.HandleFunc("GET /api/v1/market/indexes", s.marketIndexesHandler)
	s.mux.HandleFunc("GET /api/v1/market/index-series", s.marketIndexSeriesHandler)
	s.mux.HandleFunc("GET /api/v1/market/industries", s.marketIndustriesHandler)
	s.mux.HandleFunc("GET /api/v1/market/flows", s.marketFlowsHandler)
	s.mux.HandleFunc("GET /api/v1/market/margin-balance", s.marketMarginBalanceHandler)
	s.mux.HandleFunc("GET /api/v1/market/billboard", s.marketBillboardHandler)
	s.mux.HandleFunc("GET /api/v1/market/billboard/detail", s.marketBillboardDetailHandler)
	s.mux.HandleFunc("GET /api/v1/market/futures-position", s.marketFuturesPositionHandler)
	s.mux.HandleFunc("GET /api/v1/market/futures-members", s.marketFuturesMembersHandler)
	s.mux.HandleFunc("GET /api/v1/market/futures-consensus", s.marketFuturesConsensusHandler)
	s.mux.HandleFunc("GET /api/v1/research/announcements", s.marketAnnouncementsHandler)
	s.mux.HandleFunc("GET /api/v1/research/institution-reports", s.marketInstitutionReportsHandler)
	s.mux.HandleFunc("GET /api/v1/research/industries", s.marketIndustryResearchHandler)
	s.mux.HandleFunc("GET /api/v1/themes/overview", s.themeOverviewHandler)
	s.mux.HandleFunc("GET /api/v1/themes/screen", s.themeScreenHandler)
	s.mux.HandleFunc("GET /api/v1/sector-map", s.sectorMapHandler)
	s.mux.HandleFunc("GET /api/v1/short-term/limit-up-ladder", s.limitUpLadderHandler)
	s.mux.HandleFunc("POST /api/v1/short-term/ladder-theme-ai", s.ladderThemeAIIdentify)
	s.mux.HandleFunc("GET /api/v1/short-term/ladder-theme-ai", s.ladderThemeAIResults)
	s.mux.HandleFunc("GET /api/v1/short-term/emotion-history", s.marketEmotionHistoryHandler)
	s.mux.HandleFunc("GET /api/v1/short-term/mastery", s.masteryIndex)
	s.mux.HandleFunc("GET /api/v1/short-term/mastery/trader", s.masteryTrader)
	s.mux.HandleFunc("POST /api/v1/short-term/mastery/refresh", s.masteryRefresh)
	s.mux.HandleFunc("POST /api/v1/stocks/ai-analysis", s.stockAIAnalysis)
	s.mux.HandleFunc("POST /api/v1/stocks/research", s.stockResearchCreate)
	s.mux.HandleFunc("GET /api/v1/stocks/research", s.stockResearchList)
	s.mux.HandleFunc("GET /api/v1/stocks/research/{id}", s.stockResearchGet)
	s.mux.HandleFunc("DELETE /api/v1/stocks/research/{id}", s.stockResearchDelete)
	s.mux.HandleFunc("POST /api/v1/stocks/research/{id}/cancel", s.stockResearchCancel)
	s.mux.HandleFunc("POST /api/v1/stocks/research/{id}/verify", s.stockResearchVerify)
	s.mux.HandleFunc("GET /api/v1/stocks/research/{id}/snapshot", s.stockResearchSnapshot)
	s.mux.HandleFunc("GET /api/v1/stocks/directory", s.stockDirectoryHandler)
	s.mux.HandleFunc("GET /api/v1/stocks/hot-ranks", s.hotStockRanksHandler)
	s.mux.HandleFunc("GET /api/v1/portfolio-inspections", s.portfolioInspectionList)
	s.mux.HandleFunc("POST /api/v1/portfolio-inspections", s.portfolioInspectionCreate)
	s.mux.HandleFunc("GET /api/v1/portfolio-inspections/{id}", s.portfolioInspectionGet)
	s.mux.HandleFunc("POST /api/v1/reviews/portfolio-expectations", s.portfolioExpectationCreate)
	s.mux.HandleFunc("GET /api/v1/reviews/portfolio-expectations/latest", s.portfolioExpectationLatest)
	s.mux.HandleFunc("GET /api/v1/reviews/portfolio-expectations/{id}", s.portfolioExpectationGet)
	s.mux.HandleFunc("GET /api/v1/reviews/sources", s.reviewSources)
	s.mux.HandleFunc("GET /api/v1/reviews/authors", s.reviewAuthors)
	s.mux.HandleFunc("DELETE /api/v1/reviews/authors/{id}", s.reviewAuthorDelete)
	s.mux.HandleFunc("GET /api/v1/reviews/posts", s.reviewPosts)
	s.mux.HandleFunc("GET /api/v1/reviews/posts/{id}", s.reviewPost)
	s.mux.HandleFunc("DELETE /api/v1/reviews/posts/{id}", s.reviewPostDelete)
	s.mux.HandleFunc("GET /api/v1/reviews/daily-summary", s.reviewDailySummaryGet)
	s.mux.HandleFunc("POST /api/v1/reviews/daily-summary/anonymize", s.reviewDailySummaryAnonymize)
	s.mux.HandleFunc("GET /api/v1/reviews/daily-summary/window", s.reviewDailySummaryWindow)
	s.mux.HandleFunc("GET /api/v1/reviews/daily-summary/status", s.reviewDailySummaryStatus)
	s.mux.HandleFunc("POST /api/v1/reviews/daily-summary", s.reviewDailySummaryCreate)
	s.mux.HandleFunc("GET /api/v1/reviews/daily-validation", s.reviewDailyValidation)
	s.mux.HandleFunc("GET /api/v1/reviews/daily-validation/status", s.reviewDailyValidationStatus)
	s.mux.HandleFunc("POST /api/v1/reviews/daily-validation", s.reviewDailyValidationCreate)
	s.mux.HandleFunc("POST /api/v1/reviews/import", s.reviewImport)
	s.mux.HandleFunc("GET /api/v1/reviews/subscriptions", s.reviewSubscriptions)
	s.mux.HandleFunc("POST /api/v1/reviews/subscriptions", s.reviewSubscriptionCreate)
	s.mux.HandleFunc("DELETE /api/v1/reviews/subscriptions/{id}", s.reviewSubscriptionDelete)
	s.mux.HandleFunc("POST /api/v1/reviews/sync", s.reviewSyncAll)
	s.mux.HandleFunc("POST /api/v1/reviews/subscriptions/{id}/sync", s.reviewSyncOne)
	s.mux.HandleFunc("POST /api/v1/reviews/posts/{id}/analyze", s.reviewAnalyzePost)
	s.mux.HandleFunc("GET /api/v1/reviews/remote-daily/status", s.reviewRemoteDailyStatus)
	s.mux.HandleFunc("POST /api/v1/reviews/remote-daily/sync", s.reviewRemoteDailySync)
	s.mux.HandleFunc("GET /api/v1/settings", s.settingsGet)
	s.mux.HandleFunc("GET /api/v1/settings/token-usage", s.tokenUsageSummary)
	s.mux.HandleFunc("POST /api/v1/settings/token-usage", s.tokenUsageRecord)
	s.mux.HandleFunc("PUT /api/v1/settings", s.settingsUpdate)
	s.mux.HandleFunc("GET /api/v1/settings/agent", s.settingsAgentGet)
	s.mux.HandleFunc("PUT /api/v1/settings/agent", s.settingsAgentUpdate)
	s.mux.HandleFunc("POST /api/v1/settings/agent/skills/import", s.settingsAgentSkillImport)
	s.mux.HandleFunc("POST /api/v1/settings/agent/skills/delete", s.settingsAgentSkillDelete)
	s.mux.HandleFunc("POST /api/v1/settings/agent/skills/install-git", s.settingsAgentSkillInstallGit)
	s.mux.HandleFunc("GET /api/v1/settings/agent/skills/market", s.settingsAgentSkillMarket)
	s.mux.HandleFunc("GET /api/v1/settings/agent/skills/market/sources", s.settingsAgentSkillMarketSources)
	s.mux.HandleFunc("POST /api/v1/settings/llm/models", s.settingsLLMModels)
	s.mux.HandleFunc("POST /api/v1/settings/llm/test", s.settingsLLMTest)
	s.mux.HandleFunc("GET /api/v1/ai/ws", s.aiChatWebSocket)
	s.mux.HandleFunc("POST /api/v1/strategy/inflections/evaluate", s.inflectionEvaluate)
	s.mux.HandleFunc("GET /api/v1/ws/stream", s.stream)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"name": "easy-stock data foundation",
		"time": time.Now(),
	})
}

func (s *Server) sourceCatalog() []registry.Descriptor {
	return append(s.dataSources.Catalog(), s.contentSources.Catalog()...)
}

func (s *Server) sources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sources": s.sourceHealth.snapshot(time.Now()), "probes": s.sourceProbes.snapshot(), "catalog": s.sourceCatalog()})
}

func (s *Server) realtime(w http.ResponseWriter, r *http.Request) {
	symbolsParam := strings.TrimSpace(r.URL.Query().Get("symbols"))
	if symbolsParam == "" {
		writeError(w, http.StatusBadRequest, "symbols is required")
		return
	}
	symbols, err := foundation.SplitSymbols(symbolsParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if r.URL.Query().Get("detail") == "1" && len(symbols) == 1 {
		quotes, stale, err := s.detailQuotes.load(r.Context(), detailPollKey(symbols[0], "quote", time.Now()), func(ctx context.Context) ([]foundation.Quote, error) {
			items, loadErr := s.realtimeProvider.Realtime(ctx, symbols)
			if loadErr == nil && len(items) == 0 {
				loadErr = fmt.Errorf("realtime source returned no quotes")
			}
			if loadErr != nil && shouldObserveFailure(ctx) {
				s.sourceHealth.failure(s.realtimeSourceID, loadErr)
			}
			if loadErr == nil {
				for _, item := range items {
					s.sourceHealth.success(item.Meta)
				}
			}
			if loadErr == nil {
				items = markHistoricalDetailQuotes(items, time.Now())
			}
			return items, loadErr
		})
		if err != nil {
			writeError(w, http.StatusBadGateway, "单股行情暂不可用，请稍后重试")
			return
		}
		if stale {
			quotes = cloneDetailQuotesAsStale(quotes)
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": quotes})
		return
	}
	quotes, err := s.realtimeProvider.Realtime(r.Context(), symbols)
	if err != nil {
		if shouldObserveFailure(r.Context()) {
			s.sourceHealth.failure(s.realtimeSourceID, err)
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	for _, quote := range quotes {
		s.sourceHealth.success(quote.Meta)
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": quotes})
}

func (s *Server) kline(w http.ResponseWriter, r *http.Request) {
	symbol := strings.TrimSpace(r.URL.Query().Get("symbol"))
	if symbol == "" {
		writeError(w, http.StatusBadRequest, "symbol is required")
		return
	}
	period := canonicalKLinePeriod(firstNonEmpty(r.URL.Query().Get("period"), "day"))
	if period == "" {
		writeError(w, http.StatusBadRequest, "unsupported kline period")
		return
	}
	normalizedSymbol, normalizeErr := foundation.NormalizeSymbol(symbol)
	if normalizeErr != nil {
		writeError(w, http.StatusBadRequest, normalizeErr.Error())
		return
	}
	symbol = normalizedSymbol.Canonical
	providerName := firstNonEmpty(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("provider"))), "source")
	if providerName == "eastmoney" {
		writeError(w, http.StatusBadRequest, "东方财富个股K与指定复权入口已退役；请明确选择腾讯口径")
		return
	}
	if providerName != "source" && s.priceDataService().StrictProvider(providerName) == nil {
		writeError(w, http.StatusBadRequest, "provider must be source or tencent")
		return
	}
	if r.URL.Query().Get("asof") != "" {
		writeError(w, http.StatusBadRequest, "asof adjustment-factor backtesting is not supported")
		return
	}
	limit := 120
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	adjustment := strings.TrimSpace(r.URL.Query().Get("adjust"))
	if adjustment != "" && adjustment != "source" && adjustment != "none" && adjustment != "qfq" && adjustment != "hfq" {
		writeError(w, http.StatusBadRequest, "adjust must be source, none, qfq or hfq")
		return
	}
	strictRequest := providerName != "source" || (adjustment != "" && adjustment != "source")
	strictProvider := providerName
	if strictProvider == "source" {
		strictProvider = s.defaultStrictPriceSource
		if strictProvider == "" && s.strictPriceSources == nil {
			strictProvider = "tencent"
		}
	}
	strictAdjustment := adjustment
	if strictAdjustment == "" || strictAdjustment == "source" {
		strictAdjustment = "none"
	}
	if strictRequest {
		if period != "day" && period != "week" && period != "month" && period != "year" {
			writeError(w, http.StatusBadRequest, "minute data does not support explicit provider/adjustment")
			return
		}
		checkPeriod := period
		if checkPeriod == "year" {
			checkPeriod = "month"
		}
		if !s.priceDataService().SupportsAdjusted(strictProvider, symbol, checkPeriod, strictAdjustment) {
			writeError(w, http.StatusBadRequest, "Tencent stock source does not support this instrument or period")
			return
		}
	}
	if r.URL.Query().Get("detail") == "1" && strings.TrimSpace(period) == "1" {
		normalized, normalizeErr := foundation.NormalizeSymbol(symbol)
		if normalizeErr != nil {
			writeError(w, http.StatusBadRequest, normalizeErr.Error())
			return
		}
		lines, stale, loadErr := s.detailKLines.load(r.Context(), detailPollKey(normalized.Canonical, "1m:"+strconv.Itoa(limit), time.Now()), func(ctx context.Context) ([]foundation.KLine, error) {
			items, upstreamErr := s.loadKLine(ctx, normalized.Canonical, "1", limit)
			if upstreamErr == nil && len(items) == 0 {
				upstreamErr = fmt.Errorf("minute source returned no lines")
			}
			if upstreamErr == nil {
				items = markHistoricalDetailKLines(items, time.Now())
			}
			return items, upstreamErr
		})
		if loadErr != nil {
			writeError(w, http.StatusBadGateway, "单股分时暂不可用，请稍后重试")
			return
		}
		if stale {
			lines = cloneDetailKLinesAsStale(lines)
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": lines})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var lines []foundation.KLine
	var err error
	if strictRequest {
		lines, err = s.loadProviderAdjustedKLine(ctx, symbol, period, limit, strictAdjustment, strictProvider)
		if err == nil && (adjustment == "" || adjustment == "source") {
			for index := range lines {
				lines[index].Meta.RequestedAdjustment = "source"
			}
		}
	} else {
		lines, err = s.loadKLine(ctx, symbol, period, limit)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": lines})
}

func (s *Server) klineBatch(w http.ResponseWriter, r *http.Request) {
	symbolsParam := strings.TrimSpace(r.URL.Query().Get("symbols"))
	if symbolsParam == "" {
		writeError(w, http.StatusBadRequest, "symbols is required")
		return
	}
	symbols, err := foundation.SplitSymbols(symbolsParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(symbols) > 30 {
		writeError(w, http.StatusBadRequest, "batch kline supports at most 30 symbols")
		return
	}
	period := firstNonEmpty(r.URL.Query().Get("period"), "day")
	limit := 40
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed <= 0 || parsed > 240 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 240")
			return
		}
		limit = parsed
	}

	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	data := make(map[string][]foundation.KLine, len(symbols))
	errorsBySymbol := map[string]string{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 4)
	for _, symbol := range symbols {
		symbol := symbol
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				mu.Lock()
				errorsBySymbol[symbol] = ctx.Err().Error()
				mu.Unlock()
				return
			}
			lines, loadErr := s.loadKLine(ctx, symbol, period, limit)
			mu.Lock()
			defer mu.Unlock()
			if loadErr != nil {
				errorsBySymbol[symbol] = loadErr.Error()
				return
			}
			data[symbol] = lines
		}()
	}
	wg.Wait()
	if len(data) == 0 {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": "all batch kline requests failed", "errors": errorsBySymbol})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": data, "errors": errorsBySymbol})
}

func (s *Server) loadKLine(ctx context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	return s.priceDataService().KLine(ctx, symbol, period, limit)
}
func normalizeKLinePeriod(lines []foundation.KLine, period string) []foundation.KLine {
	return service.NormalizeKLinePeriod(lines, period)
}
func (s *Server) news(w http.ResponseWriter, r *http.Request) {
	source := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source")))
	if source == "" {
		source = s.newsSourceID
		if source == "" {
			source = "cls"
		}
	}
	if source != s.newsSourceID && !(s.newsSourceID == "" && source == "cls") {
		writeError(w, http.StatusBadRequest, "unsupported news source")
		return
	}
	limit := 20
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
	}
	items, err := s.newsProvider.LatestNews(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (s *Server) sectorMapHandler(w http.ResponseWriter, r *http.Request) {
	theme := strings.TrimSpace(r.URL.Query().Get("theme"))
	if theme == "" {
		theme = "semiconductor_materials"
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	sectorMap, err := s.sectorMap.Build(ctx, theme)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": sectorMap})
}

func (s *Server) themeOverviewHandler(w http.ResponseWriter, r *http.Request) {
	if s.themeOverview == nil {
		writeError(w, http.StatusServiceUnavailable, "theme overview provider is unavailable")
		return
	}
	if r.URL.Query().Get("delivery") == "progressive" {
		s.progressiveThemeOverview(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var items []foundation.ThemeOverview
	var meta foundation.SourceMeta
	var err error
	if provider, ok := s.themeOverview.(interface {
		OverviewsWithObservations(context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, []foundation.SourceObservation, error)
	}); ok {
		var observations []foundation.SourceObservation
		items, meta, observations, err = provider.OverviewsWithObservations(ctx)
		if shouldObserveFailure(ctx) {
			for _, observation := range observations {
				s.sourceHealth.observe(observation)
			}
		}
	} else {
		items, meta, err = s.themeOverview.Overviews(ctx)
		if err == nil && ctx.Err() == nil {
			s.sourceHealth.fallback(meta)
			s.sourceHealth.success(meta)
		}
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items, "meta": meta})
}

func (s *Server) inflectionEvaluate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request inflection.EvaluationRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid inflection request: "+err.Error())
		return
	}
	if err := ensureJSONEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, err := s.inflection.Evaluate(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

func writeError(w http.ResponseWriter, status int, message string) {
	if status >= http.StatusInternalServerError {
		log.Printf("level=error event=http_error status=%d message=%q", status, runtimelog.Redact(message))
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
