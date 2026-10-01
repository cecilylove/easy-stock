package sina

import (
	"context"
	"easy-stock/backend/internal/foundation"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSinaMissingPriceSeriesTypedWithoutHidingSchemaErrors(t *testing.T) {
	for _, body := range []string{"", "null", "callback(null);", "callback([]);"} {
		_, err := parseKLineJSONP(body, "009999.SZ", foundation.SourceMeta{Source: "sina"})
		if !errors.Is(err, foundation.ErrPriceNoData) {
			t.Errorf("expected symbol no-data %q %v", body, err)
		}
	}
	for _, body := range []string{"<html>blocked</html>", `callback([{"day":"bad"}]);`} {
		_, err := parseKLineJSONP(body, "009999.SZ", foundation.SourceMeta{Source: "sina"})
		if err == nil || errors.Is(err, foundation.ErrPriceNoData) {
			t.Errorf("schema/invalid bars mislabelled no-data %q %v", body, err)
		}
	}
}

func TestSinaOptionalAmountIsOnlyAvailableWhenPresent(t *testing.T) {
	lines, err := parseKLineJSONP(`callback([{"day":"2026-09-30 14:51","open":"4.3","high":"4.4","low":"4.2","close":"4.31","volume":"100","amount":"431"},{"day":"2026-09-30 14:52","open":"4.3","high":"4.4","low":"4.2","close":"4.31","volume":"100"}]);`, "000002.SZ", foundation.SourceMeta{Source: "sina"})
	if err != nil || len(lines) != 2 {
		t.Fatalf("parse error %v", err)
	}
	if !foundation.FieldAvailable(lines[0].Meta, "amount") || foundation.FieldAvailable(lines[1].Meta, "amount") || lines[0].Amount != 431 || lines[0].Meta.VolumeUnit != "shares" {
		t.Fatalf("amount presence lost: %+v", lines)
	}
}

func TestClientMonthlyKLineUsesSinaCalendarMonthScale(t *testing.T) {
	for _, period := range []string{"month", "monthly", "103", "7200", " MONTH "} {
		t.Run(period, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				query := r.URL.Query()
				if query.Get("scale") != "7200" || query.Get("symbol") != "sz000002" || query.Get("datalen") != "2" {
					t.Errorf("unexpected monthly request: %s", r.URL.RawQuery)
				}
				_, _ = w.Write([]byte(`callback([{"day":"2026-08-31","open":"3.320","high":"3.390","low":"3.030","close":"3.140","volume":"2787564843"},{"day":"2026-09-30","open":"3.100","high":"4.400","low":"2.980","close":"4.260","volume":"8133390099"}]);`))
			}))
			defer server.Close()
			client := NewClient(WithKLineBaseURL(server.URL))
			lines, err := client.KLine(context.Background(), "000002.SZ", period, 2)
			if err != nil {
				t.Fatal(err)
			}
			if len(lines) != 2 || lines[0].Time.Format("2006-01-02") != "2026-08-31" || lines[1].Time.Format("2006-01-02") != "2026-09-30" {
				t.Fatalf("unexpected monthly dates: %+v", lines)
			}
			last := lines[1]
			if last.Open != 3.1 || last.High != 4.4 || last.Low != 2.98 || last.Close != 4.26 || last.Volume != 8133390099 || last.Symbol != "000002.SZ" || last.Meta.Source != "sina" || last.Meta.SourceURL == "" {
				t.Fatalf("monthly values or provenance changed: %+v", last)
			}
		})
	}
}
