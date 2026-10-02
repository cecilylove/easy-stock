package service

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"sync"
)

// HotRanks runs independent platform rankings. Their positions and errors are
// never averaged, merged or interpreted as equivalent heat measures.
type HotRanks struct{ sources []contracts.HotRankProvider }

func NewHotRanks(sources ...contracts.HotRankProvider) *HotRanks {
	return &HotRanks{sources: append([]contracts.HotRankProvider(nil), sources...)}
}
func (s *HotRanks) HotStockRanks(ctx context.Context, limit int) []foundation.HotStockRankList {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	lists := make([]foundation.HotStockRankList, len(s.sources))
	var group sync.WaitGroup
	for i, source := range s.sources {
		if source == nil {
			continue
		}
		group.Add(1)
		go func(i int, source contracts.HotRankProvider) {
			defer group.Done()
			lists[i] = source.HotRank(ctx, limit)
		}(i, source)
	}
	group.Wait()
	return lists
}
