package sector

import (
	"easy-stock/backend/internal/foundation"
	"strings"
)

// Historical snapshots and API source labels retain their original attribution.
// These strings are compatibility metadata, not dependencies on an adapter.
const (
	legacyThemeSnapshotSource  = "duanxianxia:kaipanla"
	legacyThemeSnapshotBaseURL = "https://duanxianxia.com"
)

func themeSnapshotMeta(snapshot foundation.ThemeSnapshot) foundation.SourceMeta {
	meta := snapshot.Meta
	if meta.Source == "" {
		meta.Source = legacyThemeSnapshotSource // Snapshots persisted before source metadata was added.
		meta.SourceURL = legacyThemeSnapshotBaseURL + "/web/platerotat"
	}
	if meta.Provider == "" {
		meta.Provider = strings.SplitN(meta.Source, ":", 2)[0]
	}
	meta.FetchedAt, meta.TradeDate, meta.SnapshotID = snapshot.FetchedAt, snapshot.TradeDate, snapshot.ID
	return meta
}

func themeSnapshotLabel(snapshot foundation.ThemeSnapshot) string {
	if themeSnapshotMeta(snapshot).Provider == "duanxianxia" {
		return "开盘啦"
	}
	return "题材源"
}

func themeSnapshotMemberMethod(snapshot foundation.ThemeSnapshot) string {
	if themeSnapshotMeta(snapshot).Provider == "duanxianxia" {
		return "kaipanla_leader_rank"
	}
	return "theme_leader_rank"
}

// Native theme codes are meaningful only within their supplier. Empty source
// belongs to historical Kaipanla snapshots and legacy callers.
func themeMappingProvider(source string) string {
	if source == "" {
		return "duanxianxia"
	}
	return strings.SplitN(source, ":", 2)[0]
}

func themeMappingCode(source, code string) string {
	if themeMappingProvider(source) == "duanxianxia" {
		return code
	}
	return ""
}
