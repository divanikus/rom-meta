package medianames

import "strings"

// SourceVariant preserves explicit provider categories; it does not inspect pixels.
func SourceVariant(sourceType string) string {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "box - front", "box - back":
		return "original"
	case "box - front - reconstructed", "box - back - reconstructed":
		return "reconstructed"
	case "fanart - box - front", "fanart - box - back":
		return "fanart"
	default:
		return ""
	}
}

// SplitVariant removes the optional variant qualifier before positional
// region/language parsing. Untagged legacy files remain eligible as unknown.
func SplitVariant(parts []string) (string, []string) {
	variant := "unknown"
	clean := make([]string, 0, len(parts))
	for i, part := range parts {
		if i > 0 && i < len(parts)-1 && strings.HasPrefix(strings.ToLower(part), "variant-") {
			switch value := strings.TrimPrefix(strings.ToLower(part), "variant-"); value {
			case "original", "reconstructed", "fanart", "unknown":
				variant = value
			}
			continue
		}
		clean = append(clean, part)
	}
	return variant, clean
}
