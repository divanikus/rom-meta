package medianames

// SourceParts keeps gamelist.<collection> as one source token during positional
// region/language parsing. Existing provider suffixes retain their meaning.
func SourceParts(parts []string) []string {
	n := len(parts)
	if n >= 3 && parts[n-2] == "gamelist" && parts[n-1] != "" {
		out := append([]string(nil), parts[:n-2]...)
		return append(out, "gamelist."+parts[n-1])
	}
	return parts
}
