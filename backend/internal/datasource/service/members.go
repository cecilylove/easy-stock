package service

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
)

type BoardMembers struct {
	sources []contracts.BoardMemberProvider
}

func NewBoardMembers(sources ...contracts.BoardMemberProvider) *BoardMembers {
	return &BoardMembers{sources: append([]contracts.BoardMemberProvider(nil), sources...)}
}
func (s *BoardMembers) SupportsMembers(ref foundation.BoardRef) bool {
	for _, p := range s.sources {
		if p != nil && p.SupportsMembers(ref) {
			return true
		}
	}
	return false
}
func (s *BoardMembers) Members(ctx context.Context, ref foundation.BoardRef, limit int) ([]foundation.BoardStock, foundation.SourceMeta, error) {
	for _, p := range s.sources {
		if p != nil && p.SupportsMembers(ref) {
			return p.Members(ctx, ref, limit)
		}
	}
	return nil, foundation.SourceMeta{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: ref.Provider, Capability: "board-members"}
}
