package sina

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
