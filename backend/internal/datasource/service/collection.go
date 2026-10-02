package service

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/datasource/registry"
	"easy-stock/backend/internal/foundation"
)

type Collections struct{ sources *registry.Registry }

func NewCollections(sources *registry.Registry) *Collections { return &Collections{sources: sources} }
func (s *Collections) capabilities(id string) registry.Capabilities {
	entry, ok := s.sources.Lookup(id)
	if !ok || !entry.Descriptor.Enabled || !entry.Descriptor.Implemented {
		return registry.Capabilities{}
	}
	return entry.Capabilities
}
func (s *Collections) CollectBrowser(ctx context.Context, req contracts.BrowserCollectionRequest) (contracts.BrowserCollection, error) {
	p := s.capabilities(req.SourceID).BrowserCollection
	if p == nil {
		return contracts.BrowserCollection{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: req.SourceID, Capability: "browser-collection"}
	}
	return p.CollectBrowser(ctx, req)
}
func (s *Collections) DiscoverAuthorLinks(ctx context.Context, req contracts.AuthorLinksRequest) (contracts.AuthorLinks, error) {
	p := s.capabilities(req.SourceID).AuthorLinks
	if p == nil {
		return contracts.AuthorLinks{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: req.SourceID, Capability: "author-links"}
	}
	return p.DiscoverAuthorLinks(ctx, req)
}
func (s *Collections) FetchAuthorizedArticle(ctx context.Context, req contracts.AuthorizedArticleRequest) (foundation.Article, error) {
	p := s.capabilities(req.SourceID).AuthorizedArticle
	if p == nil {
		return foundation.Article{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: req.SourceID, Capability: "authorized-article"}
	}
	return p.FetchAuthorizedArticle(ctx, req)
}
