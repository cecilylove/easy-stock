package service

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"errors"
	"time"
)

type News struct {
	sourceID string
	provider contracts.NewsProvider
	observe  func(foundation.SourceObservation)
}

func NewNews(id string, provider contracts.NewsProvider, observe func(foundation.SourceObservation)) *News {
	return &News{sourceID: id, provider: provider, observe: observe}
}
func (s *News) LatestNews(ctx context.Context, limit int) ([]foundation.NewsItem, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.provider == nil {
		return nil, &contracts.Error{Kind: contracts.Unsupported, SourceID: s.sourceID, Capability: "news"}
	}
	started := time.Now()
	items, err := s.provider.LatestNews(ctx, limit)
	if s.observe != nil && !errors.Is(ctx.Err(), context.Canceled) && !errors.Is(err, context.Canceled) {
		meta := foundation.SourceMeta{Source: s.sourceID, Capability: "news"}
		for _, item := range items {
			if item.Meta.FetchedAt.After(meta.FetchedAt) {
				meta = item.Meta
				meta.Capability = "news"
			}
		}
		if err == nil && len(items) == 0 {
			meta.FetchedAt = started
		}
		s.observe(foundation.SourceObservation{SourceID: s.sourceID, Capability: "news", AttemptAt: started, Meta: meta, Failed: err != nil})
	}
	return items, err
}
