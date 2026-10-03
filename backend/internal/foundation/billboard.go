package foundation

import "strings"

// NormalizeBillboardSeatName matches published spelling variants. It is not
// evidence that two accounts or funding identities are the same.
func NormalizeBillboardSeatName(value string) string {
	return strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "", "（", "(", "）", ")", "有限责任公司", "", "股份有限公司", "", "有限公司", "").Replace(strings.TrimSpace(value))
}
