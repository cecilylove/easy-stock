package service

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
)

type Knowledge struct {
	id       string
	provider contracts.KnowledgeProvider
}

func NewKnowledge(id string, provider contracts.KnowledgeProvider) *Knowledge {
	return &Knowledge{id: id, provider: provider}
}
func (k *Knowledge) Tree(ctx context.Context) (contracts.KnowledgeTree, error) {
	if k.provider == nil {
		return contracts.KnowledgeTree{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: k.id, Capability: "knowledge-tree"}
	}
	tree, err := k.provider.Tree(ctx)
	tree.SourceID = k.id
	return tree, err
}
func (k *Knowledge) KnowledgeIdentity() contracts.KnowledgeIdentity {
	identity := contracts.KnowledgeIdentity{SourceID: k.id}
	if provider, ok := k.provider.(contracts.KnowledgeIdentityProvider); ok {
		identity.SourceURL = provider.KnowledgeIdentity().SourceURL
	}
	return identity
}
func (k *Knowledge) Document(ctx context.Context, path string) (contracts.KnowledgeDocument, error) {
	if k.provider == nil {
		return contracts.KnowledgeDocument{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: k.id, Capability: "knowledge-document"}
	}
	return k.provider.Document(ctx, path)
}
