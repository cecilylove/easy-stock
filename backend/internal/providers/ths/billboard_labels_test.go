package ths

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const fixturePage = `<div class="stockcont" stockcode="000001"><table>
<td class="tl rel"><a title="中国 银河证券股份有限公司（北京）"></a><label class="label red">游资&amp;观察</label></td>
<td class="tl rel"><a title="机构专用"></a></td>
<td class="tl rel"><a title="无标签席位"></a></td>
</table></div><div class="stockcont" stockcode="000002"><td class="tl rel"><a title="其它股票席位"></a><label class="label">其它</label></td></div>`

func localClient(t *testing.T, handler http.HandlerFunc) *BillboardLabelClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := NewBillboardLabelClient()
	client.baseURL = server.URL
	client.http = server.Client()
	return client
}

func TestFetchGBKNormalizationAndCacheIsolation(t *testing.T) {
	var calls atomic.Int32
	encoded, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte(fixturePage))
	if err != nil {
		t.Fatal(err)
	}
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/ifmarket/lhbggxq/report/2026-09-30/" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Referer") == "" || r.Header.Get("User-Agent") == "" {
			t.Error("missing public page headers")
		}
		_, _ = w.Write(encoded)
	})
	clock := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return clock }
	labels, meta, err := client.Fetch(context.Background(), "000001.SZ", "2026-09-30")
	if err != nil {
		t.Fatal(err)
	}
	key := NormalizeBillboardSeatName("中国银河证券有限责任公司(北京)")
	if labels[key] != "游资&观察" || labels["机构专用"] != "机构" || len(labels) != 2 {
		t.Fatalf("labels %#v", labels)
	}
	if meta.Source != "ths:billboard-labels" || meta.Capability != "billboard-labels" || meta.ExecutionState != "fetched" || !meta.FetchedAt.Equal(clock) {
		t.Fatalf("meta %#v", meta)
	}
	labels[key] = "caller changed"
	clock = clock.Add(time.Hour)
	labels, cachedMeta, err := client.Fetch(context.Background(), "000001", "2026-09-30")
	if err != nil {
		t.Fatal(err)
	}
	if labels[key] != "游资&观察" || calls.Load() != 1 || cachedMeta.ExecutionState != "cache" || !cachedMeta.FetchedAt.Equal(meta.FetchedAt) {
		t.Fatalf("cache leaked or renewed: %#v %#v calls %d", labels, cachedMeta, calls.Load())
	}
	clock = meta.FetchedAt.Add(billboardCacheTTL)
	_, renewed, err := client.Fetch(context.Background(), "000001", "2026-09-30")
	if err != nil || calls.Load() != 2 || renewed.ExecutionState != "fetched" || !renewed.FetchedAt.Equal(clock) {
		t.Fatalf("expiry: %#v %v calls %d", renewed, err, calls.Load())
	}
}

func TestFetchValidationAndFailureNotCached(t *testing.T) {
	var calls atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(fixturePage))
	})
	for _, input := range [][2]string{{"abcdef", "2026-09-30"}, {"000001", "../2026-09-30"}, {"000001", "2026-02-30"}} {
		if _, meta, err := client.Fetch(context.Background(), input[0], input[1]); err == nil || meta.ExecutionState != "skipped" {
			t.Fatalf("accepted %v", input)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid request reached HTTP")
	}
	if _, meta, err := client.Fetch(context.Background(), "000001", "2026-09-30"); err == nil || meta.Source != "ths:billboard-labels" || meta.ExecutionState != "fetched" {
		t.Fatalf("failure meta %#v %v", meta, err)
	}
	if _, _, err := client.Fetch(context.Background(), "000001", "2026-09-30"); err != nil || calls.Load() != 2 {
		t.Fatalf("failure cached %v", err)
	}
}

type waitObservedContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *waitObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestFetchWaiterCancellationAndRequestCancellation(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		select {
		case <-release:
			_, _ = w.Write([]byte(fixturePage))
		case <-r.Context().Done():
		}
	})
	owner := make(chan error, 1)
	go func() { _, _, err := client.Fetch(context.Background(), "000001", "2026-09-30"); owner <- err }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	observed := &waitObservedContext{Context: ctx, waiting: make(chan struct{})}
	waiting := make(chan error, 1)
	go func() {
		_, meta, err := client.Fetch(observed, "000002", "2026-09-30")
		if meta.ExecutionState != "skipped" {
			t.Errorf("cancelled waiter meta %#v", meta)
		}
		waiting <- err
	}()
	// Guarantee the caller has reached the in-flight select before cancelling,
	// rather than merely exercising the Fetch preflight context check.
	<-observed.waiting
	cancel()
	select {
	case err := <-waiting:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiter %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled waiter blocked on owner")
	}
	close(release)
	if err := <-owner; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("coalescing calls %d", calls.Load())
	}

	requestEntered := make(chan struct{})
	client2 := localClient(t, func(w http.ResponseWriter, r *http.Request) { close(requestEntered); <-r.Context().Done() })
	ctx2, cancel2 := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, _, err := client2.Fetch(ctx2, "000001", "2026-09-30"); done <- err }()
	<-requestEntered
	cancel2()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("request %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("HTTP ignores cancellation")
	}
}

func TestFetchBoundedDateCacheAndEmptyStock(t *testing.T) {
	var calls atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); _, _ = w.Write([]byte(fixturePage)) })
	for day := 1; day <= billboardCachePages+2; day++ {
		date := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, day).Format("2006-01-02")
		labels, _, err := client.Fetch(context.Background(), "600000.SH", date)
		if err != nil || len(labels) != 0 {
			t.Fatalf("unmatched stock must stay empty: %v %v", labels, err)
		}
	}
	if len(client.pages) != billboardCachePages {
		t.Fatalf("unbounded cache %d", len(client.pages))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestHTTPBudgetNeverExtendsCallerDeadline(t *testing.T) {
	for _, callerBudget := range []time.Duration{0, 50 * time.Millisecond} {
		client := NewBillboardLabelClient()
		client.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			deadline, ok := r.Context().Deadline()
			if !ok || time.Until(deadline) > billboardHTTPBudget {
				t.Fatal("request lacks bounded HTTP context")
			}
			// Shared transport owns an independent bounded context; each viewer
			// retains its own deadline and the last viewer cancels the fetch.
			return nil, errors.New("local transport failure")
		})}
		ctx := context.Background()
		if callerBudget > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, callerBudget)
			defer cancel()
		}
		if _, _, err := client.Fetch(ctx, "000001", "2026-09-30"); err == nil {
			t.Fatal("transport error suppressed")
		}
	}
}

func TestJoinedFetchDoesNotClaimAnotherAttempt(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
		_, _ = w.Write([]byte(fixturePage))
	})
	owner := make(chan foundation.SourceMeta, 1)
	go func() { _, meta, _ := client.Fetch(context.Background(), "000001", "2026-09-30"); owner <- meta }()
	<-entered
	observed := &waitObservedContext{Context: context.Background(), waiting: make(chan struct{})}
	joined := make(chan foundation.SourceMeta, 1)
	go func() {
		_, meta, err := client.Fetch(observed, "000002", "2026-09-30")
		if err != nil {
			t.Errorf("join %v", err)
		}
		joined <- meta
	}()
	<-observed.waiting
	close(release)
	fetched, reused := <-owner, <-joined
	// The first surviving receiver owns the observation. Scheduler order does
	// not guarantee the original viewer receives a shared result first.
	states := map[string]int{fetched.ExecutionState: 1}
	states[reused.ExecutionState]++
	if states["fetched"] != 1 || states["joined"] != 1 || !reused.FetchedAt.Equal(fetched.FetchedAt) {
		t.Fatalf("owner %#v joined %#v", fetched, reused)
	}
}

func TestInvalidHTMLNotCachedAsSuccess(t *testing.T) {
	var calls atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = fmt.Fprint(w, "<html>captcha or login challenge</html>")
			return
		}
		_, _ = w.Write([]byte(fixturePage))
	})
	if _, meta, err := client.Fetch(context.Background(), "000001", "2026-09-30"); contracts.Kind(err) != contracts.InvalidResponse || meta.ExecutionState != "fetched" {
		t.Fatalf("invalid HTML %#v %v", meta, err)
	}
	if _, _, err := client.Fetch(context.Background(), "000001", "2026-09-30"); err != nil || calls.Load() != 2 {
		t.Fatalf("invalid page cached %v", err)
	}
}

func TestHTTPBodyBudget(t *testing.T) {
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, string(make([]byte, billboardBodyLimit+1)))
	})
	if _, _, err := client.Fetch(context.Background(), "000001", "2026-09-30"); err == nil {
		t.Fatal("oversized response accepted")
	}
}
