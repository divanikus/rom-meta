// Package titlematch keeps identity-bearing symbols in provider title keys.
package titlematch

import (
	"strings"
	"unicode"
)

// Key tolerates formatting punctuation and word boundaries without erasing
// plus editions or a terminal prime. Apostrophes inside words remain formatting.
func Key(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for i, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '&':
			b.WriteString(" and ")
		case r == '+' || r == '＋':
			b.WriteString(" plus ")
		case (r == '\'' || r == '’' || r == '′') && terminalPrime(value, i, r):
			b.WriteString(" prime ")
		default:
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func terminalPrime(value string, at int, mark rune) bool {
	if at == 0 {
		return false
	}
	before := []rune(strings.TrimSpace(value[:at]))
	if len(before) == 0 {
		return false
	}
	previous := before[len(before)-1]
	if !unicode.IsLetter(previous) && !unicode.IsDigit(previous) && previous != '\'' && previous != '’' && previous != '′' {
		return false
	}
	after := strings.TrimSpace(value[at+len(string(mark)):])
	after = strings.TrimLeft(after, "'’′")
	return after == "" || strings.HasPrefix(after, "(") || strings.HasPrefix(after, "[")
}
