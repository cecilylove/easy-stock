package eastmoney

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"easy-stock/backend/internal/foundation"
)

func TestLimitUpHistoryPreservesSuccessfulEmptyDatesWithoutSecondFetch(t *testing.T) {
	const days = 40
	dates := integrityTradingDates(days)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"rc":0,"data":{"pool":[]}}`)
	}))
	defer server.Close()
	client := NewClient(WithTopicBaseURL(server.URL))
	var published foundation.LimitUpHistory
	value, err := client.ProgressiveRecentLimitUpHistory(context.Background(), days, func(v foundation.LimitUpHistory) {
		published = foundation.CloneLimitUpHistory(v)
		if len(v.CoveredDates) > 0 {
			v.CoveredDates[0] = "mutated"
		}
	})
	if err != nil || len(value.Events) != 0 || len(value.CoveredDates) != len(dates) || len(value.MissingDates) != 0 || len(published.CoveredDates) != len(dates) || requests.Load() != int32(len(dates)) {
		t.Fatalf("empty dates lost/refetched: %+v %v published=%+v requests=%d", value, err, published, requests.Load())
	}
	if value.CoveredDates[0] != dates[0] {
		t.Fatalf("callback mutated final coverage: %+v", value)
	}
	ordinary, err := client.RecentLimitUpHistory(context.Background(), days)
	if err != nil || len(ordinary.CoveredDates) != len(dates) || requests.Load() != int32(len(dates)) {
		t.Fatalf("ordinary/cache lost coverage: %+v %v requests=%d", ordinary, err, requests.Load())
	}
}
