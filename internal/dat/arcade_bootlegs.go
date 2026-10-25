package dat

import "strings"

func arcadeBootleg(g datGame) bool {
	return strings.Contains(strings.ToLower(g.Description+" "+g.Manufacturer), "bootleg")
}

// Bootleg is release provenance, not automatically a different game. Keep
// renamed derivatives for review, as cloneof alone also covers reskins/hacks.
func arcadeSameTitleBootleg(g, parent datGame) bool {
	return arcadeBootlegMatchesAliases(g, parent, nil)
}

func arcadeTitleStem(title string) string {
	return strings.ToLower(strings.TrimSpace(strings.SplitN(title, " (", 2)[0]))
}

func arcadeBootlegMatchesAliases(g, parent datGame, aliases map[string]map[string]bool) bool {
	if !arcadeBootleg(g) || arcadeFoldSpecial(g) || arcadeFoldSpecial(parent) ||
		arcadeReviewedContentVariant(g) || arcadeReviewedContentVariant(parent) ||
		arcadeFoldEditionKey(g) != arcadeFoldEditionKey(parent) {
		return false
	}
	if arcadeReviewedTechnicalClone(g) {
		return true
	}
	stem := arcadeTitleStem(g.Description)
	return stem != "" && (stem == arcadeTitleStem(parent.Description) || aliases[parent.Name][stem])
}

// 1943b changes protection/disclaimer code and the logo, not game identity.
// https://github.com/mamedev/mame/blob/mame0289/src/mame/capcom/1943.cpp
func arcadeReviewedTechnicalClone(g datGame) bool {
	return g.Name == "1943b" && g.CloneOf == "1943" &&
		strings.ReplaceAll(g.SourceFile, "\\", "/") == "capcom/1943.cpp"
}

// A regional title is evidence only inside an eligible official clone group.
// Do not learn aliases from hacks, bootlegs, development sets or unrelated roots.
func arcadeOfficialCloneAliases(games []datGame, hardware map[string]string, routing ArcadeRouting) map[string]map[string]bool {
	byName := map[string]datGame{}
	for _, g := range games {
		byName[g.Name] = g
	}
	roots := map[string]string{}
	aliases := map[string]map[string]bool{}
	for _, g := range games {
		if arcadeSpecial(g) || arcadeReviewedContentVariant(g) {
			continue
		}
		current := g
		seen := map[string]bool{g.Name: true}
		valid := true
		for current.CloneOf != "" {
			parent, ok := byName[current.CloneOf]
			if !ok || seen[parent.Name] {
				valid = false
				break
			}
			if arcadeSpecial(parent) || arcadeReviewedContentVariant(parent) ||
				arcadeFoldEditionKey(current) != arcadeFoldEditionKey(parent) ||
				!arcadeFoldHardwareCompatible(current, parent, hardware, routing) {
				break
			}
			seen[parent.Name] = true
			current = parent
		}
		if !valid {
			continue
		}
		roots[g.Name] = current.Name
		if aliases[current.Name] == nil {
			aliases[current.Name] = map[string]bool{}
		}
		aliases[current.Name][arcadeTitleStem(g.Description)] = true
	}
	result := map[string]map[string]bool{}
	for name, root := range roots {
		result[name] = aliases[root]
	}
	return result
}

func arcadeFoldEditionKey(g datGame) string {
	title := g.Description
	// Super II X is the Japanese title of Super II Turbo. A literal "turbo"
	// keyword must not override this reviewed regional parent/clone relation.
	if arcadeFoldSource(g) == "capcom/cps2.cpp" && g.CloneOf == "ssf2t" {
		switch g.Name {
		case "ssf2xj", "ssf2xjr1", "ssf2xjr1r", "ssf2xjr1d":
			return "turbo"
		}
	}
	// CPS2's Phoenix sets run decrypted code on boards with dead batteries;
	// the label is technical provenance rather than a new content edition.
	// https://github.com/mamedev/mame/blob/mame0289/src/mame/capcom/cps2.cpp
	if strings.ReplaceAll(g.SourceFile, "\\", "/") == "capcom/cps2.cpp" && arcadeBootleg(g) {
		title = strings.ReplaceAll(strings.ToLower(title), "phoenix edition", "")
	}
	return arcadeEditionKey(title)
}

// A same-game bootleg with unmapped hardware may use its eligible parent's
// browsing family. Exact hardware remains unknown; explicit routing wins.
// Resolve before release selection so clone-only imports keep stable paths.
func projectArcadeBootlegFamilies(entries []ArcadeEntry, index map[string]datGame, entryIndex map[string]int, families map[string]ArcadeFamily) {
	updates := map[string]ArcadeFamily{}
	parents := map[string]string{}
	for _, entry := range entries {
		if entry.Decision != "eligible" || entry.Family != "Arcade - Other" || entry.Hardware != "" ||
			entry.HardwareEvidence != "unmapped hardware; explicit fallback collection" {
			continue
		}
		current := index[entry.Name]
		seen := map[string]bool{current.Name: true}
		for current.CloneOf != "" {
			parent, ok := index[current.CloneOf]
			if !ok || seen[parent.Name] || current.SourceFile == "" || parent.SourceFile == "" || !arcadeSameTitleBootleg(current, parent) {
				break
			}
			seen[parent.Name] = true
			family, eligible := families[parent.Name]
			if !eligible {
				break
			}
			if family.Kind == "hardware" {
				updates[entry.Name], parents[entry.Name] = family, parent.Name
				break
			}
			if family.Platform != "Arcade - Other" {
				break
			}
			current = parent
		}
	}
	for name, family := range updates {
		families[name] = family
		entry := &entries[entryIndex[name]]
		entry.Family = family.Platform
		entry.HardwareEvidence += "; catalog family follows bootleg parent " + parents[name]
	}
}
