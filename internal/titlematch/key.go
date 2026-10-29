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
			before, after := strings.TrimSpace(value[:i]), strings.TrimSpace(value[i+len(string(r)):])
			// A spaced conjunction retains both component names; a suffix or
			// attached edition marker remains significant.
			if before != "" && after != "" && i > 0 && unicode.IsSpace([]rune(value[:i])[len([]rune(value[:i]))-1]) && !strings.HasPrefix(after, "(") && !strings.HasPrefix(after, "[") && !strings.HasPrefix(after, "+") && !strings.HasPrefix(after, "＋") {
				b.WriteByte(' ')
			} else {
				b.WriteString(" plus ")
			}
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
	// A closing quotation around a subtitle is not an edition prime.
	for i, r := range value[:at] {
		if r != '\'' && r != '‘' && r != '"' {
			continue
		}
		if i > 0 {
			previous := []rune(value[:i])
			if !unicode.IsSpace(previous[len(previous)-1]) && previous[len(previous)-1] != '-' && previous[len(previous)-1] != ':' {
				continue
			}
		}
		next := []rune(value[i+len(string(r)):])
		if len(next) > 0 && unicode.IsLetter(next[0]) {
			return false
		}
	}
	after := strings.TrimSpace(value[at+len(string(mark)):])
	after = strings.TrimLeft(after, "'’′")
	return after == "" || strings.HasPrefix(after, "(") || strings.HasPrefix(after, "[")
}

// CompatibleSymbols prevents a generic card title or alias from erasing an
// explicit release edition while allowing regional wording changes.
func CompatibleSymbols(a, b string) bool {
	signature := func(value string) string {
		var out []string
		for _, word := range strings.Fields(Key(value)) {
			if word == "plus" || word == "prime" {
				out = append(out, word)
			}
		}
		return strings.Join(out, " ")
	}
	return signature(a) == signature(b)
}
