package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"easy-stock/backend/internal/foundation"
)

type fusedObservedThemeProvider struct {
	observations []foundation.SourceObservation
	err          error
}

func (p fusedObservedThemeProvider) Overviews(context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, error) {
	return []foundation.ThemeOverview{{Theme: "bank", Name: "银行"}}, foundation.SourceMeta{Source: "radar:fusion"}, p.err
}

func (p fusedObservedThemeProvider) OverviewsWithObservations(ctx context.Context) ([]foundation.ThemeOverview, foundation.SourceMeta, []foundation.SourceObservation, error) {
	items, meta, err := p.Overviews(ctx)
	return items, meta, p.observations, err
}

func TestPlainThemeRouteRecordsComponentOutcomesDespiteFusedMetadata(t *testing.T) {
	now := time.Now()
	s := NewServer(Config{ThemeOverview: fusedObservedThemeProvider{observations: []foundation.SourceObservation{
		{Meta: foundation.SourceMeta{Source: "tencent:industry-rank", FetchedAt: now}},
		{SourceID: "duanxianxia", AttemptAt: now, Failed: true},
	}}})
	defer s.Close()
	recorder := httptest.NewRecorder()
	s.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/themes/overview", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	items := s.sourceHealth.snapshot(time.Now())
	if sourceByID(t, items, "tencent").Status != "available" || sourceByID(t, items, "duanxianxia").Status != "degraded" || sourceByID(t, items, "eastmoney").Status != "unknown" {
		t.Fatalf("component outcomes lost or invented: %+v", items)
	}
}

func TestPlainThemeRouteDoesNotRenewCachedObservation(t *testing.T) {
	old := time.Now().Add(-11 * time.Minute)
	s := NewServer(Config{ThemeOverview: fusedObservedThemeProvider{observations: []foundation.SourceObservation{{Meta: foundation.SourceMeta{Source: "tencent:industry-rank", FetchedAt: old}}}}})
	defer s.Close()
	for range 2 {
		recorder := httptest.NewRecorder()
		s.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/themes/overview", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status=%d", recorder.Code)
		}
	}
	entry := sourceByID(t, s.sourceHealth.snapshot(time.Now()), "tencent")
	if entry.Status != "unknown" || entry.CheckedAt == nil || !entry.CheckedAt.Equal(old) {
		t.Fatalf("cached read refreshed observation: %+v", entry)
	}
}

func TestPlainThemeRouteRecordsSingleSourceProvider(t *testing.T) {
	s := NewServer(Config{ThemeOverview: observedPlainThemeProvider{}})
	defer s.Close()
	recorder := httptest.NewRecorder()
	s.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/themes/overview", nil))
	if recorder.Code != http.StatusOK || sourceByID(t, s.sourceHealth.snapshot(time.Now()), "tencent").Status != "available" {
		t.Fatalf("single-source plain request was not observed: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestPlainThemeRouteObservesCompletedSourcesOnDeadlineButNotCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		name := "service deadline"
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		if canceled {
			cancel()
			name = "caller cancellation"
			ctx, cancel = context.WithCancel(context.Background())
			cancel()
		}
		t.Run(name, func(t *testing.T) {
			defer cancel()
			now := time.Now()
			s := NewServer(Config{ThemeOverview: fusedObservedThemeProvider{
				err: ctx.Err(), observations: []foundation.SourceObservation{
					{Meta: foundation.SourceMeta{Source: "tencent:industry-rank", FetchedAt: now}},
					{SourceID: "duanxianxia", AttemptAt: now, Failed: true},
				},
			}})
			defer s.Close()
			recorder := httptest.NewRecorder()
			s.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/themes/overview", nil).WithContext(ctx))
			items := s.sourceHealth.snapshot(time.Now())
			tencent, kaipanla := sourceByID(t, items, "tencent"), sourceByID(t, items, "duanxianxia")
			if recorder.Code != http.StatusBadGateway {
				t.Fatalf("status=%d", recorder.Code)
			}
			if canceled {
				if tencent.Status != "unknown" || kaipanla.Status != "unknown" {
					t.Fatalf("caller cancellation recorded outcomes: %+v", items)
				}
			} else if tencent.Status != "available" || kaipanla.Status != "degraded" {
				t.Fatalf("deadline discarded completed outcomes: %+v", items)
			}
		})
	}
}
