package sector

import (
	"encoding/base64"
	"testing"
)

func rawRadarIdentity(prefix, payload string) string {
	return prefix + base64.RawURLEncoding.EncodeToString([]byte(payload))
}

func TestRadarIdentityPresenceOnlyMissingFieldsUseLegacyInference(t *testing.T) {
	for _, testCase := range []struct{ code, provider string }{{"pt01801039", "tencent"}, {"BK1036", "eastmoney"}} {
		payload := `{"c":"` + testCase.code + `","n":"fixture"}`
		industry, ok := parseRadarIndustryThemeID(rawRadarIdentity(radarIndustryThemePrefix, payload))
		if !ok || industry.Provider != testCase.provider || industry.Dimension != "industry" {
			t.Fatalf("legacy industry=%+v ok=%v", industry, ok)
		}
		fusionPayload := `{"k":"801001","c":"` + testCase.code + `","n":"fixture"}`
		fusion, ok := parseRadarFusionThemeID(rawRadarIdentity(radarFusionThemePrefix, fusionPayload))
		if !ok || fusion.industryRef().Provider != testCase.provider || fusion.industryRef().Dimension != "industry" {
			t.Fatalf("legacy fusion=%+v ok=%v", fusion, ok)
		}
	}
}

func TestRadarIdentityPresenceRejectsExplicitEmptyIdentity(t *testing.T) {
	for _, field := range []string{"p", "d", "P", "D"} {
		for _, value := range []string{`""`, `"  "`, `null`, `false`, `0`, `{}`, `[]`} {
			t.Run(field+"="+value, func(t *testing.T) {
				payload := `{"c":"pt01801039","n":"fixture","` + field + `":` + value + `}`
				if ref, ok := parseRadarIndustryThemeID(rawRadarIdentity(radarIndustryThemePrefix, payload)); ok {
					t.Fatalf("explicit invalid field inferred provider: %+v", ref)
				}
				payload = `{"k":"801001","c":"pt01801039","n":"fixture","` + field + `":` + value + `}`
				if ref, ok := parseRadarFusionThemeID(rawRadarIdentity(radarFusionThemePrefix, payload)); ok {
					t.Fatalf("fusion explicit invalid field inferred provider: %+v", ref)
				}
			})
		}
	}
}

func TestRadarIdentityPresencePreservesExplicitForeignProvider(t *testing.T) {
	payload := `{"c":"pt01801039","n":"fixture","p":"other","d":"industry"}`
	industry, ok := parseRadarIndustryThemeID(rawRadarIdentity(radarIndustryThemePrefix, payload))
	if !ok || industry.Provider != "other" {
		t.Fatalf("foreign industry=%+v ok=%v", industry, ok)
	}
	fusionPayload := `{"k":"801001","c":"pt01801039","n":"fixture","p":"other","d":"industry"}`
	fusion, ok := parseRadarFusionThemeID(rawRadarIdentity(radarFusionThemePrefix, fusionPayload))
	if !ok || fusion.industryRef().Provider != "other" {
		t.Fatalf("foreign fusion=%+v ok=%v", fusion, ok)
	}
	if _, ok := parseRadarIndustryThemeID(rawRadarIdentity(radarIndustryThemePrefix, `{"c":"pt01801039","n":"fixture","p":"tencent","d":"concept"}`)); ok {
		t.Fatal("foreign dimension entered industry route")
	}
}
