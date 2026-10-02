package duanxianxia

import (
	"time"

	"easy-stock/backend/internal/foundation"
)

const (
	DefaultBaseURL = "https://duanxianxia.com"
	SourceID       = "duanxianxia:kaipanla"
)

type RankPoint = foundation.ThemeRankPoint
type Leader = foundation.ThemeLeader
type Theme = foundation.ThemeSnapshotItem
type Snapshot = foundation.ThemeSnapshot
type FetchMeta = foundation.ThemeFetchMeta
type LimitUpPoolSnapshot = foundation.LimitUpPoolSnapshot

type SyncState struct {
	LastAttemptAt time.Time
	NextAllowedAt time.Time
	LastSuccessAt time.Time
	LastError     string
}
