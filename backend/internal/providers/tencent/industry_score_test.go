package tencent

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"easy-stock/backend/internal/foundation"
)

func TestIndustryMomentumScoreRequiresAllThreeChanges(t *testing.T) {
	fields := []struct {
		upstream string
		contract string
		value    float64
	}{
		{"bd_zdf", "change_percent", 1},
		{"bd_zdf5", "five_day_change_percent", 2},
		{"bd_zdf20", "twenty_day_change_percent", 3},
	}
	for mask := 0; mask < 8; mask++ {
		t.Run(strconv.Itoa(mask), func(t *testing.T) {
			row := map[string]any{"bd_code": "pt1", "bd_name": "fixture"}
			for index, field := range fields {
				if mask&(1<<index) != 0 {
					row[field.upstream] = field.value
				}
			}
			items, meta := fetchIndustryScoreFixture(t, row)
			item := items[0]
			for index, field := range fields {
				want := mask&(1<<index) != 0
				if foundation.FieldAvailable(item.Meta, field.contract) != want || foundation.FieldAvailable(meta, field.contract) != want {
					t.Fatalf("mask=%d field=%s row=%+v list=%+v", mask, field.contract, item.Meta, meta)
				}
			}
			wantScore := 0.0
			if mask == 7 {
				wantScore = 62.4
			}
			if foundation.FieldAvailable(item.Meta, "score") != (mask == 7) || foundation.FieldAvailable(meta, "score") != (mask == 7) || math.Abs(item.Score-wantScore) > 1e-9 {
				t.Fatalf("incomplete inputs must not produce a score: mask=%d item=%+v list=%+v", mask, item, meta)
			}
			if !item.Meta.FieldsKnown || !meta.FieldsKnown {
				t.Fatalf("field availability is known even with no valid inputs: row=%+v list=%+v", item.Meta, meta)
			}
		})
	}
}

func TestIndustryMomentumScoreRejectsInvalidChanges(t *testing.T) {
	invalid := []struct {
		name  string
		value any
	}{
		{"null", nil},
		{"empty", ""},
		{"whitespace", "  "},
		{"placeholder", "--"},
		{"nan", "NaN"},
		{"positive_infinity", "+Inf"},
		{"negative_infinity", "-Inf"},
		{"overflow", "1e309"},
		{"boolean", false},
	}
	fields := []struct{ upstream, contract string }{
		{"bd_zdf", "change_percent"},
		{"bd_zdf5", "five_day_change_percent"},
		{"bd_zdf20", "twenty_day_change_percent"},
	}
	for _, field := range fields {
		for _, test := range invalid {
			t.Run(field.upstream+"/"+test.name, func(t *testing.T) {
				row := map[string]any{"bd_code": "pt1", "bd_name": "fixture", "bd_zdf": 1, "bd_zdf5": 2, "bd_zdf20": 3}
				row[field.upstream] = test.value
				items, meta := fetchIndustryScoreFixture(t, row)
				item := items[0]
				if foundation.FieldAvailable(item.Meta, field.contract) || foundation.FieldAvailable(meta, field.contract) || foundation.FieldAvailable(item.Meta, "score") || foundation.FieldAvailable(meta, "score") || item.Score != 0 {
					t.Fatalf("invalid %s=%v produced an available field or score: item=%+v list=%+v", field.upstream, test.value, item, meta)
				}
			})
		}
	}
}

func TestIndustryMomentumScoreAcceptsZeroAndKeepsFormula(t *testing.T) {
	for _, test := range []struct {
		name                 string
		change, five, twenty any
		want                 float64
	}{
		{"numeric_zero", 0, 0, 0, 50},
		{"string_zero", "0", "0", "0", 50},
		{"mixed_zero", " -0 ", 0, "0.0", 50},
		{"zero_with_nonzero", 0, "2", 0, 54},
		{"negative", -1, -2, -3, 37.6},
		{"clamp_low", -100, 0, 0, 0},
		{"clamp_high", 100, 0, 0, 100},
	} {
		t.Run(test.name, func(t *testing.T) {
			items, meta := fetchIndustryScoreFixture(t, map[string]any{
				"bd_code": "pt1", "bd_name": "fixture", "bd_zdf": test.change, "bd_zdf5": test.five, "bd_zdf20": test.twenty,
			})
			item := items[0]
			if math.Abs(item.Score-test.want) > 1e-9 || !foundation.FieldAvailable(item.Meta, "score") || !foundation.FieldAvailable(meta, "score") {
				t.Fatalf("valid inputs must retain the score formula: item=%+v list=%+v want=%v", item, meta, test.want)
			}
		})
	}
}

func TestIndustryMomentumScoreListMaskIsIntersection(t *testing.T) {
	complete := map[string]any{"bd_code": "pt1", "bd_name": "complete", "bd_zdf": 0, "bd_zdf5": 0, "bd_zdf20": 0}
	partial := map[string]any{"bd_code": "pt2", "bd_name": "partial", "bd_zdf": 0}
	items, meta := fetchIndustryScoreFixture(t, complete, partial)
	if !foundation.FieldAvailable(items[0].Meta, "score") || foundation.FieldAvailable(items[1].Meta, "score") || foundation.FieldAvailable(meta, "score") || items[0].Score != 50 || items[1].Score != 0 {
		t.Fatalf("list must not claim every row has a score: items=%+v list=%+v", items, meta)
	}
	items, meta = fetchIndustryScoreFixture(t, partial, map[string]any{"bd_code": "pt3", "bd_name": "disjoint", "bd_zdf5": 0})
	if !meta.FieldsKnown || len(meta.AvailableFields) != 0 || foundation.FieldAvailable(meta, "score") || foundation.FieldAvailable(meta, "change_percent") {
		t.Fatalf("known-empty intersection must not mean all fields: items=%+v list=%+v", items, meta)
	}
}

func fetchIndustryScoreFixture(t *testing.T, rows ...map[string]any) ([]foundation.MarketIndustryMomentum, foundation.SourceMeta) {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": rows}); err != nil {
			t.Errorf("encode industry fixture: %v", err)
		}
	}))
	defer upstream.Close()
	client := NewClient(WithIndustryBaseURL(upstream.URL), WithHTTPClient(upstream.Client()))
	items, meta, err := client.IndustryMomentum(context.Background(), 20)
	if err != nil || len(items) != len(rows) {
		t.Fatalf("items=%+v meta=%+v err=%v", items, meta, err)
	}
	return items, meta
}
