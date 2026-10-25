package dat

import (
	"regexp"
	"sort"
	"strings"
)

// A name match is never hardware evidence. Reviewed board families may span
// drivers; otherwise retain driver boundaries. Device lists can differ for
// protection removal or prototypes, so they do not veto a same-driver parent
// link. Matching nonempty profiles provide additional evidence for sibling
// groups on unknown boards; they do not identify a canonical hardware platform.
func arcadeFoldHardwareCompatible(a, b datGame, hardware map[string]string, r ArcadeRouting) bool {
	as, bs := arcadeFoldSource(a), arcadeFoldSource(b)
	if as == "" || bs == "" {
		return false
	}
	ah, bh := hardware[a.Name], hardware[b.Name]
	if ah != "" && bh != "" {
		return ah == bh || sameArcadeFoldFamily(ah, bh, r)
	}
	return as == bs || arcadeReviewedDriverLink(a, b)
}

// MAME 0.289 scobra.cpp retains the older Super Cobra implementation;
// tazmani3 changes type2 address decoding, not the hardware family. Keep this
// review scoped to the recorded clone edge, not every game in either driver.
func arcadeReviewedDriverLink(a, b datGame) bool {
	if a.Name == "tazmania" {
		a, b = b, a
	}
	return a.Name == "tazmani3" && a.CloneOf == "tazmania" &&
		b.Name == "tazmania" && arcadeFoldSource(a) == "galaxian/scobra.cpp" &&
		arcadeFoldSource(b) == "galaxian/galaxian.cpp"
}

func arcadeFoldSource(g datGame) string {
	return strings.ReplaceAll(g.SourceFile, "\\", "/")
}

func arcadeFoldDevices(g datGame) string {
	devices := append([]string(nil), g.DeviceRefs...)
	sort.Strings(devices)
	return strings.Join(devices, "\x00")
}

func arcadeSiblingHardwareKey(g datGame, hardware map[string]string, r ArcadeRouting) string {
	if arcadeFoldSource(g) == "" {
		return ""
	}
	if h := hardware[g.Name]; h != "" {
		return "hardware:" + arcadeCatalogPlatform(h, r)
	}
	if devices := arcadeFoldDevices(g); devices != "" {
		return "driver:" + arcadeFoldSource(g) + "\x01" + devices
	}
	return ""
}

// cloneof can flatten several hardware/content branches under one parent.
// Join compatible sibling roots after normal parent folding, without crossing
// the boundary that prevented those roots from joining their parent.
func foldArcadeSiblingGroups(groups map[string][]datGame, byName map[string]datGame, hardware map[string]string, r ArcadeRouting, notes map[string]string) map[string]string {
	type key struct{ parent, hardware, edition, content string }
	candidates := map[key][]string{}
	titles := map[string]string{}
	for root := range groups {
		g := byName[root]
		if notes[root] != "different or unknown driver hardware requires fold review" &&
			notes[root] != "renamed or modified bootleg requires content review" &&
			notes[root] != "clone parent not selected on the same platform" &&
			notes[root] != "content hack or distinct unofficial content kept separate" &&
			notes[root] != "reviewed content variant kept separate" &&
			notes[root] != "different content editions require fold review" {
			continue
		}
		parent, present := byName[g.CloneOf]
		if !present || !arcadeValidCloneAncestry(g, byName) ||
			g.Incomplete ||
			arcadeFoldSpecial(parent) || arcadeReviewedContentVariant(parent) {
			continue
		}
		h := arcadeSiblingHardwareKey(g, hardware, r)
		if h == "" {
			continue
		}
		// Keep unfamiliar content/cabinet qualifiers within an official
		// branch too. Regional aliases may differ in their title stems.
		content := "official:" + arcadeSiblingOfficialQualifiers(arcadeTechnicalDescription(g))
		// A separate content edition can have its own regional/revision branch.
		// Require the same title as well as the edition key within that branch.
		if arcadeFoldEditionKey(g) != arcadeFoldEditionKey(parent) || notes[root] == "clone parent not selected on the same platform" {
			content += "\x00" + arcadeSiblingBranchTitle(g)
		}
		if arcadeBootleg(g) {
			// Preserve the derivative name and maker/unknown qualifiers. Only
			// numeric set labels are irrelevant to sibling bootleg identity.
			content = "bootleg:" + strings.ToLower(arcadeVariantBranchTitle(g)) +
				"\x00" + arcadeSiblingBootlegMaker(g)
		}
		if arcadeFoldSpecial(g) || arcadeReviewedContentVariant(g) {
			// A generic "hack" label is not an identity shared by all unnamed
			// patches. Require a derivative name or a named modification.
			qualifier := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(arcadeSiblingOfficialQualifiers(arcadeVariantBranchTitle(g)), "bootleg", ""), "hack", ""))
			if arcadeFoldSpecial(g) && arcadeTitleStem(g.Description) == arcadeTitleStem(parent.Description) && qualifier == "" {
				continue
			}
			content = "variant:" + strings.ToLower(arcadeVariantBranchTitle(g)) + "\x00" + strings.ToLower(g.Manufacturer)
		}
		k := key{g.CloneOf, h, arcadeFoldEditionKey(g), content}
		candidates[k] = append(candidates[k], root)
	}
	for _, roots := range candidates {
		if len(roots) < 2 {
			continue
		}
		sort.Strings(roots)
		root := roots[0]
		for _, other := range roots[1:] {
			groups[root] = append(groups[root], groups[other]...)
			delete(groups, other)
		}
		for _, release := range groups[root] {
			delete(notes, release.Name)
		}
		if arcadeFoldSpecial(byName[root]) || arcadeReviewedContentVariant(byName[root]) {
			titles[root] = arcadeVariantBranchTitle(byName[root])
		} else if arcadeBootleg(byName[root]) {
			titles[root] = arcadeVariantBranchTitle(byName[root])
		} else {
			titles[root] = arcadeSiblingTitle(arcadeCardTitle(byName[root]))
		}
	}
	return titles
}

func arcadeSiblingBranchTitle(g datGame) string {
	// The CPS-2 sample's Japanese regional title is explicitly Rockman in the
	// same reviewed Mega Man branch; do not generalize aliases across drivers.
	if arcadeFoldSource(g) == "capcom/cps2.cpp" && g.CloneOf == "megaman" && g.Name == "rmancp2j" {
		return "mega man: the power battle"
	}
	return arcadeTitleStem(g.Description)
}

// sf2b2 is documented as an extremely similar PCB to sf2b and both use the
// sf2b machine configuration. Its unknown maker does not create a new branch.
// Do not generalize this to all unknown makers or change source metadata.
func arcadeSiblingBootlegMaker(g datGame) string {
	maker := strings.ToLower(strings.TrimSpace(g.Manufacturer))
	if g.CloneOf == "sf2" && arcadeFoldSource(g) == "capcom/cps1bl_5205.cpp" &&
		((g.Name == "sf2b" && maker == "bootleg (playmark)") ||
			(g.Name == "sf2b2" && maker == "bootleg")) {
		return "reviewed:sf2b"
	}
	return maker
}

func arcadeValidCloneAncestry(g datGame, byName map[string]datGame) bool {
	seen := map[string]bool{g.Name: true}
	for g.CloneOf != "" {
		parent, ok := byName[g.CloneOf]
		if !ok || seen[parent.Name] {
			return false
		}
		seen[parent.Name] = true
		g = parent
	}
	return true
}

var arcadeSiblingSetRE = regexp.MustCompile("(?i)^set\\s+[0-9]+[a-z]?$")

func arcadeSiblingTitle(title string) string {
	return strings.TrimSpace(arcadeParentheticalRE.ReplaceAllStringFunc(title, func(q string) string {
		var kept []string
		for _, part := range strings.Split(q[1:len(q)-1], ",") {
			part = strings.TrimSpace(part)
			if arcadeSiblingSetRE.MatchString(part) {
				continue
			}
			kept = append(kept, part)
		}
		if len(kept) == 0 {
			return ""
		}
		return "(" + strings.Join(kept, ", ") + ")"
	}))
}

// Inspect qualifiers individually: a region prefix or a numeric set prefix
// must not hide unfamiliar content descriptions such as "uncensored".
func arcadeSiblingOfficialQualifiers(title string) string {
	var kept []string
	for _, q := range arcadeParentheticalRE.FindAllString(title, -1) {
		for _, part := range strings.Split(q[1:len(q)-1], ",") {
			part = strings.ToLower(strings.TrimSpace(part))
			if arcadeSiblingSetRE.MatchString(part) {
				continue
			}
			if !strings.HasPrefix(part, "set ") && arcadeTechnicalQualifier(part) {
				continue
			}
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, "\x00")
}
