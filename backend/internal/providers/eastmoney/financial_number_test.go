package eastmoney

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestFinancialNumberPreservesOnlyFiniteReportedNumbers(t *testing.T) {
	for _, tc := range []struct {
		input string
		valid bool
		want  float64
	}{
		{`0`, true, 0}, {`"0"`, true, 0}, {`" 0 "`, true, 0}, {`-0`, true, 0},
		{`12.5`, true, 12.5}, {`"-8.25"`, true, -8.25}, {`1e3`, true, 1000},
		{`null`, false, 0}, {`""`, false, 0}, {`" "`, false, 0}, {`"--"`, false, 0},
		{`"NaN"`, false, 0}, {`"Inf"`, false, 0}, {`"-Infinity"`, false, 0},
		{`1e999`, false, 0}, {`"1e999"`, false, 0}, {`true`, false, 0},
		{`{}`, false, 0}, {`[]`, false, 0}, {`"not a number"`, false, 0},
	} {
		t.Run(tc.input, func(t *testing.T) {
			var number financialNumber
			if err := json.Unmarshal([]byte(tc.input), &number); err != nil {
				t.Fatal(err)
			}
			if number.valid != tc.valid || number.value != tc.want {
				t.Fatalf("decoded %+v, want valid=%v value=%v", number, tc.valid, tc.want)
			}
		})
	}
	var record struct {
		Number financialNumber `json:"number"`
	}
	if err := json.Unmarshal([]byte(`{}`), &record); err != nil || record.Number.valid {
		t.Fatalf("missing field is valid: %+v %v", record, err)
	}
	var reused financialNumber
	_ = json.Unmarshal([]byte(`42`), &reused)
	_ = json.Unmarshal([]byte(`null`), &reused)
	if reused.valid || reused.value != 0 {
		t.Fatalf("invalid value retained prior value: %+v", reused)
	}
}

func TestFinancialSnapshotFieldValidity(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		fields                         string
		want                           []string
		deductedAmount, deductedGrowth bool
	}{
		{
			name:           "real zeros are available",
			fields:         `"TOTALOPERATEREVE":0,"TOTALOPERATEREVETZ":"0","PARENTNETPROFIT":0,"PARENTNETPROFITTZ":0,"KCFJCXSYJLR":0,"KCFJCXSYJLRTZ":0,"EPSJB":0,"ROEJQ":0,"XSMLL":0,"ZCFZL":0,"MGJYXJJE":0`,
			want:           []string{"revenue", "revenue_yoy", "net_profit", "net_profit_yoy", "deducted_net_profit", "deducted_net_profit_yoy", "eps", "roe", "gross_margin", "debt_ratio", "operating_cash_flow_per_share"},
			deductedAmount: true, deductedGrowth: true,
		},
		{
			name:   "missing null and sentinels remain unknown",
			fields: `"TOTALOPERATEREVE":null,"TOTALOPERATEREVETZ":"","PARENTNETPROFIT":"--","PARENTNETPROFITTZ":"NaN","KCFJCXSYJLR":" ","KCFJCXSYJLRTZ":"Inf","EPSJB":"-Infinity","ROEJQ":1e999,"XSMLL":null`,
			want:   []string{},
		},
		{
			name:   "financial industry gross margin not applicable",
			fields: `"TOTALOPERATEREVE":100,"PARENTNETPROFIT":20,"KCFJCXSYJLR":18,"XSMLL":null,"ROEJQ":12`,
			want:   []string{"revenue", "net_profit", "deducted_net_profit", "roe"}, deductedAmount: true,
		},
		{
			name:   "deducted growth null with valid amount",
			fields: `"KCFJCXSYJLR":0,"KCFJCXSYJLRTZ":null`,
			want:   []string{"deducted_net_profit"}, deductedAmount: true,
		},
		{
			name:   "deducted amount null with valid growth",
			fields: `"KCFJCXSYJLR":null,"KCFJCXSYJLRTZ":0`,
			want:   []string{"deducted_net_profit_yoy"}, deductedGrowth: true,
		},
		{
			name:   "deducted sentinel with valid growth",
			fields: `"KCFJCXSYJLR":"--","KCFJCXSYJLRTZ":2`,
			want:   []string{"deducted_net_profit_yoy"}, deductedGrowth: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(w, `{"success":true,"result":{"data":[{"SECUCODE":"600000.SH","REPORT_DATE":"2026-06-30",%s}]}}`, tc.fields)
			}))
			defer server.Close()
			item, err := NewClient(WithF10BaseURL(server.URL)).StockFundamentals(context.Background(), "600000.SH")
			if err != nil {
				t.Fatal(err)
			}
			if !item.Meta.FieldsKnown || !slices.Equal(item.Meta.AvailableFields, tc.want) {
				t.Fatalf("financial field validity lost: %+v, want %v", item, tc.want)
			}
			if item.DeductedNetProfitAvailable != tc.deductedAmount || item.DeductedNetProfitYearOverYearAvailable != tc.deductedGrowth {
				t.Fatalf("deducted amount/growth are not independently valid: %+v", item)
			}
			if (item.DeductedNetProfitReportDate != "") != tc.deductedAmount {
				t.Fatalf("missing amount acquired a report date: %+v", item)
			}
			for _, field := range tc.want {
				if !item.FieldAvailable(field) {
					t.Fatalf("reported field %s is unavailable", field)
				}
			}
			if _, err := json.Marshal(item); err != nil {
				t.Fatalf("financial JSON cannot be encoded: %v", err)
			}
		})
	}
}
