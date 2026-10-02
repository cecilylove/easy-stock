package sector

import (
	"context"
	"easy-stock/backend/internal/foundation"
	"fmt"
	"strings"
)

// This adapter is only the compatibility boundary for IndustryStocks, whose
// historical signature accepted a Tencent code and could not express a supplier.
// New assembly injects BoardMembers and capability support directly.
type legacyTencentIndustryMembers struct{ source IndustryConstituentSource }

func (a legacyTencentIndustryMembers) SupportsMembers(ref foundation.BoardRef) bool {
	return ref.Provider == "tencent" && ref.Dimension == "industry" && strings.HasPrefix(ref.NativeCode, "pt")
}
func (a legacyTencentIndustryMembers) Members(ctx context.Context, ref foundation.BoardRef, limit int) ([]foundation.BoardStock, foundation.SourceMeta, error) {
	if !a.SupportsMembers(ref) {
		return nil, foundation.SourceMeta{}, fmt.Errorf("unsupported legacy industry identity")
	}
	return a.source.IndustryStocks(ctx, ref.NativeCode, limit)
}
func memberProviderLabel(provider string) string {
	switch provider {
	case "tencent":
		return "腾讯"
	case "eastmoney":
		return "东方财富"
	default:
		return provider
	}
}
