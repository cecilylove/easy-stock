package tencent

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"strings"
)

// Native board support is a supplier constraint, not a sector business rule.
func (c *Client) SupportsMembers(ref foundation.BoardRef) bool {
	return ref.Provider == "tencent" && ref.Dimension == "industry" && strings.HasPrefix(ref.NativeCode, "pt") && ref.ClassificationVersion == ""
}
func (c *Client) Members(ctx context.Context, ref foundation.BoardRef, limit int) ([]foundation.BoardStock, foundation.SourceMeta, error) {
	if !c.SupportsMembers(ref) {
		return nil, foundation.SourceMeta{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: "tencent", Capability: "board-members"}
	}
	return c.IndustryStocks(ctx, ref.NativeCode, limit)
}
