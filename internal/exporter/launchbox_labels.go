package exporter

import (
	"fmt"
	"strings"
)

// Filename tags are opaque display text. Never infer language, author, patch
// version or meaning from them, and never append a path to distinguish labels.
func launchBoxFileLabel(r Record, discCount int) string {
	stem := titleStem(r.ROM)
	if discCount > 1 {
		stem = withoutTitleDisc(stem)
	}
	plain, _ := splitTitleLabels(stem)
	base, _ := titleSuffixes(plain)
	name := stem
	// Keep the original tag tail, including order, spacing and spelling. If the
	// filename cannot be separated safely, use its basename as-is.
	if strings.HasPrefix(stem, base) {
		if tail := strings.TrimSpace(strings.TrimPrefix(stem, base)); tail != "" {
			name = tail
		}
	}
	if discCount > 1 {
		name += fmt.Sprintf(" [%d discs]", discCount)
	}
	if r.SourceOnly {
		name += " [source set]"
	}
	return name
}
