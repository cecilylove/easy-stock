package sector

import (
	"context"
	"easy-stock/backend/internal/foundation"
	"testing"
	"time"
)

func TestNativeMembersDoNotIncludeHydratedLimitUpCandidates(t *testing.T) {
	for _, native := range []bool{false, true} {
		stocks := []foundation.BoardStock{}
		if native {
			stocks = append(stocks, foundation.BoardStock{Symbol: "600001.SH", Name: "原始成员", Meta: foundation.SourceMeta{Source: "vendor:members"}})
		}
		mapper := NewMapper(fakeBoardProvider{stocks: map[string][]foundation.BoardStock{"native": stocks}})
		event := foundation.LimitUpEvent{Symbol: "300576.SZ", Name: "光刻胶候选", Date: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC), Streak: 1, Industry: "光刻胶", Concepts: []string{"光刻胶"}, Meta: foundation.SourceMeta{Source: "pool-vendor:events"}}
		node := mapper.buildNode(context.Background(), Node{ID: "photoresist", Name: "光刻胶", BoardKeywords: []string{"光刻胶"}}, []foundation.Board{{Code: "native", Name: "光刻胶", Meta: foundation.SourceMeta{Source: "vendor:board"}}}, nil, []foundation.LimitUpEvent{event}, nil)
		if node.BoardRef == nil || node.BoardRef.Provider != "vendor" {
			t.Fatalf("supplier hardcoded %+v", node.BoardRef)
		}
		if node.MemberSet == nil || node.MemberSet.Kind == "native" {
			t.Fatalf("candidate became native (native fetched=%v) %+v", native, node)
		}
	}
}
