package eastmoney

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"easy-stock/backend/internal/foundation"
)

func TestIndustryScoreRequiresAllValidFormulaInputs(t *testing.T) {
	for _, tc := range []struct {
		name, missing string
		valid         bool
	}{
		{"full", "", true}, {"change", "f3", false}, {"five_day", "f109", false}, {"twenty_day", "f24", false}, {"rising", "f104", false}, {"falling", "f105", false}, {"flow", "f62", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := `"f12":"BK001","f14":"行业","f3":0,"f109":0,"f24":0,"f104":0,"f105":0,"f62":0`
			if tc.missing != "" {
				row = strings.Replace(row, `"`+tc.missing+`":0`, `"`+tc.missing+`":null`, 1)
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"rc":0,"data":{"diff":[{%s}]}}`, row) }))
			defer upstream.Close()
			client := NewClient(WithQuoteBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
			items, meta, err := client.IndustryMomentum(context.Background(), 5)
			if err != nil || len(items) != 1 {
				t.Fatalf("items=%+v err=%v", items, err)
			}
			if !foundation.FieldAvailable(meta, "score") {
				t.Fatal("normal source schema must declare derived score capability")
			}
			if foundation.FieldAvailable(items[0].Meta, "score") != tc.valid {
				t.Fatalf("score validity=%+v", items[0].Meta)
			}
			if tc.valid && items[0].Score != 50 {
				t.Fatalf("real zero inputs must yield neutral formula score, got=%v", items[0].Score)
			}
			if !tc.valid && items[0].Score != 0 {
				t.Fatalf("missing input generated fake neutral score: %v", items[0].Score)
			}
		})
	}
}

func TestIndustryScoreAvailableOnNormalIndustryButNotFundOnlyFallback(t *testing.T) {
	normal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"rc":0,"data":{"diff":[{"f12":"BK001","f14":"行业","f3":1,"f109":2,"f24":3,"f104":6,"f105":4,"f62":100000000}]}}`)
	}))
	defer normal.Close()
	client := NewClient(WithQuoteBaseURL(normal.URL), WithHTTPClient(normal.Client()))
	items, _, err := client.IndustryMomentum(context.Background(), 5)
	if err != nil || len(items) != 1 || !foundation.FieldAvailable(items[0].Meta, "score") || items[0].Score != scoreMomentum(1, 2, 3, 100000000, 6, 4) {
		t.Fatalf("normal industry derived score lost: %+v %v", items, err)
	}
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/qt/clist/get" {
			fmt.Fprint(w, `{"rc":0,"data":{"diff":[]}}`)
			return
		}
		fmt.Fprint(w, `{"rc":0,"data":{"diff":[{"f12":"BK001","f14":"行业","f62":100000000}]}}`)
	}))
	defer fallback.Close()
	client = NewClient(WithQuoteBaseURL(fallback.URL), WithDataBaseURL(fallback.URL), WithHTTPClient(fallback.Client()))
	items, meta, err := client.IndustryMomentum(context.Background(), 5)
	if err != nil || len(items) != 1 || foundation.FieldAvailable(meta, "score") || foundation.FieldAvailable(items[0].Meta, "score") {
		t.Fatalf("fund-only fallback pretends score capability: %+v %+v %v", items, meta, err)
	}
}
