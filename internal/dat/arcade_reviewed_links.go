package dat

// These catalog links are narrower than MAME's ROM sharing graph. Never alter
// cloneof/romof in a release or use a title match to infer other independent roots.
func arcadeCatalogParent(g datGame) string {
	if arcadeFoldSource(g) == "sega/naomi.cpp" && g.CloneOf == "" {
		switch g.Name {
		case "initdexp":
			return "initd"
		case "initdv3e":
			return "initdv3j"
		}
	}
	if arcadeFoldSource(g) == "capcom/cps2.cpp" && g.Name == "ssf2tbd" && g.CloneOf == "ssf2" {
		return "ssf2tb"
	}
	return g.CloneOf
}
