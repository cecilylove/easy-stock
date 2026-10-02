// Package registry owns the static, typed supplier directory. It does not own
// business merging, HTTP handlers, credentials, or network requests.
package registry

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type Descriptor struct {
	ArticleHosts  []string `json:"articleHosts,omitempty"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Mode          string   `json:"mode"`
	Kinds         []string `json:"kinds"`
	Usage         string   `json:"usage"`
	Configuration string   `json:"configuration"`
	Capabilities  []string `json:"capabilities"`
	ProbeScope    string   `json:"probeScope"`
	Implemented   bool     `json:"implemented"`
	Enabled       bool     `json:"enabled"`
}
type Probe func(context.Context, time.Time) error
type Capabilities struct {
	BrowserCollection contracts.BrowserCollectionProvider
	AuthorLinks       contracts.AuthorLinksProvider
	AuthorizedArticle contracts.AuthorizedArticleProvider
	Article           contracts.ArticleSource
	Archive           contracts.ArchiveProvider
	Knowledge         contracts.KnowledgeProvider
	Boards            contracts.BoardProvider
	BoardMembers      contracts.BoardMemberProvider
	Theme             contracts.ThemeFetcher
	USSector          contracts.USSectorProvider
	Realtime          contracts.RealtimeProvider
	Auction           contracts.AuctionProvider
	KLine             contracts.KLineProvider
	AdjustedKLine     contracts.AdjustedKLineProvider
	Intraday          contracts.KLineProvider
	HistoryIntraday   contracts.HistoryIntradayProvider
	News              contracts.NewsProvider
	Index             contracts.IndexProvider
	Industry          contracts.IndustryProvider
	FundFlow          contracts.FundFlowProvider
	Margin            contracts.MarginProvider
	Billboard         contracts.BillboardProvider
	Announcements     contracts.AnnouncementProvider
	Reports           contracts.ReportProvider
	Directory         contracts.StockDirectoryProvider
	ProbeDirectory    contracts.StockDirectoryProvider
	Business          contracts.BusinessProvider
	Fundamentals      contracts.FundamentalsProvider
	LimitUp           contracts.LimitUpProvider
	Pools             contracts.MarketPoolProvider
	HotRank           contracts.HotRankProvider
	FuturesTrend      contracts.FuturesTrendProvider
	FuturesSnapshot   contracts.FuturesSnapshotProvider
	FuturesMembers    contracts.FuturesMembersProvider
	FuturesConsensus  contracts.FuturesConsensusProvider
}
type Entry struct {
	Descriptor   Descriptor
	Capabilities Capabilities
	Probe        Probe
}
type Registry struct{ entries []Entry }

var validID = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// New validates the complete immutable registration set before activation.
// Source removal produces a new registry; running requests keep their snapshot.
func New(entries ...Entry) (*Registry, error) {
	r := &Registry{}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Descriptor.Mode == "" {
			e.Descriptor.Mode = "public"
		}
		if len(e.Descriptor.Kinds) == 0 {
			e.Descriptor.Kinds = []string{"market", "information"}
		}
		switch e.Descriptor.Mode {
		case "public", "credential", "browser", "archive":
		default:
			return nil, fmt.Errorf("invalid source mode %q", e.Descriptor.Mode)
		}
		for _, kind := range e.Descriptor.Kinds {
			if kind != "market" && kind != "information" {
				return nil, fmt.Errorf("invalid source kind %q", kind)
			}
		}
		if !validID.MatchString(e.Descriptor.ID) || strings.TrimSpace(e.Descriptor.Name) == "" {
			return nil, fmt.Errorf("invalid source descriptor %q", e.Descriptor.ID)
		}
		if seen[e.Descriptor.ID] {
			return nil, fmt.Errorf("duplicate source %q", e.Descriptor.ID)
		}
		seen[e.Descriptor.ID] = true
		e.Descriptor = cloneDescriptor(e.Descriptor)
		r.entries = append(r.entries, e)
	}
	return r, nil
}
func cloneDescriptor(d Descriptor) Descriptor {
	d.ArticleHosts = append([]string(nil), d.ArticleHosts...)
	d.Kinds = append([]string(nil), d.Kinds...)
	d.Capabilities = append([]string{}, d.Capabilities...)
	return d
}
func (r *Registry) Entries() []Entry {
	if r == nil {
		return nil
	}
	result := append([]Entry(nil), r.entries...)
	for i := range result {
		result[i].Descriptor = cloneDescriptor(result[i].Descriptor)
	}
	return result
}
func (r *Registry) Catalog() []Descriptor {
	result := []Descriptor{}
	for _, e := range r.Entries() {
		result = append(result, e.Descriptor)
	}
	return result
}
func (r *Registry) Lookup(id string) (Entry, bool) {
	for _, e := range r.Entries() {
		if e.Descriptor.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}
func (r *Registry) SourceID(value string) string {
	id, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(value)), ":")
	if _, ok := r.Lookup(id); ok {
		return id
	}
	return ""
}
