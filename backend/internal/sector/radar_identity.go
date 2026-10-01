package sector

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

const (
	radarIndustryThemePrefix = "industry:"
	radarFusionThemePrefix   = "fusion:"
)

// Provider and dimension travel with the native code. Code/name-only payloads
// remain readable, but prefix inference is restricted to this compatibility layer.
type radarIndustryThemeRef struct {
	Code      string `json:"c,omitempty"`
	Name      string `json:"n"`
	Provider  string `json:"p,omitempty"`
	Dimension string `json:"d,omitempty"`
}

type radarIndustryLeader struct {
	Symbol        string
	Name          string
	ChangePercent float64
}

type radarFusionThemeRef struct {
	KaipanlaCode      string `json:"k"`
	IndustryCode      string `json:"c,omitempty"`
	IndustryName      string `json:"n"`
	IndustryProvider  string `json:"p,omitempty"`
	IndustryDimension string `json:"d,omitempty"`
}

func normalizeRadarIndustryRef(ref radarIndustryThemeRef) radarIndustryThemeRef {
	ref.Code = strings.TrimSpace(ref.Code)
	ref.Name = strings.TrimSpace(ref.Name)
	ref.Provider = strings.ToLower(strings.TrimSpace(ref.Provider))
	ref.Dimension = strings.ToLower(strings.TrimSpace(ref.Dimension))
	if ref.Provider == "" {
		switch {
		case strings.HasPrefix(ref.Code, "pt"):
			ref.Provider = "tencent"
		case strings.HasPrefix(ref.Code, "BK"):
			ref.Provider = "eastmoney"
		}
	}
	if ref.Dimension == "" {
		ref.Dimension = "industry"
	}
	return ref
}

func radarIndustryThemeID(code string, name string) string {
	return radarIndustryRefID(radarIndustryThemeRef{Code: code, Name: name})
}

func radarIndustryRefID(ref radarIndustryThemeRef) string {
	return encodeRadarThemeRef(radarIndustryThemePrefix, normalizeRadarIndustryRef(ref))
}

func parseRadarIndustryThemeID(id string) (radarIndustryThemeRef, bool) {
	var ref radarIndustryThemeRef
	if !decodeRadarThemeRef(id, radarIndustryThemePrefix, &ref) {
		return radarIndustryThemeRef{}, false
	}
	ref = normalizeRadarIndustryRef(ref)
	return ref, ref.Name != "" && ref.Dimension == "industry"
}

func radarFusionThemeID(kaipanlaCode string, industry radarIndustryThemeRef) string {
	industry = normalizeRadarIndustryRef(industry)
	return encodeRadarThemeRef(radarFusionThemePrefix, radarFusionThemeRef{
		KaipanlaCode: strings.TrimSpace(kaipanlaCode), IndustryCode: industry.Code,
		IndustryName: industry.Name, IndustryProvider: industry.Provider, IndustryDimension: industry.Dimension,
	})
}

func (ref radarFusionThemeRef) industryRef() radarIndustryThemeRef {
	return normalizeRadarIndustryRef(radarIndustryThemeRef{
		Code: ref.IndustryCode, Name: ref.IndustryName, Provider: ref.IndustryProvider, Dimension: ref.IndustryDimension,
	})
}

func parseRadarFusionThemeID(id string) (radarFusionThemeRef, bool) {
	var ref radarFusionThemeRef
	if !decodeRadarThemeRef(id, radarFusionThemePrefix, &ref) {
		return radarFusionThemeRef{}, false
	}
	ref.KaipanlaCode = strings.TrimSpace(ref.KaipanlaCode)
	industry := ref.industryRef()
	ref.IndustryCode, ref.IndustryName = industry.Code, industry.Name
	ref.IndustryProvider, ref.IndustryDimension = industry.Provider, industry.Dimension
	return ref, ref.KaipanlaCode != "" && ref.IndustryName != "" && industry.Dimension == "industry"
}

func encodeRadarThemeRef(prefix string, value any) string {
	payload, _ := json.Marshal(value)
	return prefix + base64.RawURLEncoding.EncodeToString(payload)
}

func decodeRadarThemeRef(id string, prefix string, target any) bool {
	if !strings.HasPrefix(id, prefix) {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(id, prefix))
	if err != nil || len(payload) == 0 {
		return false
	}
	// A missing p/d is a legacy payload; an explicitly present but empty
	// identity is not. Validate the raw presence before unmarshalling into
	// strings, where null and absence otherwise become indistinguishable.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil || fields == nil {
		return false
	}
	for key, raw := range fields {
		// encoding/json matches struct tags case-insensitively, so presence
		// validation must do the same (P/D cannot bypass the guard).
		if !strings.EqualFold(key, "p") && !strings.EqualFold(key, "d") {
			continue
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil || strings.TrimSpace(value) == "" {
			return false
		}
	}
	return json.Unmarshal(payload, target) == nil
}
