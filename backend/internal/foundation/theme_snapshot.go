package foundation

import "time"

// Theme snapshots are retained upstream observations; provider aliases preserve stored JSON.
type ThemeRankPoint struct {
	TradeDate string  `json:"trade_date"`
	Rank      int     `json:"rank"`
	Strength  float64 `json:"strength"`
}

type ThemeLeader struct {
	Rank   int    `json:"rank"`
	Role   string `json:"role"`
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
}

type ThemeSnapshotItem struct {
	Code          string           `json:"code"`
	Name          string           `json:"name"`
	Rank          int              `json:"rank"`
	Strength      float64          `json:"strength"`
	History       []ThemeRankPoint `json:"history,omitempty"`
	Leaders       []ThemeLeader    `json:"leaders,omitempty"`
	LeadersLoaded bool             `json:"leaders_loaded,omitempty"`
	NoLeaders     bool             `json:"no_leaders,omitempty"`
}

type ThemeSnapshot struct {
	Meta      SourceMeta          `json:"meta,omitempty"`
	ID        string              `json:"id"`
	TradeDate string              `json:"trade_date"`
	FetchedAt time.Time           `json:"fetched_at"`
	Themes    []ThemeSnapshotItem `json:"themes"`
}

type LimitUpPoolSnapshot struct {
	Meta       SourceMeta     `json:"meta,omitempty"`
	ID         string         `json:"id"`
	TradeDate  string         `json:"trade_date"`
	FetchedAt  time.Time      `json:"fetched_at"`
	ModifiedAt time.Time      `json:"modified_at,omitempty"`
	SourceURL  string         `json:"source_url"`
	ETag       string         `json:"etag,omitempty"`
	Events     []LimitUpEvent `json:"events"`
}

func (s ThemeSnapshot) FindTheme(code string) (ThemeSnapshotItem, bool) {
	for _, theme := range s.Themes {
		if theme.Code == code {
			return theme, true
		}
	}
	return ThemeSnapshotItem{}, false
}

type ThemeFetchMeta struct {
	SourceID      string
	PoolSource    string
	LastAttemptAt time.Time
	NextAllowedAt time.Time
	LastSuccessAt time.Time
	RefreshError  string
	Refreshed     bool
	PoolRefreshed bool
	PoolFetchedAt time.Time
	FromCache     bool
	Attempted     bool // This call reserved a new upstream refresh, not a gated cache read.
}
