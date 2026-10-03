package foundation

import (
	"errors"
	"sort"
	"time"
)

// LimitUpHistory records successful full pools independently of their events.
// A covered empty date is a real zero-event pool; a missing date is never one.
type LimitUpHistory struct {
	Events         []LimitUpEvent `json:"events"`
	RequestedDates []string       `json:"requested_dates"`
	CoveredDates   []string       `json:"covered_dates"`
	MissingDates   []string       `json:"missing_dates"`
	Meta           SourceMeta     `json:"meta"`
}

func CloneLimitUpHistory(value LimitUpHistory) LimitUpHistory {
	value.Events = append([]LimitUpEvent(nil), value.Events...)
	for i := range value.Events {
		value.Events[i].Concepts = append([]string(nil), value.Events[i].Concepts...)
		value.Events[i].Meta = CloneSourceMeta(value.Events[i].Meta)
	}
	value.RequestedDates = append([]string(nil), value.RequestedDates...)
	value.CoveredDates = append([]string(nil), value.CoveredDates...)
	value.MissingDates = append([]string(nil), value.MissingDates...)
	value.Meta = CloneSourceMeta(value.Meta)
	return value
}

// LimitUpHistoryFromEvents is the compatibility path for events-only suppliers.
// It cannot discover a successful empty day unless coverage metadata/error says so.
func LimitUpHistoryFromEvents(events []LimitUpEvent, err error) LimitUpHistory {
	value := CloneLimitUpHistory(LimitUpHistory{Events: events})
	covered := map[string]bool{}
	missing := map[string]bool{}
	explicit := false
	for _, event := range events {
		if value.Meta.Source == "" && event.Meta.Source != "" {
			value.Meta = CloneSourceMeta(event.Meta)
		}
		if len(event.Meta.CoveredDates) > 0 {
			explicit = true
		}
		for _, date := range event.Meta.CoveredDates {
			covered[date] = true
		}
		for _, date := range event.Meta.MissingIDs {
			if event.Meta.Partial {
				missing[date] = true
			}
		}
	}
	var partial *LimitUpCoverageError
	if errors.As(err, &partial) {
		value.RequestedDates = append([]string(nil), partial.RequestedDates...)
		value.CoveredDates = append([]string(nil), partial.CoveredDates...)
		value.MissingDates = append([]string(nil), partial.MissingDates...)
	} else {
		if !explicit {
			for _, event := range events {
				if !event.Meta.Partial && !event.Date.IsZero() && (event.Meta.TradeDate == "" || event.Meta.TradeDate == event.Date.Format("2006-01-02")) {
					covered[event.Date.Format("2006-01-02")] = true
				}
			}
		}
		for date := range missing {
			delete(covered, date)
		}
		for date := range covered {
			value.CoveredDates = append(value.CoveredDates, date)
		}
		for date := range missing {
			value.MissingDates = append(value.MissingDates, date)
		}
		sort.Strings(value.CoveredDates)
		sort.Strings(value.MissingDates)
		value.RequestedDates = append(append([]string(nil), value.CoveredDates...), value.MissingDates...)
		sort.Strings(value.RequestedDates)
	}
	value.Meta.Partial = value.Meta.Partial || err != nil || len(value.MissingDates) > 0
	return StampLimitUpHistory(value)
}

// StampLimitUpHistory carries coverage through legacy events-only callbacks.
// It clones before annotating so published/cached inputs remain immutable.
func StampLimitUpHistory(value LimitUpHistory) LimitUpHistory {
	value = CloneLimitUpHistory(value)
	value.Meta.CoveredDates = append([]string(nil), value.CoveredDates...)
	value.Meta.MissingIDs = append([]string(nil), value.MissingDates...)
	if len(value.MissingDates) > 0 {
		value.Meta.Partial = true
	}
	for i := range value.Events {
		value.Events[i].Meta.CoveredDates = append([]string(nil), value.CoveredDates...)
	}
	return value
}

// LimitUpRequestedDates retains the legacy lookback meaning: calendar days,
// filtered by the A-share exchange calendar, not a count of trading sessions.
func LimitUpRequestedDates(now time.Time, days int) []string {
	if days <= 0 {
		days = 12
	}
	now = now.In(time.FixedZone("Asia/Shanghai", 8*3600))
	dates := make([]string, 0, days)
	for offset := days - 1; offset >= 0; offset-- {
		date := now.AddDate(0, 0, -offset)
		if IsAStockTradingDay(date) {
			dates = append(dates, date.Format("2006-01-02"))
		}
	}
	return dates
}
