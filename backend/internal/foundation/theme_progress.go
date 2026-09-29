package foundation

import "time"

// ThemeProgress is an immutable published view. Steps can finish independently;
// partial data remains useful when one of the upstreams fails.
type ThemeProgress struct {
	Data         []ThemeOverview     `json:"data"`
	Meta         SourceMeta          `json:"meta"`
	Revision     uint64              `json:"revision"`
	RefreshID    string              `json:"refresh_id"`
	Stage        string              `json:"stage"`
	Refreshing   bool                `json:"refreshing"`
	Steps        map[string]string   `json:"steps"`
	Errors       map[string]string   `json:"errors"`
	Observations []SourceObservation `json:"-"`
}

// SourceObservation is an internal, per-refresh fact; it is not part of the
// response or persisted snapshot. Polling a progress result must not renew it.
type SourceObservation struct {
	Meta      SourceMeta
	SourceID  string
	AttemptAt time.Time
	Failed    bool
}
