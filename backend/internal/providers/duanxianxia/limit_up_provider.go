package duanxianxia

import "easy-stock/backend/internal/datasource/service"

// Compatibility names keep callers and persisted provider fixtures working.
// Cross-source selection and merging belong to the data access service.
type RecentLimitUpProvider = service.RecentLimitUpProvider
type LimitUpProvider = service.LimitUpProvider

const kaipanlaThemeLeaderSource = service.ThemeLeaderSource

func NewLimitUpProvider(primary *Service, fallback RecentLimitUpProvider) *LimitUpProvider {
	// A nil *Service must remain a nil interface, rather than a typed nil.
	if primary == nil {
		return service.NewLimitUpProvider(nil, fallback)
	}
	return service.NewLimitUpProvider(primary, fallback)
}
