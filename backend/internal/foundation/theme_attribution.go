package foundation

import "strings"

// Evidence role is separate from supplier identity. Historical snapshots with
// no role retain their original interpretation through the compatibility case.
type ThemeAttributionKind string

const (
	ThemeAttributionPool   ThemeAttributionKind = "pool"
	ThemeAttributionLeader ThemeAttributionKind = "leader"
)

func ThemeEvidenceKind(kind ThemeAttributionKind, source string) ThemeAttributionKind {
	if kind != "" {
		return kind
	}
	if strings.Contains(source, "kaipanla-theme-leader") {
		return ThemeAttributionLeader
	}
	if strings.Contains(source, "kaipanla-limit-up") {
		return ThemeAttributionPool
	}
	return ""
}
