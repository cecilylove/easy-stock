package assembly

import (
	"easy-stock/backend/internal/datasource/registry"
	"fmt"
	"strings"
)

// Routes explicitly opts registered abilities into business access. Registration
// never silently changes a default chain or permits a different adjustment basis.
type Routes struct {
	Realtime, Auction, Intraday, HistoryIntraday, News, Directory, Business, Fundamentals, LimitUp, Pools, Boards                        string
	KLine                                                                                                                                []string
	Strict                                                                                                                               []string
	DefaultStrict                                                                                                                        string
	Index, Industry, IndustryFallback, FundFlow, FundFlowFallback, Margin, Billboard, Announcements, Reports, USSector, USSectorFallback string
	HotRanks                                                                                                                             []string
	FuturesHistory, FuturesExchange                                                                                                      string
	FuturesSnapshot, FuturesMembers, FuturesConsensus                                                                                    string // Independent overrides; FuturesExchange is the legacy combined default.
	BoardMembers                                                                                                                         []string
	Theme                                                                                                                                string
	BillboardLabels                                                                                                                      string // Optional enrichment; empty disables labels without disabling raw details.
}

func DefaultRoutes() Routes {
	return Routes{
		Realtime: "sina", Auction: "eastmoney", Intraday: "sina", HistoryIntraday: "sina", News: "cls", Directory: "eastmoney", Business: "eastmoney", Fundamentals: "eastmoney", LimitUp: "eastmoney", Pools: "eastmoney", Boards: "eastmoney", Theme: "duanxianxia",
		KLine: []string{"sina", "tencent"}, Strict: []string{"tencent"}, DefaultStrict: "tencent", Index: "tencent", Industry: "tencent", IndustryFallback: "eastmoney", FundFlow: "sina", FundFlowFallback: "eastmoney", Margin: "eastmoney", Billboard: "eastmoney", BillboardLabels: "ths", Announcements: "eastmoney", Reports: "eastmoney", USSectorFallback: "tencent", HotRanks: []string{"ths", "eastmoney"}, FuturesHistory: "eastmoney", FuturesExchange: "cffex", BoardMembers: []string{"tencent"},
	}
}

// Validate checks capability bindings before activation; removed/missing sources
// must be explicitly removed from affected routes. Empty routes disable abilities.
func (r Routes) Validate(sources *registry.Registry) error {
	for _, chain := range [][]string{r.KLine, r.Strict} {
		seen := map[string]bool{}
		for _, id := range chain {
			if id == "eastmoney" {
				return fmt.Errorf("retired EastMoney price route cannot be enabled")
			}
			if seen[id] {
				return fmt.Errorf("duplicate source route %q", id)
			}
			seen[id] = true
		}
	}
	if r.Index == "eastmoney" {
		return fmt.Errorf("retired EastMoney index route cannot be enabled")
	}
	if r.DefaultStrict != "" {
		found := false
		for _, id := range r.Strict {
			if id == r.DefaultStrict {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("default strict source %q is not enabled", r.DefaultStrict)
		}
	}
	checks := []struct {
		id, cap string
		present func(registry.Capabilities) bool
	}{
		{r.Realtime, "quote", func(c registry.Capabilities) bool { return c.Realtime != nil }},
		{r.Auction, "auction", func(c registry.Capabilities) bool { return c.Auction != nil }},
		{r.Intraday, "intraday", func(c registry.Capabilities) bool { return c.Intraday != nil }},
		{r.HistoryIntraday, "historical-intraday", func(c registry.Capabilities) bool { return c.HistoryIntraday != nil }},
		{r.News, "news", func(c registry.Capabilities) bool { return c.News != nil }},
		{r.Directory, "stock-directory", func(c registry.Capabilities) bool { return c.Directory != nil }},
		{r.Business, "business", func(c registry.Capabilities) bool { return c.Business != nil }},
		{r.Fundamentals, "fundamentals", func(c registry.Capabilities) bool { return c.Fundamentals != nil }},
		{r.Index, "index", func(c registry.Capabilities) bool { return c.Index != nil }},
		{r.Industry, "industry", func(c registry.Capabilities) bool { return c.Industry != nil }},
		{r.IndustryFallback, "industry-fallback", func(c registry.Capabilities) bool { return c.Industry != nil }},
		{r.FundFlow, "fund-flow", func(c registry.Capabilities) bool { return c.FundFlow != nil }},
		{r.FundFlowFallback, "fund-flow-fallback", func(c registry.Capabilities) bool { return c.FundFlow != nil }},
		{r.Margin, "margin", func(c registry.Capabilities) bool { return c.Margin != nil }},
		{r.Billboard, "billboard", func(c registry.Capabilities) bool { return c.Billboard != nil }},
		{r.BillboardLabels, "billboard-labels", func(c registry.Capabilities) bool { return c.BillboardLabels != nil }},
		{r.Announcements, "announcements", func(c registry.Capabilities) bool { return c.Announcements != nil }},
		{r.Reports, "reports", func(c registry.Capabilities) bool { return c.Reports != nil }},
		{r.LimitUp, "limit-up", func(c registry.Capabilities) bool { return c.LimitUp != nil }},
		{r.Pools, "market-pools", func(c registry.Capabilities) bool { return c.Pools != nil }},
		{r.Boards, "boards", func(c registry.Capabilities) bool { return c.Boards != nil }},
		{r.USSector, "us-sector", func(c registry.Capabilities) bool { return c.USSector != nil }},
		{r.USSectorFallback, "us-sector-fallback", func(c registry.Capabilities) bool { return c.USSector != nil }},
		{r.FuturesHistory, "futures-history", func(c registry.Capabilities) bool { return c.FuturesTrend != nil }},
		{r.FuturesSnapshotSource(), "futures-snapshot", func(c registry.Capabilities) bool { return c.FuturesSnapshot != nil }},
		{r.FuturesMembersSource(), "futures-members", func(c registry.Capabilities) bool { return c.FuturesMembers != nil }},
		{r.FuturesConsensusSource(), "futures-consensus", func(c registry.Capabilities) bool { return c.FuturesConsensus != nil }},
		{r.Theme, "theme", func(c registry.Capabilities) bool { return c.Theme != nil }},
	}
	for _, id := range r.KLine {
		checks = append(checks, struct {
			id, cap string
			present func(registry.Capabilities) bool
		}{id, "kline", func(c registry.Capabilities) bool { return c.KLine != nil }})
	}
	for _, id := range r.Strict {
		checks = append(checks, struct {
			id, cap string
			present func(registry.Capabilities) bool
		}{id, "adjusted-kline", func(c registry.Capabilities) bool { return c.AdjustedKLine != nil }})
	}
	for _, id := range r.HotRanks {
		checks = append(checks, struct {
			id, cap string
			present func(registry.Capabilities) bool
		}{id, "hot-ranks", func(c registry.Capabilities) bool { return c.HotRank != nil }})
	}
	for _, id := range r.BoardMembers {
		checks = append(checks, struct {
			id, cap string
			present func(registry.Capabilities) bool
		}{id, "board-members", func(c registry.Capabilities) bool { return c.BoardMembers != nil }})
	}
	for _, check := range checks {
		if check.id == "" {
			continue
		}
		entry, ok := sources.Lookup(check.id)
		if !ok || !entry.Descriptor.Enabled || !entry.Descriptor.Implemented || !check.present(entry.Capabilities) {
			return fmt.Errorf("source route %s: %q is disabled, missing, or unsupported", check.cap, check.id)
		}
		// Interface values containing typed nil pointers must not activate a
		// route. The canonical slot list already excludes those placeholders.
		capability := strings.TrimSuffix(check.cap, "-fallback")
		found := false
		for _, actual := range registry.CanonicalCapabilities(entry.Capabilities) {
			if actual == capability {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("source route %s: %q has no implementation", check.cap, check.id)
		}
	}
	return nil
}
func Capabilities(sources *registry.Registry, id string) registry.Capabilities {
	entry, ok := sources.Lookup(id)
	if !ok || !entry.Descriptor.Enabled || !entry.Descriptor.Implemented {
		return registry.Capabilities{}
	}
	return entry.Capabilities
}

func (r Routes) FuturesSnapshotSource() string {
	if r.FuturesSnapshot != "" {
		return r.FuturesSnapshot
	}
	return r.FuturesExchange
}
func (r Routes) FuturesMembersSource() string {
	if r.FuturesMembers != "" {
		return r.FuturesMembers
	}
	return r.FuturesExchange
}
func (r Routes) FuturesConsensusSource() string {
	if r.FuturesConsensus != "" {
		return r.FuturesConsensus
	}
	return r.FuturesExchange
}
