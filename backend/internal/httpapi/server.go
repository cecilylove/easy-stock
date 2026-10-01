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
	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/hermes"
	"easy-stock/backend/internal/marketemotion"
	"easy-stock/backend/internal/methodology"
	"easy-stock/backend/internal/portfolioinspection"
	"easy-stock/backend/internal/providers/cls"
	"easy-stock/backend/internal/providers/duanxianxia"
	"easy-stock/backend/internal/providers/eastmoney"
	futurespositionprovider "easy-stock/backend/internal/providers/futuresposition"
	"easy-stock/backend/internal/providers/hotstock"
	marketoverviewprovider "easy-stock/backend/internal/providers/marketoverview"
	"easy-stock/backend/internal/providers/sina"
	"easy-stock/backend/internal/providers/tencent"
	"easy-stock/backend/internal/review"
	"easy-stock/backend/internal/runtimelog"
	"easy-stock/backend/internal/sector"
	"easy-stock/backend/internal/stockanalysis"
	"easy-stock/backend/internal/strategy/inflection"
)

type Server struct {
	ladderThemeAI         *ladderThemeAI
	mux                   *http.ServeMux
	token                 string
	allowedOrigins        []string
	enforceLoopbackHost   bool
	realtimeProvider      RealtimeProvider
	detailQuotes          *detailPollCache[[]foundation.Quote]
	detailKLines          *detailPollCache[[]foundation.KLine]
	detailAuctions        *detailPollCache[foundation.AuctionTrace]
	auctionProvider       AuctionProvider
	auctionSourceID       string
	realtimeSourceID      string
	kLinePrimary          KLineProvider
	kLinePrimarySourceID  string
	kLineFallback         KLineProvider
	kLineFallbackSourceID string
	newsProvider          NewsProvider
	newsSourceID          string
	sectorMap             SectorMapProvider
	themeOverview         ThemeOverviewProvider
	limitUpProvider       LimitUpProvider
	marketPools           MarketPoolProvider
	stockConcepts         StockConceptProvider
	stockBusiness         StockBusinessProfileProvider
	stockDirectory        StockDirectoryProvider
	hotStockProvider      HotStockProvider
	futuresPosition       FuturesPositionProvider
	marketOverview        MarketOverviewProvider
	marketFailureSourceID string
	inflection            InflectionEvaluator
	themeSnapshots        *themeSnapshotCache
	limitUpSnapshots      *limitUpLadderCache
	limitUpProgress       *shortTermCache[limitUpLadderData]
	emotionProgress       *shortTermCache[marketemotion.History]
	stockDirectories      *stockDirectoryCache
	hotStockRanks         *hotStockRankCache
	marketSnapshots       *marketOverviewCache
	sourceHealth          *sourceHealthTracker
	marketEmotion         *marketEmotionEngine
	marketEmotionIntraday *marketEmotionIntradayCache
	reviewStore           *review.Store
	portfolioStore        *portfolioinspection.Store
	portfolioInspection   *portfolioinspection.Service
	portfolioExpectation  *portfolioinspection.ExpectationService
	stockResearchStore    *stockanalysis.ResearchStore
	stockResearch         *stockanalysis.ResearchService
	reviewImporter        ReviewImporter
	wechatAPIURL          string
	settingsStore         *appsettings.Store
	reviewAutomation      *review.Automation
	remoteDailySync       *review.RemoteDailySync
	hermesGateway         hermes.Gateway
	usageGateway          hermes.Gateway
	masteryLibrary        *methodology.Library
	marketEmotionStore    *marketemotion.Store
	themeRadarStore       *duanxianxia.Store
	themeProgress         *themeProgressCache
	startupError          error
	logger                *log.Logger
	tokenUsage            *tokenUsageStore
}

func NewServer(config any) *Server {
	cfg := normalizeConfig(config)
	tokenUsage := newTokenUsageStore(cfg.SettingsPath)
	if cfg.Logger == nil {
		cfg.Logger = log.Default()
	}
	var startupErrors []error
	sinaClient := sina.NewClient()
	eastMoneyClient := eastmoney.NewClient()
	tencentClient := tencent.NewClient()
	clsClient := cls.NewClient()
	realtimeSourceID, primarySourceID, fallbackSourceID, newsSourceID := "", "", "", ""
	if cfg.Realtime == nil {
		cfg.Realtime = sinaClient
		realtimeSourceID = "sina"
	}
	auctionSourceID := ""
	if cfg.Auction == nil {
		cfg.Auction = eastMoneyClient
		auctionSourceID = "eastmoney"
	}
	if cfg.KLinePrimary == nil {
		cfg.KLinePrimary = eastMoneyClient
		primarySourceID = "eastmoney"
	}
	if cfg.KLineFallback == nil {
		cfg.KLineFallback = sinaClient
		fallbackSourceID = "sina"
	}
	if cfg.News == nil {
		cfg.News = clsClient
		newsSourceID = "cls"
	}
	var kaipanlaService *duanxianxia.Service
	if strings.TrimSpace(cfg.ThemeRadarDBPath) != "" {
		if store, err := duanxianxia.OpenStore(cfg.ThemeRadarDBPath); err == nil {
			client := duanxianxia.NewClient(duanxianxia.ClientConfig{BaseURL: cfg.DuanxianxiaBaseURL})
			kaipanlaService = duanxianxia.NewService(client, store, duanxianxia.ServiceConfig{
				RefreshInterval:  5 * time.Minute,
				LeaderThemeLimit: 3,
			})
		} else if cfg.StrictPersistence {
			startupErrors = append(startupErrors, fmt.Errorf("open theme radar database: %w", err))
		}
	}
	usingDefaultLimitUp := cfg.LimitUp == nil
	if usingDefaultLimitUp {
		if kaipanlaService != nil {
			cfg.LimitUp = duanxianxia.NewLimitUpProvider(kaipanlaService, eastMoneyClient)
		} else {
			cfg.LimitUp = eastMoneyClient
		}
	}
	if cfg.MarketPools == nil {
		cfg.MarketPools = eastMoneyClient
	}
	if cfg.StockConcept == nil && usingDefaultLimitUp {
		cfg.StockConcept = eastMoneyClient
	}
	if cfg.StockBusiness == nil {
		cfg.StockBusiness = eastMoneyClient
	}
	if cfg.StockDirectory == nil {
		cfg.StockDirectory = eastMoneyClient
	}
	marketFailureSourceID := ""
	if cfg.MarketOverview == nil {
		cfg.MarketOverview = marketoverviewprovider.New(eastMoneyClient, tencentClient, tencentClient, sinaClient)
		marketFailureSourceID = "eastmoney"
	}
	if cfg.SectorMap == nil {
		mapper := sector.NewMapper(
			eastMoneyClient,
			sector.WithQuoteProvider(sinaClient),
			sector.WithLimitUpProvider(cfg.LimitUp),
			sector.WithStockCatalogProvider(eastMoneyClient),
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
		radar := sector.NewRadarProvider(radarSource, radarFallback, cfg.Realtime, sector.RadarProviderConfig{
			IndustryMomentum:  cfg.MarketOverview,
			IndustryStocks:    tencentClient,
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
		cfg.HotStocks = hotstock.NewClient()
	}
	if cfg.FuturesPosition == nil {
		cfg.FuturesPosition = futurespositionprovider.NewClient()
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
	if cfg.ReviewImporter == nil {
		cfg.ReviewImporter = review.NewImporter(cfg.ReviewHTTP, cfg.WeChatAPIURL)
	}
	if cfg.ReviewAutomation == nil {
		cfg.ReviewAutomation = review.NewAutomation(cfg.ReviewStore, cfg.ReviewImporter, cfg.SettingsStore, cfg.ReviewHTTP, cfg.WeChatAPIURL, usageGateway)
	}
	if dailyMarketProvider := newReviewDailyMarketProvider(cfg.MarketOverview); dailyMarketProvider != nil {
		cfg.ReviewAutomation.SetDailyMarketProvider(dailyMarketProvider)
	}
	if cfg.RemoteDailySync == nil {
		cfg.RemoteDailySync = review.NewRemoteDailySync(cfg.ReviewStore, review.RemoteDailySyncConfig{
			BaseURL: cfg.RemoteDailyReviewURL,
			Client:  cfg.ReviewHTTP,
		})
	}
	s := &Server{
		mux:                   http.NewServeMux(),
		token:                 cfg.Token,
		allowedOrigins:        append([]string(nil), cfg.AllowedOrigins...),
		enforceLoopbackHost:   cfg.EnforceLoopbackHost,
		realtimeProvider:      cfg.Realtime,
		detailQuotes:          newDetailPollCache[[]foundation.Quote](),
		detailKLines:          newDetailPollCache[[]foundation.KLine](),
		detailAuctions:        newDetailPollCache[foundation.AuctionTrace](),
		auctionProvider:       cfg.Auction,
		auctionSourceID:       auctionSourceID,
		realtimeSourceID:      realtimeSourceID,
		kLinePrimary:          cfg.KLinePrimary,
		kLinePrimarySourceID:  primarySourceID,
		kLineFallback:         cfg.KLineFallback,
		kLineFallbackSourceID: fallbackSourceID,
		newsProvider:          cfg.News,
		newsSourceID:          newsSourceID,
		sectorMap:             cfg.SectorMap,
		themeOverview:         cfg.ThemeOverview,
		limitUpProvider:       cfg.LimitUp,
		marketPools:           cfg.MarketPools,
		stockConcepts:         cfg.StockConcept,
		stockBusiness:         cfg.StockBusiness,
		stockDirectory:        cfg.StockDirectory,
		hotStockProvider:      cfg.HotStocks,
		futuresPosition:       cfg.FuturesPosition,
		marketOverview:        cfg.MarketOverview,
		marketFailureSourceID: marketFailureSourceID,
		inflection:            cfg.Inflection,
		themeSnapshots:        newThemeSnapshotCache(30 * time.Second),
		themeProgress:         newThemeProgressCache(),
		limitUpSnapshots:      newLimitUpLadderCache(30 * time.Second),
		limitUpProgress:       &shortTermCache[limitUpLadderData]{},
		emotionProgress:       &shortTermCache[marketemotion.History]{},
		stockDirectories:      newStockDirectoryCache(6 * time.Hour),
		hotStockRanks:         newHotStockRankCache(2 * time.Minute),
		marketSnapshots:       newMarketOverviewCache(45 * time.Second),
		sourceHealth:          newSourceHealthTracker(),
		marketEmotionIntraday: newMarketEmotionIntradayCache(marketEmotionIntradayTTL),
		reviewStore:           cfg.ReviewStore,
		portfolioStore:        cfg.PortfolioStore,
		stockResearchStore:    cfg.StockResearchStore,
		reviewImporter:        cfg.ReviewImporter,
		wechatAPIURL:          strings.TrimSpace(cfg.WeChatAPIURL),
		settingsStore:         cfg.SettingsStore,
		ladderThemeAI:         newLadderThemeAI(cfg.SettingsPath),
		reviewAutomation:      cfg.ReviewAutomation,
		remoteDailySync:       cfg.RemoteDailySync,
		hermesGateway:         cfg.HermesGateway,
		usageGateway:          usageGateway,
		masteryLibrary:        cfg.MasteryLibrary,
		marketEmotionStore:    cfg.MarketEmotionStore,
		startupError:          errors.Join(startupErrors...),
		logger:                cfg.Logger,
		tokenUsage:            tokenUsage,
	}
	if kaipanlaService != nil {
		s.themeRadarStore = kaipanlaService.Store()
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
	s.marketEmotion = newMarketEmotionEngine(
		cfg.MarketEmotionStore,
		s.limitUpProvider,
		s.marketPools,
		s.kLinePrimary,
		s.kLineFallback,
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
	s.mux.HandleFunc("GET /api/v1/quotes/realtime", s.realtime)
	s.mux.HandleFunc("GET /api/v1/quotes/auction", s.auctionTrace)
	s.mux.HandleFunc("GET /api/v1/quotes/kline", s.kline)
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

func (s *Server) sources(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"sources": s.sourceHealth.snapshot(time.Now())})
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
	period := firstNonEmpty(r.URL.Query().Get("period"), "day")
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
	if adjustment != "" && adjustment != "source" && strings.TrimSpace(period) == "1" && r.URL.Query().Get("detail") == "1" {
		writeError(w, http.StatusBadRequest, "single-day detail minute data does not support explicit adjustment")
		return
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
	if adjustment != "" && adjustment != "source" {
		lines, err = s.loadAdjustedKLine(ctx, symbol, period, limit, adjustment)
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

func (s *Server) loadKLine(ctx context.Context, symbol string, period string, limit int) ([]foundation.KLine, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(period) == "year" {
		// Neither configured source has a portable calendar-year period.
		// Fetch an extra year of monthly history to avoid a truncated leading year.
		limit = min(max(limit, 1), 50)
		months, err := s.loadKLine(ctx, symbol, "month", (limit+1)*12)
		if err != nil {
			return nil, err
		}
		years := aggregateYearKLines(months, limit)
		if len(years) == 0 {
			return nil, fmt.Errorf("monthly source returned no usable bars for year aggregation")
		}
		return years, nil
	}
	// The primary gets at most half of the remaining request budget, capped
	// at six seconds, so a slow primary cannot prevent the fallback running.
	primaryBudget := 6 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		primaryBudget = min(primaryBudget, time.Until(deadline)/2)
	}
	primaryCtx, cancelPrimary := context.WithTimeout(ctx, primaryBudget)
	lines, err := s.kLinePrimary.KLine(primaryCtx, symbol, period, limit)
	if err == nil {
		err = primaryCtx.Err()
	}
	cancelPrimary()
	if err == nil && len(lines) == 0 {
		err = fmt.Errorf("primary kline source returned no bars")
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil, ctx.Err()
	}
	if err == nil {
		for _, line := range lines {
			s.sourceHealth.success(line.Meta)
		}
		return normalizeKLinePeriod(lines, period), nil
	}
	if shouldObserveFailure(ctx) {
		s.sourceHealth.failure(s.kLinePrimarySourceID, err)
	}
	// Do not attribute an already expired/cancelled request to a fallback
	// that has not actually been called.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	primaryErr := err
	fallbackCtx, cancelFallback := context.WithTimeout(ctx, 10*time.Second)
	defer cancelFallback()
	lines, err = s.kLineFallback.KLine(fallbackCtx, symbol, period, limit)
	if err == nil {
		err = fallbackCtx.Err()
	}
	if err == nil && len(lines) == 0 {
		err = fmt.Errorf("fallback kline source returned no bars")
	}
	if err != nil {
		if shouldObserveFailure(ctx) {
			s.sourceHealth.failure(s.kLineFallbackSourceID, err)
		}
		return nil, fmt.Errorf("primary kline failed: %v; fallback failed: %w", primaryErr, err)
	}
	lines = append([]foundation.KLine(nil), lines...)
	for index := range lines {
		if lines[index].Meta.FallbackReason == "" {
			lines[index].Meta.FallbackReason = "主 K 线来源不可用，已切换备用来源"
		}
		s.sourceHealth.success(lines[index].Meta)
	}
	return normalizeKLinePeriod(lines, period), nil
}

func normalizeKLinePeriod(lines []foundation.KLine, period string) []foundation.KLine {
	if strings.TrimSpace(period) != "1" || len(lines) == 0 {
		return lines
	}

	chinaTime := time.FixedZone("Asia/Shanghai", 8*60*60)
	latestTime := time.Time{}
	for _, line := range lines {
		if !line.Time.IsZero() && line.Time.After(latestTime) {
			latestTime = line.Time
		}
	}
	if latestTime.IsZero() {
		return lines
	}

	latestDate := latestTime.In(chinaTime).Format("2006-01-02")
	previousTime := time.Time{}
	previousClose := 0.0
	for _, line := range lines {
		if line.Time.IsZero() || line.Time.In(chinaTime).Format("2006-01-02") == latestDate {
			continue
		}
		if line.Close > 0 && line.Time.Before(latestTime) && line.Time.After(previousTime) {
			previousTime = line.Time
			previousClose = line.Close
		}
	}

	filtered := make([]foundation.KLine, 0, len(lines))
	for _, line := range lines {
		if line.Time.IsZero() || line.Time.In(chinaTime).Format("2006-01-02") != latestDate {
			continue
		}
		if line.PreviousClose <= 0 && previousClose > 0 {
			line.PreviousClose = previousClose
		}
		filtered = append(filtered, line)
	}
	if len(filtered) == 0 {
		return lines
	}
	return filtered
}

func (s *Server) news(w http.ResponseWriter, r *http.Request) {
	source := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source")))
	if source == "" {
		source = "cls"
	}
	if source != "cls" {
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
		if shouldObserveFailure(r.Context()) {
			s.sourceHealth.failure(s.newsSourceID, err)
		}
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	for _, item := range items {
		s.sourceHealth.success(item.Meta)
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
