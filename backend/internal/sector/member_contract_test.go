package sector

import (
	"context"
	"errors"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
	"easy-stock/backend/internal/providers/duanxianxia"
)

type unavailableMemberMapping struct{}

func (unavailableMemberMapping) Build(context.Context, string) (foundation.SectorMap, error) {
	return foundation.SectorMap{}, errors.New("eastmoney unavailable")
}

type recordingIndustryMembers struct {
	codes  []string
	stocks []foundation.BoardStock
}

func (source *recordingIndustryMembers) IndustryStocks(_ context.Context, code string, _ int) ([]foundation.BoardStock, foundation.SourceMeta, error) {
	source.codes = append(source.codes, code)
	return source.stocks, foundation.SourceMeta{Source: "tencent:industry-constituents", Provider: "tencent", NativeCode: code, FieldsKnown: true, MemberSet: &foundation.MemberSetMeta{Kind: "native", Complete: true, Total: len(source.stocks), Returned: len(source.stocks), BoardRef: foundation.BoardRef{Provider: "tencent", NativeCode: code, Dimension: "industry"}}}, nil
}

func TestIndustryIdentityCompatibilityAndProviderRouting(t *testing.T) {
	legacy := encodeRadarThemeRef(radarIndustryThemePrefix, map[string]string{"c": "BK1036", "n": "半导体"})
	ref, ok := parseRadarIndustryThemeID(legacy)
	if !ok || ref.Provider != "eastmoney" || ref.Dimension != "industry" {
		t.Fatalf("legacy ref=%+v", ref)
	}
	members := &recordingIndustryMembers{stocks: []foundation.BoardStock{{Symbol: "688001.SH"}}}
	fallback := fakeRadarFallback{sectorMap: foundation.SectorMap{Name: "半导体", Groups: []foundation.SectorMapGroup{{Nodes: []foundation.SectorMapNode{{Stocks: []foundation.BoardStock{{Symbol: "600001.SH"}}}}}}}}
	provider := NewRadarProvider(nil, fallback, nil, RadarProviderConfig{IndustryStocks: members})
	if _, err := provider.Build(context.Background(), legacy); err != nil {
		t.Fatal(err)
	}
	if len(members.codes) != 0 {
		t.Fatalf("BK entered Tencent route: %v", members.codes)
	}
	foreign := radarIndustryRefID(radarIndustryThemeRef{Code: "pt01801039", Name: "同名行业", Provider: "other", Dimension: "industry"})
	if _, err := provider.Build(context.Background(), foreign); err != nil {
		t.Fatal(err)
	}
	if len(members.codes) != 0 {
		t.Fatalf("foreign provider entered Tencent route: %v", members.codes)
	}
	native := radarIndustryRefID(radarIndustryThemeRef{Code: "pt01801039", Name: "同名行业", Provider: "tencent", Dimension: "industry"})
	if native == foreign {
		t.Fatal("provider identities collapsed")
	}
	fusion, ok := parseRadarFusionThemeID(radarFusionThemeID("801001", ref))
	if !ok || fusion.industryRef().Provider != "eastmoney" {
		t.Fatalf("fusion ref=%+v", fusion)
	}
}

func TestFusionSharesTencentMemberResolverWhenMappingFails(t *testing.T) {
	now := time.Now()
	snapshot := duanxianxia.Snapshot{ID: "fixture", TradeDate: now.Format("2006-01-02"), FetchedAt: now, Themes: []duanxianxia.Theme{{Code: "801001", Name: "芯片", LeadersLoaded: true, Leaders: []duanxianxia.Leader{{Symbol: "600001.SH", Name: "领涨"}}}}}
	members := &recordingIndustryMembers{stocks: []foundation.BoardStock{{Symbol: "688001.SH", Name: "原生成员"}}}
	provider := NewRadarProvider(fakeRadarSource{snapshot: snapshot}, unavailableMemberMapping{}, nil, RadarProviderConfig{IndustryStocks: members})
	ref := radarIndustryThemeRef{Code: "pt01801081", Name: "半导体", Provider: "tencent", Dimension: "industry"}
	industry, err := provider.Build(context.Background(), radarIndustryRefID(ref))
	if err != nil {
		t.Fatal(err)
	}
	fusion, err := provider.BuildSnapshot(context.Background(), radarFusionThemeID("801001", ref), snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members.codes) != 2 || members.codes[0] != ref.Code || members.codes[1] != ref.Code {
		t.Fatalf("routes=%v", members.codes)
	}
	if _, exists := sectorMapStockSymbols(fusion)["688001.SH"]; !exists {
		t.Fatalf("fusion bypassed native resolver: %+v", fusion)
	}
	if industry.Groups[0].Nodes[0].MemberSet == nil || !industry.Groups[0].Nodes[0].MemberSet.Complete {
		t.Fatalf("member metadata lost: %+v", industry)
	}
}

func TestIndustryNativeMembersSurviveEmptyFallbackLayout(t *testing.T) {
	members := &recordingIndustryMembers{stocks: []foundation.BoardStock{{Symbol: "688001.SH"}}}
	provider := NewRadarProvider(nil, fakeRadarFallback{}, nil, RadarProviderConfig{IndustryStocks: members})
	result, err := provider.Build(context.Background(), radarIndustryThemeID("pt1", "fixture"))
	if err != nil || sectorMapStockCount(result) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

func TestIndustryKnownEmptyFieldsDoNotProduceStrength(t *testing.T) {
	items := []foundation.MarketIndustryMomentum{{Code: "pt1", Name: "fixture", Score: 50, Meta: foundation.SourceMeta{FieldsKnown: true}}}
	if result := buildIndustryRadarOverviews(items, foundation.SourceMeta{}, time.Now()); len(result) != 0 {
		t.Fatalf("missing fields produced strength: %+v", result)
	}
}

func TestNativeMembersRemainSeparateFromCatalogCandidates(t *testing.T) {
	result := foundation.SectorMap{Groups: []foundation.SectorMapGroup{{Nodes: []foundation.SectorMapNode{{Name: "半导体", StockSource: "eastmoney:stock-selection", Stocks: []foundation.BoardStock{{Symbol: "600001.SH", Name: "概念候选"}}}}}}}
	meta := foundation.SourceMeta{Source: "tencent:industry-constituents", MemberSet: &foundation.MemberSetMeta{Kind: "native", Complete: true, Total: 1, Returned: 1}}
	mergeIndustryConstituents("pt1", []foundation.BoardStock{{Symbol: "688001.SH", Name: "真实成员"}}, meta, false, &result)
	if len(result.Groups) != 2 || len(result.Groups[0].Nodes[0].Stocks) != 1 || result.Groups[0].Nodes[0].Stocks[0].Symbol != "688001.SH" {
		t.Fatalf("candidate contaminated native set: %+v", result)
	}
	candidate := result.Groups[1].Nodes[0]
	if candidate.Stocks[0].Symbol != "600001.SH" || candidate.MemberSet.Kind != "candidate" || candidate.MemberSet.Complete {
		t.Fatalf("candidate provenance lost: %+v", candidate)
	}
}

func TestPriceDoesNotProveFiveDayReturnAndKnownZeroIsValid(t *testing.T) {
	provider := NewRadarProvider(nil, nil, nil, RadarProviderConfig{})
	pools := map[string][]foundation.BoardStock{"fixture": {
		{Symbol: "600001.SH", Price: 10},
		{Symbol: "600002.SH", Price: 10, Meta: foundation.SourceMeta{FieldsKnown: true, AvailableFields: []string{"price", "change_percent"}}},
		{Symbol: "600003.SH", Price: 10, Meta: foundation.SourceMeta{FieldsKnown: true, AvailableFields: []string{"five_day_change_percent"}}},
		{Symbol: "600004.SH", Price: 10, FiveDayChangePercent: 2, Meta: foundation.SourceMeta{FieldsKnown: true}},
	}}
	changes := provider.strengthChangeLookup(context.Background(), nil, pools)
	if changes["600001.SH"].fiveDayValid || changes["600002.SH"].fiveDayValid || changes["600004.SH"].fiveDayValid || !changes["600003.SH"].fiveDayValid || changes["600003.SH"].fiveDay != 0 {
		t.Fatalf("five-day validity=%+v", changes)
	}
	if industryFieldAvailable(foundation.SourceMeta{FieldsKnown: true}, "change_percent") {
		t.Fatal("known-empty fields treated as all valid")
	}
}
