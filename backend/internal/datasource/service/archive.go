package service

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
)

type Archive struct {
	id       string
	provider contracts.ArchiveProvider
}

func NewArchive(id string, provider contracts.ArchiveProvider) *Archive {
	return &Archive{id: id, provider: provider}
}
func (a *Archive) FetchAuthors(ctx context.Context, req contracts.ArchiveAuthorsRequest) (contracts.ArchiveAuthorsResult, error) {
	if a.provider == nil {
		return contracts.ArchiveAuthorsResult{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: a.id, Capability: "review-archive"}
	}
	return a.provider.FetchAuthors(ctx, req)
}
func (a *Archive) FetchArchiveArticle(ctx context.Context, req contracts.ArchiveArticleRequest) (contracts.ArchiveArticleResult, error) {
	if a.provider == nil {
		return contracts.ArchiveArticleResult{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: a.id, Capability: "review-archive"}
	}
	return a.provider.FetchArchiveArticle(ctx, req)
}
