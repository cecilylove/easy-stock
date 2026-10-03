package httpapi

import (
	"easy-stock/backend/internal/foundation"
	"testing"
	"time"
)

func TestExplicitCacheJoinSkipNeverCreatesSourceHealthAttempt(t *testing.T) {
	for _, state := range []string{"cache", "joined", "skipped"} {
		tracker := newSourceHealthTracker()
		meta := foundation.SourceMeta{Source: "ths:billboard-labels", Capability: "billboard-labels", FetchedAt: time.Now(), ExecutionState: state}
		tracker.success(meta)
		tracker.observe(foundation.SourceObservation{SourceID: "ths", Capability: "billboard-labels", Meta: meta, AttemptAt: time.Now(), Failed: true})
		actual := sourceByID(t, tracker.snapshot(time.Now()), "ths")
		if actual.Status != "unknown" || actual.LastSuccess != nil || actual.LastFailure != nil || len(actual.Capabilities) != 0 {
			t.Fatalf("%s created network health %+v", state, actual)
		}
	}
}
